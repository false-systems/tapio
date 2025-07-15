package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/yairfalse/tapio/internal/output"
	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/k8s"
	"github.com/yairfalse/tapio/pkg/simple"
	"github.com/yairfalse/tapio/pkg/types"
)

var (
	watchNamespace    string
	watchAll          bool
	watchFollow       bool
	watchCorrelation  bool
	watchOutputFormat string
	watchVerbose      bool
)

var watchCmd = &cobra.Command{
	Use:   "watch [resource]",
	Short: "Real-time monitoring that tells you when things break",
	Long: `Watch provides real-time monitoring of your Kubernetes resources.

Watch continuously monitors your cluster and alerts you when:
  • Pods crash or restart unexpectedly
  • Resources approach dangerous thresholds
  • Patterns indicate upcoming failures
  • Related issues cascade across services

The monitoring is intelligent - it correlates events across multiple sources
to give you the full picture of what's happening in your cluster.`,

	Example: `  # Watch current namespace for any issues
  tapio watch

  # Watch specific application
  tapio watch my-app

  # Watch specific pod
  tapio watch pod/my-app-7d4b9c8f-h2x9m

  # Watch entire cluster
  tapio watch --all

  # Watch specific namespace
  tapio watch --namespace production

  # Watch and follow new resources
  tapio watch --follow

  # Watch with detailed correlation analysis
  tapio watch --correlation`,

	Args: cobra.MaximumNArgs(1),

	PreRunE: func(cmd *cobra.Command, args []string) error {
		// Validate output format
		if err := ValidateOutputFormat(watchOutputFormat); err != nil {
			return err
		}

		// Validate namespace
		if err := ValidateNamespace(watchNamespace); err != nil {
			return err
		}

		// Validate resource format if provided
		if len(args) > 0 {
			if err := validateResourceFormat(args[0]); err != nil {
				return err
			}
		}

		// Check for conflicting flags
		if watchAll && watchNamespace != "" {
			return NewCLIError(
				"flag validation",
				"Cannot use --all and --namespace together",
				"Use either --all for all namespaces or --namespace for a specific one",
			).WithExamples(
				"tapio watch --all",
				"tapio watch --namespace production",
			)
		}

		return nil
	},

	RunE: runWatch,
}

func init() {
	watchCmd.Flags().StringVarP(&watchNamespace, "namespace", "n", "",
		"Target namespace (default: current namespace from kubeconfig)")
	watchCmd.Flags().BoolVar(&watchAll, "all", false,
		"Watch all namespaces (requires cluster-wide permissions)")
	watchCmd.Flags().BoolVar(&watchFollow, "follow", false,
		"Follow new resources as they appear")
	watchCmd.Flags().BoolVar(&watchCorrelation, "correlation", true,
		"Enable intelligent correlation analysis")
	watchCmd.Flags().StringVarP(&watchOutputFormat, "output", "o", "stream",
		"Output format: stream (default), json-stream")
	watchCmd.Flags().BoolVarP(&watchVerbose, "verbose", "v", false,
		"Show detailed information including all events")
}

// GetWatchCommand returns the watch command
func GetWatchCommand() *cobra.Command {
	return watchCmd
}

// WatchState tracks the current state of monitored resources
type WatchState struct {
	resources      map[string]*types.ResourceInfo
	problems       map[string]*types.Problem
	correlationSvc *correlation.Service
	eventCount     int64
	startTime      time.Time
	mu             sync.RWMutex
}

func newWatchState() *WatchState {
	return &WatchState{
		resources: make(map[string]*types.ResourceInfo),
		problems:  make(map[string]*types.Problem),
		startTime: time.Now(),
	}
}

func (ws *WatchState) updateResource(resource *types.ResourceInfo) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	
	key := fmt.Sprintf("%s/%s/%s", resource.Namespace, resource.Kind, resource.Name)
	ws.resources[key] = resource
}

func (ws *WatchState) updateProblem(problem *types.Problem) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	
	key := fmt.Sprintf("%s/%s/%s", problem.Resource.Namespace, problem.Resource.Kind, problem.Resource.Name)
	if problem.Severity == types.SeverityHealthy {
		// Resource is healthy, remove any existing problem
		delete(ws.problems, key)
	} else {
		ws.problems[key] = problem
	}
}

func (ws *WatchState) incrementEventCount() {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.eventCount++
}

func (ws *WatchState) getStats() (int, int, int64, time.Duration) {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	
	return len(ws.resources), len(ws.problems), ws.eventCount, time.Since(ws.startTime)
}

