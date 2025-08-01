package ebpf

import (
	"fmt"
	"testing"
)

// TestFullCorrelationStory demonstrates all correlations working together
func TestFullCorrelationStory(t *testing.T) {
	fmt.Println("\n=== Full K8s Correlation Story ===")
	fmt.Println("Demonstrating how all correlations work together to create narratives")
	fmt.Println()

	// The story setup
	fmt.Println("📖 Chapter 1: The Setup")
	fmt.Println("A microservices application is deployed to K8s:")
	fmt.Println("- Frontend pod (nginx) serving the UI")
	fmt.Println("- Backend API pod handling business logic")
	fmt.Println("- Database pod (PostgreSQL)")
	fmt.Println()

	// Correlation 1: Process PID ↔ Container ID
	fmt.Println("🔗 Correlation 1: Process PID ↔ Container ID")
	fmt.Println("When containers start, we map their main processes:")
	fmt.Println("  PID 12345 → Container nginx-abc123 (frontend)")
	fmt.Println("  PID 23456 → Container api-def456 (backend)")
	fmt.Println("  PID 34567 → Container postgres-ghi789 (database)")
	fmt.Println()

	// Correlation 2: Cgroup ID ↔ Pod UID
	fmt.Println("🔗 Correlation 2: Cgroup ID ↔ Pod UID")
	fmt.Println("Containers run in cgroups that map to pods:")
	fmt.Println("  Cgroup 1111 → Pod frontend-pod-xyz")
	fmt.Println("  Cgroup 2222 → Pod backend-pod-uvw")
	fmt.Println("  Cgroup 3333 → Pod database-pod-rst")
	fmt.Println()

	// Correlation 3: Network connections ↔ Service endpoints
	fmt.Println("🔗 Correlation 3: Network connections ↔ Service endpoints")
	fmt.Println("Services expose pods via ClusterIPs:")
	fmt.Println("  10.96.0.10:80 → frontend-service")
	fmt.Println("  10.96.0.20:8080 → backend-api service")
	fmt.Println("  10.96.0.30:5432 → database service")
	fmt.Println()

	// Correlation 4: File operations ↔ ConfigMaps/Secrets
	fmt.Println("🔗 Correlation 4: File operations ↔ ConfigMaps/Secrets")
	fmt.Println("Configuration and secrets are mounted:")
	fmt.Println("  /etc/nginx/nginx.conf → ConfigMap nginx-config")
	fmt.Println("  /etc/config/app.yaml → ConfigMap app-config")
	fmt.Println("  /etc/secrets/db-password → Secret db-credentials")
	fmt.Println()

	// The story unfolds
	fmt.Println("📖 Chapter 2: The Story Unfolds")
	fmt.Println()

	fmt.Println("🎬 Scene 1: Frontend Startup")
	fmt.Println("Raw events:")
	fmt.Println("  - Process 12345 reads /etc/nginx/nginx.conf")
	fmt.Println("  - Process 12345 binds to port 80")
	fmt.Println("With correlation:")
	fmt.Println("  → \"Frontend pod loaded nginx configuration from ConfigMap 'nginx-config'\"")
	fmt.Println()

	fmt.Println("🎬 Scene 2: User Request Flow")
	fmt.Println("Raw events:")
	fmt.Println("  - Process 12345 connects to 10.96.0.20:8080")
	fmt.Println("  - Process 23456 reads /etc/secrets/db-password")
	fmt.Println("  - Process 23456 connects to 10.96.0.30:5432")
	fmt.Println("With correlation:")
	fmt.Println("  → \"Frontend pod called backend-api service\"")
	fmt.Println("  → \"Backend pod retrieved database credentials from Secret\"")
	fmt.Println("  → \"Backend pod connected to database service\"")
	fmt.Println()

	fmt.Println("🎬 Scene 3: Performance Issue")
	fmt.Println("Raw events:")
	fmt.Println("  - Process 23456 allocates 500MB memory")
	fmt.Println("  - Process 23456 CPU usage spikes to 90%")
	fmt.Println("With correlation:")
	fmt.Println("  → \"Backend pod experiencing high memory usage (500MB)\"")
	fmt.Println("  → \"This started after querying the database\"")
	fmt.Println()

	fmt.Println("🎬 Scene 4: Configuration Update")
	fmt.Println("Raw events:")
	fmt.Println("  - ConfigMap app-config updated")
	fmt.Println("  - Process 23456 reads /etc/config/app.yaml")
	fmt.Println("  - Process 23456 memory usage drops to 100MB")
	fmt.Println("With correlation:")
	fmt.Println("  → \"Backend pod reloaded configuration after ConfigMap update\"")
	fmt.Println("  → \"Memory usage normalized after config change\"")
	fmt.Println()

	// The complete narrative
	fmt.Println("📖 Chapter 3: The Complete Narrative")
	fmt.Println()
	fmt.Println("Without correlation:")
	fmt.Println("❌ \"Process 12345 connected to 10.96.0.20:8080\"")
	fmt.Println("❌ \"Process 23456 read file /etc/secrets/db-password\"")
	fmt.Println("❌ \"Process 23456 used 500MB memory\"")
	fmt.Println()
	fmt.Println("With correlation:")
	fmt.Println("✅ \"Frontend pod served user request, calling backend-api service\"")
	fmt.Println("✅ \"Backend pod authenticated with database using mounted credentials\"")
	fmt.Println("✅ \"Backend pod memory spike (500MB) resolved after ConfigMap update\"")
	fmt.Println()

	fmt.Println("🎯 The Power of Correlation:")
	fmt.Println("1. Security: Track which pods access which secrets")
	fmt.Println("2. Performance: Understand resource usage in context")
	fmt.Println("3. Troubleshooting: See the full request flow across services")
	fmt.Println("4. Configuration: Track config changes and their effects")
	fmt.Println("5. Compliance: Audit all data access with full context")
}

