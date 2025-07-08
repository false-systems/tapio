package simple

import (
	"fmt"
	"strings"

	"github.com/falseyair/tapio/pkg/types"
	corev1 "k8s.io/api/core/v1"
)

// FriendlyExplainer provides human-friendly explanations
type FriendlyExplainer struct {
	*Explainer
}

// MakeFriendlySummary creates a conversational summary
func MakeFriendlySummary(pod *corev1.Pod, causes []types.RootCause) string {
	if len(causes) == 0 {
		return fmt.Sprintf("Good news! Your pod '%s' looks healthy to me! 🎉", pod.Name)
	}

	primaryCause := causes[0]
	
	switch {
	case strings.Contains(primaryCause.Title, "Memory limit"):
		return fmt.Sprintf("Your app '%s' is using too much memory and keeps getting killed 💥", pod.Name)
		
	case strings.Contains(primaryCause.Title, "image not found"):
		return fmt.Sprintf("I can't find the container image for '%s' 🔍", pod.Name)
		
	case strings.Contains(primaryCause.Title, "cannot be scheduled"):
		return fmt.Sprintf("Your pod '%s' is stuck waiting - there's no room in the cluster 📦", pod.Name)
		
	case strings.Contains(primaryCause.Title, "keeps crashing"):
		return fmt.Sprintf("Your app '%s' keeps crashing right after it starts 💔", pod.Name)
		
	case strings.Contains(primaryCause.Title, "probe"):
		return fmt.Sprintf("Your app '%s' isn't responding to health checks ❤️‍🩹", pod.Name)
		
	default:
		return fmt.Sprintf("Something's not right with '%s' 🤔", pod.Name)
	}
}

// MakeFriendlyRootCauses creates conversational root cause explanations
func MakeFriendlyRootCauses(causes []types.RootCause) []types.RootCause {
	friendlyCauses := make([]types.RootCause, len(causes))
	
	for i, cause := range causes {
		friendlyCauses[i] = cause
		
		switch {
		case strings.Contains(cause.Title, "Memory limit"):
			friendlyCauses[i].Title = "Your app needs more memory"
			friendlyCauses[i].Description = "Think of it like this: Your app is trying to use a gallon of water, but you only gave it a cup. It keeps overflowing and Kubernetes has to restart it."
			
		case strings.Contains(cause.Title, "image not found"):
			friendlyCauses[i].Title = "Can't find your container image"
			friendlyCauses[i].Description = "It's like trying to install an app that doesn't exist in the app store. Either the name is wrong, or you need to log in to access it."
			
		case strings.Contains(cause.Title, "cannot be scheduled"):
			friendlyCauses[i].Title = "No room in your cluster"
			friendlyCauses[i].Description = "Imagine trying to park a bus in a compact car spot - your pod needs more resources than any node can provide right now."
			
		case strings.Contains(cause.Title, "keeps crashing"):
			friendlyCauses[i].Title = "Your app crashes on startup"
			friendlyCauses[i].Description = "Something's wrong with your application code or configuration. It starts, then immediately exits with an error."
			
		case strings.Contains(cause.Title, "probe"):
			friendlyCauses[i].Title = "Health checks are failing"
			friendlyCauses[i].Description = "Kubernetes keeps asking 'Are you OK?' but your app isn't responding correctly. This usually means the health check path is wrong or the app is actually unhealthy."
		}
	}
	
	return friendlyCauses
}

