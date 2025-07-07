package output

import (
	"fmt"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/fatih/color"
	"github.com/falseyair/tapio/pkg/health"
)

type Output struct {
	spinner *spinner.Spinner
}

func New() *Output {
	return &Output{}
}

func (o *Output) StartSpinner(message string) {
	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	s.Suffix = " " + message
	s.Color("cyan")
	s.Start()
	o.spinner = s
}

func (o *Output) StopSpinner() {
	if o.spinner != nil {
		o.spinner.Stop()
		o.spinner = nil
	}
}

func (o *Output) Success(message string) {
	green := color.New(color.FgGreen, color.Bold)
	green.Print("✓ ")
	fmt.Println(message)
}

func (o *Output) Error(message string) {
	red := color.New(color.FgRed, color.Bold)
	red.Print("✗ ")
	fmt.Println(message)
}

func (o *Output) Warning(message string) {
	yellow := color.New(color.FgYellow, color.Bold)
	yellow.Print("⚠ ")
	fmt.Println(message)
}

func (o *Output) Info(message string) {
	blue := color.New(color.FgBlue)
	blue.Print("ℹ ")
	fmt.Println(message)
}

func (o *Output) EmptyLine() {
	fmt.Println()
}

func (o *Output) Header(title string) {
	fmt.Println()
	bold := color.New(color.Bold)
	bold.Println(title)
	fmt.Println(strings.Repeat("─", len(title)))
}

func (o *Output) RenderHealthReport(report *health.Report) {
	o.EmptyLine()
	
	// Overall health status
	o.renderOverallHealth(report)
	
	// Namespace summary
	if len(report.Namespaces) > 0 {
		o.Header("Namespace Health")
		for _, ns := range report.Namespaces {
			o.renderNamespaceHealth(ns)
		}
	}
	
	// Pod details
	if len(report.Pods) > 0 {
		o.Header("Pod Status")
		o.renderPodTable(report.Pods)
	}
	
	// Issues summary
	if len(report.Issues) > 0 {
		o.Header("Issues Found")
		for _, issue := range report.Issues {
			o.renderIssue(issue)
		}
	}
	
	o.EmptyLine()
}

func (o *Output) renderOverallHealth(report *health.Report) {
	title := color.New(color.Bold, color.FgWhite).Sprint("Cluster Health Summary")
	fmt.Println(title)
	fmt.Println(strings.Repeat("═", 50))
	
	var statusColor *color.Color
	var statusIcon string
	
	switch report.OverallStatus {
	case health.StatusHealthy:
		statusColor = color.New(color.FgGreen, color.Bold)
		statusIcon = "✓"
	case health.StatusWarning:
		statusColor = color.New(color.FgYellow, color.Bold)
		statusIcon = "⚠"
	case health.StatusCritical:
		statusColor = color.New(color.FgRed, color.Bold)
		statusIcon = "✗"
	default:
		statusColor = color.New(color.FgWhite)
		statusIcon = "?"
	}
	
	fmt.Printf("Status: %s %s\n", statusIcon, statusColor.Sprint(report.OverallStatus))
	fmt.Printf("Checked at: %s\n", report.Timestamp.Format("15:04:05 MST"))
	fmt.Printf("Total Pods: %d\n", report.TotalPods)
	fmt.Printf("Healthy Pods: %d\n", report.HealthyPods)
	
	if report.TotalPods > 0 {
		healthPercentage := float64(report.HealthyPods) / float64(report.TotalPods) * 100
		fmt.Printf("Health Score: %.1f%%\n", healthPercentage)
	}
	
	fmt.Println(strings.Repeat("═", 50))
}

func (o *Output) renderNamespaceHealth(ns health.NamespaceHealth) {
	var statusColor *color.Color
	switch ns.Status {
	case health.StatusHealthy:
		statusColor = color.New(color.FgGreen)
	case health.StatusWarning:
		statusColor = color.New(color.FgYellow)
	case health.StatusCritical:
		statusColor = color.New(color.FgRed)
	default:
		statusColor = color.New(color.FgWhite)
	}
	
	nameColor := color.New(color.Bold)
	fmt.Printf("  %s %s (%d/%d pods healthy)\n",
		statusColor.Sprint("●"),
		nameColor.Sprint(ns.Name),
		ns.HealthyPods,
		ns.TotalPods,
	)
}

func (o *Output) renderPodTable(pods []health.PodHealth) {
	// Table headers
	fmt.Println()
	fmt.Printf("%-40s %-15s %-10s %-20s\n", "Pod", "Status", "Restarts", "Age")
	fmt.Println(strings.Repeat("─", 90))
	
	for _, pod := range pods {
		var statusColor *color.Color
		switch pod.Status {
		case "Running":
			statusColor = color.New(color.FgGreen)
		case "Pending", "ContainerCreating":
			statusColor = color.New(color.FgYellow)
		case "Failed", "Error", "CrashLoopBackOff":
			statusColor = color.New(color.FgRed)
		default:
			statusColor = color.New(color.FgWhite)
		}
		
		podName := pod.Name
		if len(podName) > 38 {
			podName = podName[:35] + "..."
		}
		
		fmt.Printf("%-40s %-15s %-10d %-20s\n",
			podName,
			statusColor.Sprint(pod.Status),
			pod.RestartCount,
			formatDuration(pod.Age),
		)
	}
}

func (o *Output) renderIssue(issue health.Issue) {
	var icon string
	var iconColor *color.Color
	
	switch issue.Severity {
	case health.SeverityCritical:
		icon = "✗"
		iconColor = color.New(color.FgRed, color.Bold)
	case health.SeverityWarning:
		icon = "⚠"
		iconColor = color.New(color.FgYellow, color.Bold)
	case health.SeverityInfo:
		icon = "ℹ"
		iconColor = color.New(color.FgBlue)
	}
	
	fmt.Printf("%s %s\n", iconColor.Sprint(icon), issue.Message)
	if issue.Resource != "" {
		gray := color.New(color.FgHiBlack)
		fmt.Printf("  %s\n", gray.Sprint("Resource: "+issue.Resource))
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	days := int(d.Hours() / 24)
	return fmt.Sprintf("%dd", days)
}