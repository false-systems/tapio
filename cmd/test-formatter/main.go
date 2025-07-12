package main

import (
	"fmt"
	"os"

	"github.com/yairfalse/tapio/internal/output"
	"github.com/yairfalse/tapio/pkg/types"
)

func main() {
	fmt.Println("=== Testing Tapio Output Formatter ===\n")

	// Test 1: Basic formatting with all severity levels
	fmt.Println("Test 1: Severity Levels")
	fmt.Println("-----------------------")

	formatter := output.NewFormatter(&output.Config{
		Format: output.FormatHuman,
		Writer: os.Stdout,
	})

	formatter.Status(output.SeveritySuccess, "All systems operational")
	formatter.Status(output.SeverityInfo, "Checking 42 resources")
	formatter.Status(output.SeverityDebug, "Debug: Cache hit rate 95%%")
	formatter.Status(output.SeverityWarning, "Pod memory usage at 85%%")
	formatter.Status(output.SeverityError, "Failed to connect to database")
	formatter.Status(output.SeverityCritical, "API server is unreachable!")

	// Test 2: Headings and structure
	fmt.Println("\n\nTest 2: Headings and Structure")
	fmt.Println("------------------------------")

	formatter.Heading("Health Check Results")
	formatter.Subheading("Pod Analysis")

	formatter.Indent()
	formatter.Status(output.SeveritySuccess, "web-frontend: Running")
	formatter.Status(output.SeverityWarning, "api-backend: High CPU (78%%)")
	formatter.Status(output.SeverityError, "database: CrashLoopBackOff")
	formatter.Outdent()

	// Test 3: Commands and next steps
	fmt.Println("\n\nTest 3: Commands and Next Steps")
	fmt.Println("--------------------------------")

	formatter.Subheading("Fix Commands")
	formatter.Command("kubectl rollout restart deployment/api-backend")
	formatter.Command("kubectl delete pod database-xyz789")

	formatter.NextSteps([]string{
		"Review database pod logs for error details",
		"Scale down non-critical services to free CPU",
		"Update resource limits in deployment specs",
	})

	// Test 4: Table formatting
	fmt.Println("\n\nTest 4: Table Display")
	fmt.Println("---------------------")

	table := output.NewTable(os.Stdout, 80)
	table.Render(&output.TableConfig{
		Headers: []string{"Pod Name", "Status", "CPU", "Memory", "Restarts"},
		Rows: [][]string{
			{"web-frontend-abc123", "Running", "25%", "512Mi", "0"},
			{"api-backend-def456", "Running", "78%", "1.2Gi", "2"},
			{"database-xyz789", "Error", "10%", "2Gi", "15"},
			{"cache-server-123", "Pending", "-", "-", "0"},
		},
		Alignment: []output.Alignment{
			output.AlignLeft,
			output.AlignCenter,
			output.AlignRight,
			output.AlignRight,
			output.AlignRight,
		},
	})

	// Test 5: Progress indicators
	fmt.Println("\n\nTest 5: Progress Indicators")
	fmt.Println("---------------------------")

	// Step progress
	steps := []string{
		"Connecting to cluster",
		"Gathering resource data",
		"Analyzing health metrics",
		"Generating report",
	}

	stepProgress := output.NewStepProgress(os.Stdout, steps)
	for i := 0; i < len(steps); i++ {
		stepProgress.NextStep()
	}
	stepProgress.Complete()

	// Test 6: No emoji mode
	fmt.Println("\n\nTest 6: No-Emoji Mode")
	fmt.Println("---------------------")

	noEmojiFormatter := output.NewFormatter(&output.Config{
		Format:  output.FormatHuman,
		Writer:  os.Stdout,
		NoEmoji: true,
	})

	noEmojiFormatter.Status(output.SeveritySuccess, "Text-only success indicator")
	noEmojiFormatter.Status(output.SeverityError, "Text-only error indicator")
	noEmojiFormatter.Status(output.SeverityWarning, "Text-only warning indicator")

	// Test 7: JSON output
	fmt.Println("\n\nTest 7: JSON Output Format")
	fmt.Println("--------------------------")

	jsonFormatter := output.NewFormatter("json")
	checkResult := &types.CheckResult{
		Summary: types.Summary{
			TotalPods:    10,
			HealthyPods:  7,
			WarningPods:  2,
			CriticalPods: 1,
		},
		Problems: []types.Problem{
			{
				Title:       "High Memory Usage",
				Severity:    types.SeverityWarning,
				Description: "Pod is using 85% of allocated memory",
				Resource: types.ResourceReference{
					Kind: "Pod",
					Name: "api-backend",
				},
				Solutions: []string{"Increase memory limit", "Optimize application"},
				Commands:  []string{"kubectl edit deployment api-backend"},
			},
		},
	}

	fmt.Println("```json")
	jsonFormatter.Print(checkResult)
	fmt.Println("```")

	// Test 8: Real-world scenario
	fmt.Println("\n\nTest 8: Real-World Scenario")
	fmt.Println("---------------------------")

	formatter.Heading("Tapio Analysis Complete")

	formatter.Status(output.SeverityCritical, "Found 1 critical issue requiring immediate attention")
	formatter.Indent()
	formatter.Status(output.SeverityError, "database-xyz789: OOMKilled 5 times in last hour")
	formatter.Outdent()

	formatter.Status(output.SeverityWarning, "Found 2 warnings that may impact stability")
	formatter.Indent()
	formatter.Status(output.SeverityWarning, "api-backend: CPU throttling detected")
	formatter.Status(output.SeverityWarning, "cache-server: Persistent volume 90%% full")
	formatter.Outdent()

	formatter.Subheading("Recommended Actions")
	formatter.NextSteps([]string{
		"URGENT: Increase database memory limit to prevent OOM kills",
		"Review api-backend CPU requests and limits",
		"Clean up cache-server persistent volume or increase size",
	})

	formatter.Subheading("Quick Fixes")
	formatter.Command("kubectl set resources deployment/database --limits=memory=4Gi")
	formatter.Command("kubectl exec -it cache-server -- rm -rf /cache/temp/*")

	fmt.Println("\n\n=== Formatter Test Complete ===")
}