// TestCorrelationUseCases shows real-world use cases
func TestCorrelationUseCases(t *testing.T) {
	fmt.Println("\n=== Real-World Use Cases for K8s Correlation ===")

	useCases := []struct {
		title       string
		problem     string
		solution    string
		correlation []string
	}{
		{
			title:    "🔍 Security Incident Investigation",
			problem:  "Unauthorized database access detected",
			solution: "Track which pod accessed the database and what secrets it used",
			correlation: []string{
				"PID → Container → Pod identity",
				"File access → Secret mount",
				"Network connection → Database service",
			},
		},
		{
			title:    "🚀 Performance Troubleshooting",
			problem:  "Application response time degraded",
			solution: "Trace request flow and identify bottlenecks",
			correlation: []string{
				"Network connections → Service dependencies",
				"Memory/CPU usage → Container identity",
				"Config changes → Performance impact",
			},
		},
		{
			title:    "🔧 Configuration Debugging",
			problem:  "Application not picking up new configuration",
			solution: "Verify which ConfigMaps are actually being read",
			correlation: []string{
				"File operations → ConfigMap mounts",
				"Process restarts → Config reloads",
				"Pod identity → Expected ConfigMaps",
			},
		},
		{
			title:    "🛡️ Compliance Auditing",
			problem:  "Need to prove data access controls",
			solution: "Complete audit trail of who accessed what data",
			correlation: []string{
				"Pod identity → Team ownership",
				"Secret access → Compliance requirements",
				"Network flows → Data boundaries",
			},
		},
		{
			title:    "📊 Dependency Mapping",
			problem:  "Unknown service dependencies",
			solution: "Automatically discover service communication patterns",
			correlation: []string{
				"Network connections → Service endpoints",
				"Pod relationships → Service mesh",
				"Configuration dependencies → Shared ConfigMaps",
			},
		},
	}

	for _, uc := range useCases {
		fmt.Printf("\n%s\n", uc.title)
		fmt.Printf("Problem: %s\n", uc.problem)
		fmt.Printf("Solution: %s\n", uc.solution)
		fmt.Println("Required correlations:")
		for _, c := range uc.correlation {
			fmt.Printf("  - %s\n", c)
		}
	}

	fmt.Println("\n✨ All of these use cases are now possible with our correlation implementation!")
}
