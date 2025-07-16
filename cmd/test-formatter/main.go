package main

import (
	"fmt"

	"github.com/yairfalse/tapio/internal/output"
	"github.com/yairfalse/tapio/pkg/types"
)

func main() {
	fmt.Println("=== Testing Tapio Output Formatter ===\n")

	// Test 1: Human formatting
	fmt.Println("Test 1: Human Format Output")
	fmt.Println("---------------------------")

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
			{
				Title:       "Pod OOMKilled",
				Severity:    types.SeverityCritical,
				Description: "Pod was killed due to out of memory",
				Resource: types.ResourceReference{
					Kind:      "Pod",
					Name:      "database-xyz789",
					Namespace: "default",
				},
				Solutions: []string{"Increase memory limit to 4Gi"},
				Commands:  []string{"kubectl set resources deployment/database --limits=memory=4Gi"},
			},
		},
	}

	humanFormatter := output.NewHumanFormatter()
	err := humanFormatter.Print(checkResult)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}

	// Test 2: JSON formatting
	fmt.Println("\n\nTest 2: JSON Format Output")
	fmt.Println("--------------------------")

	jsonFormatter := output.NewFormatter("json")
	err = jsonFormatter.Print(checkResult)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}

	// Test 3: YAML formatting
	fmt.Println("\n\nTest 3: YAML Format Output")
	fmt.Println("--------------------------")

	yamlFormatter := output.NewFormatter("yaml")
	err = yamlFormatter.Print(checkResult)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}

	// Test 4: Explanation formatting
	fmt.Println("\n\nTest 4: Explanation Output")
	fmt.Println("-------------------------")

	explanation := &types.Explanation{
		Title:       "Why is my pod restarting?",
		Summary:     "Your pod is experiencing memory issues that lead to OOM kills",
		Details: []string{
			"The pod's memory usage has been steadily increasing",
			"It reached the memory limit of 2Gi",
			"Kubernetes killed the pod to protect the node",
			"This pattern has repeated 5 times in the last hour",
		},
		Recommendations: []string{
			"Increase the memory limit to 4Gi",
			"Investigate memory leaks in the application",
			"Add memory monitoring and alerts",
		},
		Resources: []types.ResourceReference{
			{
				Kind:      "Pod",
				Name:      "database-xyz789",
				Namespace: "default",
			},
		},
	}

	err = humanFormatter.PrintExplanation(explanation)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}

	fmt.Println("\n\n=== Formatter Test Complete ===")
}