// MakeFriendlySolutions creates easy-to-understand solutions
func MakeFriendlySolutions(pod *corev1.Pod, causes []types.RootCause) []types.Solution {
	var solutions []types.Solution
	
	for _, cause := range causes {
		switch {
		case strings.Contains(cause.Title, "memory"):
			solutions = append(solutions, types.Solution{
				Title:       "Give your app more memory",
				Description: "Let's increase the memory limit so your app has room to breathe",
				Commands: []string{
					"# Quick fix - double the memory limit:",
					fmt.Sprintf("kubectl set resources deployment %s --limits=memory=512Mi", getSimpleDeploymentName(pod)),
					"",
					"# Or edit the deployment for more control:",
					fmt.Sprintf("kubectl edit deployment %s", getSimpleDeploymentName(pod)),
				},
				Urgency:    types.SeverityCritical,
				Difficulty: "easy",
				Risk:       "low",
			})
			
		case strings.Contains(cause.Title, "image"):
			solutions = append(solutions, types.Solution{
				Title:       "Fix the image name or add credentials",
				Description: "Let's get the right image name and make sure you can access it",
				Commands: []string{
					"# First, check what image you're trying to use:",
					fmt.Sprintf("kubectl describe pod %s | grep Image:", pod.Name),
					"",
					"# If it's a private registry, add credentials:",
					"kubectl create secret docker-registry my-registry-key \\",
					"  --docker-server=YOUR_REGISTRY \\",
					"  --docker-username=YOUR_USERNAME \\",
					"  --docker-password=YOUR_PASSWORD",
					"",
					"# Then update your deployment to use it:",
					fmt.Sprintf("kubectl patch deployment %s -p '{\"spec\":{\"template\":{\"spec\":{\"imagePullSecrets\":[{\"name\":\"my-registry-key\"}]}}}}'", getSimpleDeploymentName(pod)),
				},
				Urgency:    types.SeverityCritical,
				Difficulty: "medium",
				Risk:       "low",
			})
			
		case strings.Contains(cause.Title, "scheduling"):
			solutions = append(solutions, types.Solution{
				Title:       "Make room or reduce requirements",
				Description: "Either add more nodes or ask for less resources",
				Commands: []string{
					"# Option 1: See what resources your pod wants:",
					fmt.Sprintf("kubectl describe pod %s | grep -A5 Requests:", pod.Name),
					"",
					"# Option 2: Reduce the resource requests:",
					fmt.Sprintf("kubectl set resources deployment %s --requests=memory=100Mi,cpu=100m", getSimpleDeploymentName(pod)),
					"",
					"# Option 3: Check if your nodes have capacity:",
					"kubectl top nodes",
				},
				Urgency:    types.SeverityCritical,
				Difficulty: "medium",
				Risk:       "low",
			})
		}
	}
	
	// Always add a "need more help?" option
	solutions = append(solutions, types.Solution{
		Title:       "Need more details?",
		Description: "Here's how to dig deeper into the problem",
		Commands: []string{
			fmt.Sprintf("kubectl describe pod %s", pod.Name),
			fmt.Sprintf("kubectl logs %s --previous", pod.Name),
			fmt.Sprintf("kubectl get events --field-selector involvedObject.name=%s", pod.Name),
		},
		Urgency:    types.SeverityHealthy,
		Difficulty: "easy",
		Risk:       "low",
	})
	
	return solutions
}

// getSimpleDeploymentName tries to extract deployment name from pod
func getSimpleDeploymentName(pod *corev1.Pod) string {
	// Most pods follow pattern: deployment-name-hash-hash
	parts := strings.Split(pod.Name, "-")
	if len(parts) >= 3 {
		return strings.Join(parts[:len(parts)-2], "-")
	}
	return "YOUR-DEPLOYMENT"
}

// MakeFriendlyAnalysis creates a more conversational analysis section
func MakeFriendlyAnalysis(k8sView *types.KubernetesView, reality *types.RealityCheck) map[string]string {
	analysis := make(map[string]string)
	
	// Status explanation
	switch k8sView.Status {
	case "Running":
		analysis["status"] = "Kubernetes thinks everything is fine"
	case "Pending":
		analysis["status"] = "Your pod is waiting to start"
	case "Failed":
		analysis["status"] = "Your pod crashed and won't restart"
	case "Unknown":
		analysis["status"] = "Kubernetes lost track of your pod"
	default:
		analysis["status"] = fmt.Sprintf("Your pod is %s", strings.ToLower(k8sView.Status))
	}
	
	// Memory explanation
	if memLimit, ok := k8sView.Resources["memory_limit"]; ok {
		analysis["memory"] = fmt.Sprintf("Memory limit set to %s", memLimit)
	}
	
	// Restart explanation
	if reality.RestartPattern != "" {
		restartCount := extractRestartCount(reality.RestartPattern)
		if restartCount > 10 {
			analysis["restarts"] = fmt.Sprintf("It's been restarting like crazy (%d times!)", restartCount)
		} else if restartCount > 5 {
			analysis["restarts"] = fmt.Sprintf("It's restarted %d times - something's definitely wrong", restartCount)
		} else if restartCount > 0 {
			analysis["restarts"] = fmt.Sprintf("It's restarted %d times", restartCount)
		}
	}
	
	return analysis
}

func extractRestartCount(pattern string) int {
	// Extract number from "Container restarted X times"
	var count int
	fmt.Sscanf(pattern, "Container restarted %d times", &count)
	return count
}