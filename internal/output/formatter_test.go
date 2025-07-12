package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
	"github.com/yairfalse/tapio/pkg/types"
)

func TestFormatter(t *testing.T) {
	// Disable color for consistent test output
	color.NoColor = true

	tests := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{"TestBasicFormatting", testBasicFormatting},
		{"TestSeverityLevels", testSeverityLevels},
		{"TestIndentation", testIndentation},
		{"TestTextWrapping", testTextWrapping},
		{"TestNoEmoji", testNoEmoji},
		{"TestQuietMode", testQuietMode},
		{"TestHeadingsAndSeparators", testHeadingsAndSeparators},
		{"TestCommandFormatting", testCommandFormatting},
		{"TestNextSteps", testNextSteps},
		{"TestJSONOutput", testJSONOutput},
		{"TestYAMLOutput", testYAMLOutput},
		{"TestTableFormatting", testTableFormatting},
		{"TestProgressBar", testProgressBar},
		{"TestStepProgress", testStepProgress},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func testBasicFormatting(t *testing.T) {
	var buf bytes.Buffer
	config := &Config{
		Format: FormatHuman,
		Writer: &buf,
	}
	f := NewFormatter(config)

	f.Status(SeverityInfo, "Test message")
	output := buf.String()

	if !strings.Contains(output, "Test message") {
		t.Errorf("Expected output to contain 'Test message', got: %s", output)
	}
}

func testSeverityLevels(t *testing.T) {
	severities := []struct {
		severity Severity
		icon     string
		textIcon string
	}{
		{SeverityCritical, Icons.Critical, "[CRIT]"},
		{SeverityError, Icons.Error, "[ERR] "},
		{SeverityWarning, Icons.Warning, "[WARN]"},
		{SeverityInfo, Icons.Info, "[INFO]"},
		{SeveritySuccess, Icons.Success, "[OK]  "},
		{SeverityDebug, Icons.Debug, "[DBG] "},
	}

	for _, s := range severities {
		t.Run(string(s.severity), func(t *testing.T) {
			// Test with emoji
			var buf bytes.Buffer
			f := NewFormatter(&Config{
				Format: FormatHuman,
				Writer: &buf,
			})
			f.Status(s.severity, "Test")

			if !strings.Contains(buf.String(), s.icon) {
				t.Errorf("Expected icon %s for severity %s", s.icon, s.severity)
			}

			// Test without emoji
			buf.Reset()
			f = NewFormatter(&Config{
				Format:  FormatHuman,
				Writer:  &buf,
				NoEmoji: true,
			})
			f.Status(s.severity, "Test")

			if !strings.Contains(buf.String(), s.textIcon) {
				t.Errorf("Expected text icon %s for severity %s", s.textIcon, s.severity)
			}
		})
	}
}

func testIndentation(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatter(&Config{
		Format: FormatHuman,
		Writer: &buf,
	})

	f.Status(SeverityInfo, "Level 0")
	f.Indent()
	f.Status(SeverityInfo, "Level 1")
	f.Indent()
	f.Status(SeverityInfo, "Level 2")
	f.Outdent()
	f.Status(SeverityInfo, "Level 1 again")
	f.Outdent()
	f.Status(SeverityInfo, "Level 0 again")

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	if len(lines) != 5 {
		t.Fatalf("Expected 5 lines, got %d", len(lines))
	}

	// Check indentation
	if !strings.HasPrefix(lines[1], "  ") || strings.HasPrefix(lines[1], "    ") {
		t.Error("Level 1 should have 2 spaces indent")
	}
	if !strings.HasPrefix(lines[2], "    ") {
		t.Error("Level 2 should have 4 spaces indent")
	}
}

func testTextWrapping(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatter(&Config{
		Format:        FormatHuman,
		Writer:        &buf,
		TerminalWidth: 30, // Small width to force wrapping
	})

	longText := "This is a very long message that should be wrapped to multiple lines"
	f.Status(SeverityInfo, longText)

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	if len(lines) < 2 {
		t.Error("Long text should be wrapped to multiple lines")
	}

	for _, line := range lines {
		if len(line) > 30 {
			t.Errorf("Line exceeds terminal width: %s", line)
		}
	}
}

func testNoEmoji(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatter(&Config{
		Format:  FormatHuman,
		Writer:  &buf,
		NoEmoji: true,
	})

	f.Status(SeveritySuccess, "Success message")
	output := buf.String()

	if strings.Contains(output, Icons.Success) {
		t.Error("Output should not contain emoji when NoEmoji is true")
	}
	if !strings.Contains(output, "[OK]") {
		t.Error("Output should contain text icon when NoEmoji is true")
	}
}

func testQuietMode(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatter(&Config{
		Format: FormatHuman,
		Writer: &buf,
		Quiet:  true,
	})

	f.Status(SeverityInfo, "Info message")
	f.Status(SeverityWarning, "Warning message")

	if buf.Len() > 0 {
		t.Error("Quiet mode should suppress info and warning messages")
	}

	f.Status(SeverityError, "Error message")
	f.Status(SeverityCritical, "Critical message")

	output := buf.String()
	if !strings.Contains(output, "Error message") || !strings.Contains(output, "Critical message") {
		t.Error("Quiet mode should still show error and critical messages")
	}
}

func testHeadingsAndSeparators(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatter(&Config{
		Format:        FormatHuman,
		Writer:        &buf,
		TerminalWidth: 40,
	})

	f.Heading("Main Heading")
	f.Subheading("Sub Heading")

	output := buf.String()

	if !strings.Contains(output, "Main Heading") {
		t.Error("Output should contain main heading")
	}
	if !strings.Contains(output, "Sub Heading") {
		t.Error("Output should contain sub heading")
	}
	if !strings.Contains(output, strings.Repeat(Icons.Separator, 40)) {
		t.Error("Output should contain separator line")
	}
}

func testCommandFormatting(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatter(&Config{
		Format: FormatHuman,
		Writer: &buf,
	})

	f.Command("kubectl get pods")
	output := buf.String()

	if !strings.Contains(output, "kubectl get pods") {
		t.Error("Output should contain the command")
	}
}

func testNextSteps(t *testing.T) {
	var buf bytes.Buffer
	f := NewFormatter(&Config{
		Format: FormatHuman,
		Writer: &buf,
	})

	steps := []string{
		"Check the logs",
		"Restart the service",
		"Contact support",
	}

	f.NextSteps(steps)
	output := buf.String()

	for i, step := range steps {
		expected := fmt.Sprintf("%d. %s", i+1, step)
		if !strings.Contains(output, expected) {
			t.Errorf("Output should contain numbered step: %s", expected)
		}
	}
}

func testJSONOutput(t *testing.T) {
	var buf bytes.Buffer
	f := NewJSONFormatter(&buf, true)

	result := &types.CheckResult{
		Summary: types.Summary{
			TotalPods:    10,
			HealthyPods:  7,
			WarningPods:  2,
			CriticalPods: 1,
		},
		Problems: []types.Problem{
			{
				Title:    "Test Problem",
				Severity: types.SeverityWarning,
				Resource: types.ResourceReference{
					Kind: "Pod",
					Name: "test-pod",
				},
			},
		},
	}

	err := f.Print(result)
	if err != nil {
		t.Fatalf("Failed to print JSON: %v", err)
	}

	output := buf.String()

	// Check for valid JSON structure
	if !strings.Contains(output, `"summary"`) {
		t.Error("JSON output should contain summary")
	}
	if !strings.Contains(output, `"total_pods": 10`) {
		t.Error("JSON output should contain total_pods")
	}
	if !strings.Contains(output, `"problems"`) {
		t.Error("JSON output should contain problems")
	}
}

func testYAMLOutput(t *testing.T) {
	var buf bytes.Buffer
	f := NewYAMLFormatter(&buf)

	result := &types.CheckResult{
		Summary: types.Summary{
			TotalPods:    5,
			HealthyPods:  5,
			WarningPods:  0,
			CriticalPods: 0,
		},
		Problems: []types.Problem{},
	}

	err := f.Print(result)
	if err != nil {
		t.Fatalf("Failed to print YAML: %v", err)
	}

	output := buf.String()

	// Check for valid YAML structure
	if !strings.Contains(output, "summary:") {
		t.Error("YAML output should contain summary")
	}
	if !strings.Contains(output, "total_pods: 5") {
		t.Error("YAML output should contain total_pods")
	}
}

func testTableFormatting(t *testing.T) {
	var buf bytes.Buffer
	table := NewTable(&buf, 50)

	config := &TableConfig{
		Headers: []string{"Name", "Status", "Age"},
		Rows: [][]string{
			{"pod-1", "Running", "1d"},
			{"pod-2", "Pending", "5m"},
			{"pod-3", "Failed", "2h"},
		},
	}

	err := table.Render(config)
	if err != nil {
		t.Fatalf("Failed to render table: %v", err)
	}

	output := buf.String()

	// Check headers
	for _, header := range config.Headers {
		if !strings.Contains(output, header) {
			t.Errorf("Table should contain header: %s", header)
		}
	}

	// Check rows
	for _, row := range config.Rows {
		for _, cell := range row {
			if !strings.Contains(output, cell) {
				t.Errorf("Table should contain cell: %s", cell)
			}
		}
	}
}

func testProgressBar(t *testing.T) {
	var buf bytes.Buffer
	pb := NewProgressBar(&buf, 40)

	pb.Start("Testing", 100)
	pb.Update(50, "Halfway")
	pb.Complete("Done")

	output := buf.String()

	// Progress bar should contain progress indicators
	if !strings.Contains(output, "[") || !strings.Contains(output, "]") {
		t.Error("Progress bar should contain brackets")
	}
	if !strings.Contains(output, "50%") {
		t.Error("Progress bar should show percentage")
	}
}

func testStepProgress(t *testing.T) {
	var buf bytes.Buffer
	steps := []string{"Step 1", "Step 2", "Step 3"}
	sp := NewStepProgress(&buf, steps)

	sp.NextStep()
	sp.NextStep()
	sp.Complete()

	output := buf.String()

	// Should contain all steps
	for _, step := range steps {
		if !strings.Contains(output, step) {
			t.Errorf("Step progress should contain: %s", step)
		}
	}

	// Should show completion
	if !strings.Contains(output, "Completed in") {
		t.Error("Step progress should show completion time")
	}
}

// Test error scenarios
func TestFormatterErrors(t *testing.T) {
	t.Run("NilConfig", func(t *testing.T) {
		// Should not panic with nil config
		f := NewFormatter(nil)
		f.Status(SeverityInfo, "Test")
	})

	t.Run("NilWriter", func(t *testing.T) {
		// Should use stdout as default
		f := NewFormatter(&Config{
			Format: FormatHuman,
			Writer: nil,
		})
		f.Status(SeverityInfo, "Test")
	})

	t.Run("InvalidFormat", func(t *testing.T) {
		err := ValidateFormat("invalid")
		if err == nil {
			t.Error("Should return error for invalid format")
		}
	})
}

// Test the human formatter with real types
func TestHumanFormatterIntegration(t *testing.T) {
	var buf bytes.Buffer
	// Need to create formatter that writes to buffer
	f := NewHumanFormatter()
	f.formatter = NewFormatter(&Config{
		Format: FormatHuman,
		Writer: &buf,
	})

	t.Run("PrintExplanation", func(t *testing.T) {
		explanation := &types.Explanation{
			Resource: types.ResourceReference{
				Kind:      "Pod",
				Name:      "test-pod",
				Namespace: "default",
			},
			Summary: "Pod is failing due to resource limits",
			RootCauses: []types.RootCause{
				{
					Title:       "Memory Limit Exceeded",
					Description: "Pod is requesting more memory than available",
					Confidence:  0.85,
					Evidence:    []string{"OOMKilled event", "Memory usage at 99%"},
				},
			},
			Solutions: []types.Solution{
				{
					Title:       "Increase Memory Limit",
					Description: "Update the pod spec to request more memory",
					Commands:    []string{"kubectl edit pod test-pod"},
					Urgency:     types.SeverityCritical,
				},
			},
		}

		err := f.PrintExplanation(explanation)
		if err != nil {
			t.Fatalf("Failed to print explanation: %v", err)
		}

		output := buf.String()

		// Check key components are present
		if !strings.Contains(output, "Analyzing Pod/test-pod") {
			t.Error("Should contain resource being analyzed")
		}
		if !strings.Contains(output, "Memory Limit Exceeded") {
			t.Error("Should contain root cause")
		}
		if !strings.Contains(output, "kubectl edit pod test-pod") {
			t.Error("Should contain solution command")
		}
	})
}

// Benchmark tests
func BenchmarkFormatter(b *testing.B) {
	var buf bytes.Buffer
	f := NewFormatter(&Config{
		Format: FormatHuman,
		Writer: &buf,
	})

	b.Run("Status", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buf.Reset()
			f.Status(SeverityInfo, "Benchmark message %d", i)
		}
	})

	b.Run("TextWrap", func(b *testing.B) {
		longText := strings.Repeat("This is a long message. ", 20)
		for i := 0; i < b.N; i++ {
			f.wrapText(longText, 80)
		}
	})
}

// Examples for documentation
func ExampleFormatter_Status() {
	f := NewFormatter(&Config{
		Format: FormatHuman,
	})

	f.Status(SeveritySuccess, "Operation completed successfully")
	f.Status(SeverityWarning, "Resource usage is high: %d%%", 85)
	f.Status(SeverityError, "Failed to connect to database")
}

func ExampleFormatter_NextSteps() {
	f := NewFormatter(&Config{
		Format: FormatHuman,
	})

	f.NextSteps([]string{
		"Check the pod logs: kubectl logs pod-name",
		"Describe the pod: kubectl describe pod pod-name",
		"Delete and recreate: kubectl delete pod pod-name",
	})
}

func ExampleTable() {
	table := NewTable(os.Stdout, 80)

	table.Render(&TableConfig{
		Headers: []string{"Pod", "Status", "Restarts", "Age"},
		Rows: [][]string{
			{"frontend-abc123", "Running", "0", "2d"},
			{"backend-def456", "CrashLoopBackOff", "5", "1h"},
			{"database-xyz789", "Pending", "0", "5m"},
		},
		Alignment: []Alignment{AlignLeft, AlignCenter, AlignRight, AlignRight},
	})
}
