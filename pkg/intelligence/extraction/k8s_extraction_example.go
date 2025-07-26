package extraction

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes/fake"
)

// ExampleK8sContextExtraction demonstrates how to use the K8s context extractor
func ExampleK8sContextExtraction() {
	// Create a fake K8s client for testing
	k8sClient := fake.NewSimpleClientset()
	logger := zap.NewExample()

	// Create the extractor
	extractor, err := NewK8sContextExtractor(k8sClient, logger)
	if err != nil {
		panic(fmt.Errorf("failed to create extractor: %w", err))
	}

	// Create sample events from different collectors
	events := []*domain.UnifiedEvent{
		// eBPF event with container ID
		{
			ID:        "ebpf-001",
			Timestamp: time.Now(),
			Type:      domain.EventTypeSystem,
			Source:    "ebpf",
			Kernel: &domain.KernelData{
				Syscall:     "openat",
				PID:         1234,
				ContainerID: "docker://abc123def456",
				Comm:        "nginx",
			},
			Entity: &domain.EntityContext{
				Type: "container",
				Name: "nginx",
			},
		},
		// K8s event
		{
			ID:        "k8s-001",
			Timestamp: time.Now(),
			Type:      domain.EventTypeLog,
			Source:    "k8s",
			Kubernetes: &domain.KubernetesData{
				EventType: "Warning",
				Reason:    "BackOff",
				Object:    "pod/my-app-xyz",
				Message:   "Back-off restarting failed container",
			},
			Entity: &domain.EntityContext{
				Type:      "pod",
				Name:      "my-app-xyz",
				Namespace: "default",
			},
		},
		// CNI network event
		{
			ID:        "cni-001",
			Timestamp: time.Now(),
			Type:      domain.EventTypeNetwork,
			Source:    "cni",
			Network: &domain.NetworkData{
				Protocol:   "TCP",
				SourceIP:   "10.244.1.5",
				SourcePort: 8080,
				DestIP:     "10.244.2.10",
				DestPort:   3306,
				Direction:  "egress",
			},
			Entity: &domain.EntityContext{
				Type: "pod",
				Name: "web-frontend",
				Labels: map[string]string{
					"app": "frontend",
					"env": "production",
				},
			},
		},
		// Critical error event
		{
			ID:        "app-001",
			Timestamp: time.Now(),
			Type:      domain.EventTypeLog,
			Source:    "app",
			Severity:  domain.EventSeverityCritical,
			Application: &domain.ApplicationData{
				Level:   "error",
				Message: "Database connection pool exhausted",
				Logger:  "com.example.db.ConnectionPool",
			},
			Entity: &domain.EntityContext{
				Type:      "pod",
				Name:      "api-server-abc",
				Namespace: "production",
			},
		},
	}

	// Process each event
	ctx := context.Background()
	for _, event := range events {
		fmt.Printf("\n=== Processing Event: %s ===\n", event.ID)
		fmt.Printf("Source: %s, Type: %s\n", event.Source, event.Type)

		// Extract K8s context
		err := extractor.Process(ctx, event)
		if err != nil {
			fmt.Printf("Error extracting context: %v\n", err)
			continue
		}

		// Display extracted context
		if event.K8sContext != nil {
			k8sCtx := event.K8sContext
			fmt.Printf("K8s Context Extracted:\n")
			fmt.Printf("  - Kind: %s\n", k8sCtx.Kind)
			fmt.Printf("  - Name: %s\n", k8sCtx.Name)
			fmt.Printf("  - Namespace: %s\n", k8sCtx.Namespace)
			fmt.Printf("  - Node: %s\n", k8sCtx.NodeName)
			fmt.Printf("  - Workload: %s/%s\n", k8sCtx.WorkloadKind, k8sCtx.WorkloadName)
			fmt.Printf("  - QoS Class: %s\n", k8sCtx.QoSClass)
			fmt.Printf("  - Phase: %s\n", k8sCtx.Phase)

			// Show extraction depth
			depth := extractor.determineExtractionDepth(event)
			fmt.Printf("  - Extraction Depth: %v\n", depth)
		} else {
			fmt.Printf("No K8s context extracted (not K8s related)\n")
		}
	}

	// Display metrics
	fmt.Printf("\n=== Extraction Metrics ===\n")
	metrics := extractor.GetMetrics()
	for source, m := range metrics {
		fmt.Printf("Source: %s\n", source)
		fmt.Printf("  - Total: %d\n", m.TotalExtractions)
		fmt.Printf("  - Shallow: %d\n", m.ShallowCount)
		fmt.Printf("  - Medium: %d\n", m.MediumCount)
		fmt.Printf("  - Deep: %d\n", m.DeepCount)
	}
}

// DemonstrateCorrelationFromK8sContext shows how K8s context enables automatic correlation
func DemonstrateCorrelationFromK8sContext() {
	fmt.Println("\n=== K8s Native Correlation Demo ===")

	// Example events that would be automatically correlated
	type eventExample struct {
		id          string
		description string
		k8sContext  domain.K8sContext
	}

	_ = []eventExample{
		{
			id:          "event-1",
			description: "Pod OOMKilled",
			k8sContext: domain.K8sContext{
				Name:      "frontend-abc123",
				Namespace: "production",
				OwnerReferences: []domain.OwnerReference{
					{Kind: "ReplicaSet", Name: "frontend-56789"},
				},
				NodeName: "node-1",
				Labels: map[string]string{
					"app": "frontend",
					"env": "production",
				},
			},
		},
		{
			id:          "event-2",
			description: "Service endpoint not ready",
			k8sContext: domain.K8sContext{
				Name:      "frontend-service",
				Namespace: "production",
				Selectors: map[string]string{
					"app": "frontend",
				},
			},
		},
		{
			id:          "event-3",
			description: "ReplicaSet scaling up",
			k8sContext: domain.K8sContext{
				Name:      "frontend-56789",
				Namespace: "production",
				OwnerReferences: []domain.OwnerReference{
					{Kind: "Deployment", Name: "frontend"},
				},
			},
		},
		{
			id:          "event-4",
			description: "Node memory pressure",
			k8sContext: domain.K8sContext{
				Name: "node-1",
				Kind: "Node",
				Conditions: []domain.ConditionSnapshot{
					{Type: "MemoryPressure", Status: "True"},
				},
			},
		},
	}

	// Show correlations
	fmt.Println("\nAutomatic Correlations from K8s Structure:")
	fmt.Println("1. Ownership Chain: Pod → ReplicaSet → Deployment")
	fmt.Println("   - Event 1 (Pod OOMKilled) → Event 3 (ReplicaSet scaling)")
	fmt.Println("\n2. Label Selector Matching:")
	fmt.Println("   - Event 1 (Pod) matches Event 2 (Service) via app=frontend")
	fmt.Println("\n3. Node Topology:")
	fmt.Println("   - Event 1 (Pod) and Event 4 (Node) on same node")
	fmt.Println("\n4. Multi-dimensional Correlation:")
	fmt.Println("   - Temporal: All events within 5 minute window")
	fmt.Println("   - Spatial: Same namespace and node")
	fmt.Println("   - Causal: Memory pressure → OOMKill → Scaling")
	fmt.Println("   - Semantic: All related to 'frontend' workload")
}