func runWatch(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Create checker
	checker, err := simple.NewChecker()
	if err != nil {
		return ErrKubernetesConnection(err)
	}

	// Create watch manager
	watchManager := k8s.NewWatchManager(checker.GetClient(), k8s.DefaultWatchConfig())
	defer watchManager.Close()

	// Create watch state
	state := newWatchState()

	// Start correlation service if enabled
	if watchCorrelation {
		correlationSvc, err := correlation.NewService()
		if err != nil {
			// Log but don't fail
			fmt.Fprintf(os.Stderr, "Warning: Correlation service unavailable: %v\n", err)
		} else {
			state.correlationSvc = correlationSvc
			if err := correlationSvc.Start(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to start correlation service: %v\n", err)
				state.correlationSvc = nil
			} else {
				defer correlationSvc.Stop()
			}
		}
	}

	// Determine what to watch
	namespace := watchNamespace
	if namespace == "" && !watchAll {
		namespace = getCurrentNamespace()
	}

	resource := ""
	if len(args) > 0 {
		resource = args[0]
	}

	// Print header
	printWatchHeader(namespace, resource, watchAll)

	// Create event processor
	eventProcessor := createEventProcessor(checker, state, watchOutputFormat)

	// Start watching resources
	eventChannels := []<-chan k8s.WatchEvent{}

	if watchAll {
		// Watch all namespaces
		namespaces, err := checker.GetNamespaces(ctx)
		if err != nil {
			return NewCLIError(
				"namespace listing",
				"Failed to list namespaces",
				"Check your cluster permissions",
			)
		}

		for _, ns := range namespaces {
			ch, err := watchManager.WatchPods(ctx, ns)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to watch pods in namespace %s: %v\n", ns, err)
				continue
			}
			eventChannels = append(eventChannels, ch)
		}
	} else {
		// Watch specific namespace
		ch, err := watchManager.WatchPods(ctx, namespace)
		if err != nil {
			return NewCLIError(
				"watch setup",
				fmt.Sprintf("Failed to watch pods in namespace %s", namespace),
				"Check your permissions and namespace existence",
			)
		}
		eventChannels = append(eventChannels, ch)

		// Also watch deployments and services for better correlation
		if deploymentCh, err := watchManager.WatchDeployments(ctx, namespace); err == nil {
			eventChannels = append(eventChannels, deploymentCh)
		}
		if serviceCh, err := watchManager.WatchServices(ctx, namespace); err == nil {
			eventChannels = append(eventChannels, serviceCh)
		}
	}

	// Merge all event channels
	mergedEvents := mergeEventChannels(ctx, eventChannels...)

	// Create status ticker
	statusTicker := time.NewTicker(30 * time.Second)
	defer statusTicker.Stop()

	// Main event loop
	for {
		select {
		case event, ok := <-mergedEvents:
			if !ok {
				return fmt.Errorf("watch stream closed unexpectedly")
			}

			state.incrementEventCount()
			eventProcessor(ctx, event)

		case <-statusTicker.C:
			// Print periodic status update
			if watchVerbose {
				printWatchStatus(state)
			}

		case <-sigCh:
			// Graceful shutdown
			fmt.Println("\n\nStopping watch...")
			printWatchSummary(state)
			return nil

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func printWatchHeader(namespace, resource string, all bool) {
	fmt.Println()
	
	watchTarget := "current namespace"
	if all {
		watchTarget = "all namespaces"
	} else if namespace != "" {
		watchTarget = fmt.Sprintf("namespace: %s", namespace)
	}
	
	if resource != "" {
		watchTarget = fmt.Sprintf("%s (filtering: %s)", watchTarget, resource)
	}

	fmt.Printf("👁  Watching %s for issues...\n", watchTarget)
	fmt.Println("Press Ctrl+C to stop")
	fmt.Println(strings.Repeat("-", 80))
	fmt.Println()
}

func createEventProcessor(checker *simple.Checker, state *WatchState, outputFormat string) func(context.Context, k8s.WatchEvent) {
	// Create output formatter
	var formatter output.WatchEventFormatter
	switch outputFormat {
	case "json-stream":
		formatter = output.NewJSONStreamFormatter()
	default:
		formatter = output.NewStreamFormatter()
	}

	return func(ctx context.Context, event k8s.WatchEvent) {
		// Extract resource info from event
		resourceInfo := extractResourceInfo(event)
		if resourceInfo == nil {
			return
		}

		// Update state
		state.updateResource(resourceInfo)

		// Check resource health
		problem := checkResourceHealth(ctx, checker, resourceInfo, event)
		if problem != nil {
			state.updateProblem(problem)
			
			// Run correlation if available
			if state.correlationSvc != nil {
				correlateEvent(ctx, state.correlationSvc, problem)
			}
		}

		// Format and output event
		watchEvent := &output.WatchEvent{
			Type:      string(event.Type),
			Resource:  resourceInfo,
			Problem:   problem,
			Timestamp: event.Time,
			Sequence:  event.Sequence,
		}

		formatter.Format(watchEvent)
	}
}

func extractResourceInfo(event k8s.WatchEvent) *types.ResourceInfo {
	// Extract metadata from the Kubernetes object
	// This is a simplified version - in production we'd use reflection or type assertions
	return &types.ResourceInfo{
		Kind:      getObjectKind(event.Object),
		Name:      getObjectName(event.Object),
		Namespace: getObjectNamespace(event.Object),
	}
}

func checkResourceHealth(ctx context.Context, checker *simple.Checker, resource *types.ResourceInfo, event k8s.WatchEvent) *types.Problem {
	// Analyze the event to determine if there's a problem
	if event.Type == watch.Deleted {
		return &types.Problem{
			Resource:    *resource,
			Title:       fmt.Sprintf("%s deleted", resource.Kind),
			Description: fmt.Sprintf("%s %s was deleted from namespace %s", resource.Kind, resource.Name, resource.Namespace),
			Severity:    types.SeverityWarning,
			DetectedAt:  time.Now(),
		}
	}

	// For modified events, check the resource state based on type
	if event.Type == watch.Modified {
		switch obj := event.Object.(type) {
		case *corev1.Pod:
			return analyzePodHealth(obj, resource)
		case *appsv1.Deployment:
			return analyzeDeploymentHealth(obj, resource)
		case *corev1.Service:
			return analyzeServiceHealth(obj, resource)
		}
	}

	return nil
}

func analyzePodHealth(pod *corev1.Pod, resource *types.ResourceInfo) *types.Problem {
	// Check pod phase
	if pod.Status.Phase == corev1.PodFailed {
		return &types.Problem{
			Resource:    *resource,
			Title:       "Pod failed",
			Description: fmt.Sprintf("Pod %s has failed: %s", pod.Name, pod.Status.Reason),
			Severity:    types.SeverityCritical,
			DetectedAt:  time.Now(),
		}
	}

	// Check container statuses
	for _, containerStatus := range pod.Status.ContainerStatuses {
		// Check for crash loops
		if containerStatus.RestartCount > 3 {
			return &types.Problem{
				Resource:    *resource,
				Title:       "Container restart loop detected",
				Description: fmt.Sprintf("Container %s has restarted %d times", containerStatus.Name, containerStatus.RestartCount),
				Severity:    types.SeverityWarning,
				DetectedAt:  time.Now(),
				NextSteps: []string{
					fmt.Sprintf("kubectl logs %s -c %s --previous", pod.Name, containerStatus.Name),
					fmt.Sprintf("kubectl describe pod %s", pod.Name),
				},
			}
		}

		// Check for OOMKilled
		if containerStatus.State.Terminated != nil && containerStatus.State.Terminated.Reason == "OOMKilled" {
			return &types.Problem{
				Resource:    *resource,
				Title:       "Container killed due to memory limit",
				Description: fmt.Sprintf("Container %s was OOMKilled", containerStatus.Name),
				Severity:    types.SeverityCritical,
				DetectedAt:  time.Now(),
				SuggestedFix: fmt.Sprintf("kubectl set resources deployment %s -c %s --limits=memory=<higher-value>", 
					pod.Labels["app"], containerStatus.Name),
			}
		}

		// Check for image pull errors
		if containerStatus.State.Waiting != nil && strings.Contains(containerStatus.State.Waiting.Reason, "ImagePull") {
			return &types.Problem{
				Resource:    *resource,
				Title:       "Image pull error",
				Description: fmt.Sprintf("Cannot pull image for container %s: %s", 
					containerStatus.Name, containerStatus.State.Waiting.Message),
				Severity:    types.SeverityCritical,
				DetectedAt:  time.Now(),
			}
		}
	}

	// Check pod conditions
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status != corev1.ConditionTrue {
			if time.Since(condition.LastTransitionTime.Time) > 5*time.Minute {
				return &types.Problem{
					Resource:    *resource,
					Title:       "Pod not ready",
					Description: fmt.Sprintf("Pod has been not ready for %s: %s", 
						time.Since(condition.LastTransitionTime.Time).Round(time.Minute), condition.Message),
					Severity:    types.SeverityWarning,
					DetectedAt:  time.Now(),
				}
			}
		}
	}

	// Pod seems healthy
	return nil
}

func analyzeDeploymentHealth(deployment *appsv1.Deployment, resource *types.ResourceInfo) *types.Problem {
	// Check if deployment is progressing
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentProgressing && condition.Status != corev1.ConditionTrue {
			return &types.Problem{
				Resource:    *resource,
				Title:       "Deployment not progressing",
				Description: fmt.Sprintf("Deployment %s is not progressing: %s", deployment.Name, condition.Message),
				Severity:    types.SeverityWarning,
				DetectedAt:  time.Now(),
			}
		}
	}

	// Check replica availability
	if deployment.Status.Replicas > deployment.Status.AvailableReplicas {
		unavailable := deployment.Status.Replicas - deployment.Status.AvailableReplicas
		return &types.Problem{
			Resource:    *resource,
			Title:       "Deployment has unavailable replicas",
			Description: fmt.Sprintf("%d of %d replicas are unavailable", unavailable, deployment.Status.Replicas),
			Severity:    types.SeverityWarning,
			DetectedAt:  time.Now(),
			NextSteps: []string{
				fmt.Sprintf("kubectl get pods -l app=%s", deployment.Name),
				fmt.Sprintf("kubectl describe deployment %s", deployment.Name),
			},
		}
	}

	return nil
}

func analyzeServiceHealth(service *corev1.Service, resource *types.ResourceInfo) *types.Problem {
	// Check if service has endpoints
	if service.Spec.Type != corev1.ServiceTypeExternalName && len(service.Spec.Selector) > 0 {
		// Note: In a real implementation, we'd check the endpoints object
		// For now, we'll just return nil
		return nil
	}

	return nil
}

func correlateEvent(ctx context.Context, correlationSvc *correlation.Service, problem *types.Problem) {
	// Send problem to correlation service for analysis
	result := &types.CheckResult{
		Problems: []types.Problem{*problem},
	}

	correlationResult, err := correlationSvc.AnalyzeCheckResult(ctx, result)
	if err != nil {
		return
	}

	// Output any critical insights
	insights := correlationResult.GetMostCriticalInsights(3)
	for _, insight := range insights {
		if insight.Severity == "critical" || insight.Severity == "high" {
			fmt.Printf("\n%s Correlation Insight: %s\n", 
				color.YellowString("⚡"),
				color.YellowString(insight.Title))
			if len(insight.Description) > 0 {
				fmt.Printf("   %s\n", insight.Description)
			}
		}
	}
}

func mergeEventChannels(ctx context.Context, channels ...<-chan k8s.WatchEvent) <-chan k8s.WatchEvent {
	out := make(chan k8s.WatchEvent, 100)
	var wg sync.WaitGroup

	// Start a goroutine for each input channel
	for _, ch := range channels {
		wg.Add(1)
		go func(c <-chan k8s.WatchEvent) {
			defer wg.Done()
			for {
				select {
				case event, ok := <-c:
					if !ok {
						return
					}
					select {
					case out <- event:
					case <-ctx.Done():
						return
					}
				case <-ctx.Done():
					return
				}
			}
		}(ch)
	}

	// Start a goroutine to close the output channel when all input channels are done
	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

func printWatchStatus(state *WatchState) {
	resources, problems, events, duration := state.getStats()
	
	fmt.Printf("\n📊 Status: Monitoring %d resources | %d issues detected | %d events processed | Running for %s\n\n",
		resources, problems, events, duration.Round(time.Second))
}

func printWatchSummary(state *WatchState) {
	resources, problems, events, duration := state.getStats()
	
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("\n📈 Watch Summary:\n")
	fmt.Printf("   Duration:          %s\n", duration.Round(time.Second))
	fmt.Printf("   Resources watched: %d\n", resources)
	fmt.Printf("   Issues detected:   %d\n", problems)
	fmt.Printf("   Events processed:  %d\n", events)
	
	if problems > 0 {
		fmt.Printf("\n   💡 Run 'tapio check' for detailed analysis of current issues\n")
	}
}

// Helper functions to extract metadata from Kubernetes objects
func getObjectKind(obj interface{}) string {
	// Use Kubernetes runtime helpers to get object kind
	switch obj.(type) {
	case *corev1.Pod:
		return "Pod"
	case *corev1.Service:
		return "Service"
	case *appsv1.Deployment:
		return "Deployment"
	case *appsv1.DaemonSet:
		return "DaemonSet"
	case *appsv1.StatefulSet:
		return "StatefulSet"
	case *batchv1.Job:
		return "Job"
	case *batchv1.CronJob:
		return "CronJob"
	default:
		// Try to get from TypeMeta
		if metaObj, ok := obj.(metav1.Object); ok {
			gvk := metaObj.GetObjectKind().GroupVersionKind()
			if gvk.Kind != "" {
				return gvk.Kind
			}
		}
		return "Unknown"
	}
}

func getObjectName(obj interface{}) string {
	// Extract name from metadata
	if metaObj, ok := obj.(metav1.Object); ok {
		return metaObj.GetName()
	}
	return "unknown"
}

func getObjectNamespace(obj interface{}) string {
	// Extract namespace from metadata
	if metaObj, ok := obj.(metav1.Object); ok {
		ns := metaObj.GetNamespace()
		if ns == "" {
			return "default"
		}
		return ns
	}
	return "default"
}