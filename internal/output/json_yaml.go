package output

import (
	"encoding/json"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"

	"github.com/yairfalse/tapio/pkg/types"
)

// JSONFormatter formats output as JSON
type JSONFormatter struct {
	Writer io.Writer
	Indent bool
}

// NewJSONFormatter creates a new JSON formatter
func NewJSONFormatter(w io.Writer, indent bool) *JSONFormatter {
	return &JSONFormatter{
		Writer: w,
		Indent: indent,
	}
}

// Print formats and prints check results as JSON
func (f *JSONFormatter) Print(result *types.CheckResult) error {
	// Create a well-structured JSON output
	output := map[string]interface{}{
		"summary": map[string]interface{}{
			"total_pods":    result.Summary.TotalPods,
			"healthy_pods":  result.Summary.HealthyPods,
			"warning_pods":  result.Summary.WarningPods,
			"critical_pods": result.Summary.CriticalPods,
		},
		"problems": f.formatProblems(result.Problems),
	}

	if len(result.QuickFixes) > 0 {
		fixes := make([]map[string]interface{}, 0, len(result.QuickFixes))
		for _, fix := range result.QuickFixes {
			fixes = append(fixes, map[string]interface{}{
				"command":     fix.Command,
				"description": fix.Description,
				"urgency":     string(fix.Urgency),
			})
		}
		output["quick_fixes"] = fixes
	}

	encoder := json.NewEncoder(f.Writer)
	if f.Indent {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(output)
}

// PrintHealthCheck formats and prints health check results as JSON (deprecated compatibility method)
func (f *JSONFormatter) PrintHealthCheck(result *types.CheckResult) error {
	// Use the standard Print method
	return f.Print(result)
}

// PrintExplanation formats and prints explanation as JSON
func (f *JSONFormatter) PrintExplanation(explanation *types.Explanation) error {
	// Create a well-structured JSON output
	output := map[string]interface{}{
		"resource": map[string]interface{}{
			"kind":      explanation.Resource.Kind,
			"name":      explanation.Resource.Name,
			"namespace": explanation.Resource.Namespace,
		},
		"summary":     explanation.Summary,
		"root_causes": f.formatRootCauses(explanation.RootCauses),
		"solutions":   f.formatSolutions(explanation.Solutions),
	}

	if explanation.Analysis != nil && explanation.Analysis.RealityCheck != nil {
		output["reality_check"] = map[string]interface{}{
			"actual_memory":   explanation.Analysis.RealityCheck.ActualMemory,
			"restart_pattern": explanation.Analysis.RealityCheck.RestartPattern,
			"error_patterns":  explanation.Analysis.RealityCheck.ErrorPatterns,
		}
	}

	encoder := json.NewEncoder(f.Writer)
	if f.Indent {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(output)
}

func (f *JSONFormatter) formatProblems(problems []types.Problem) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(problems))
	for _, problem := range problems {
		p := map[string]interface{}{
			"title":       problem.Title,
			"severity":    string(problem.Severity),
			"resource":    fmt.Sprintf("%s/%s", problem.Resource.Kind, problem.Resource.Name),
			"description": problem.Description,
		}

		if problem.Prediction != nil {
			p["prediction"] = map[string]interface{}{
				"time_to_failure": problem.Prediction.TimeToFailure.String(),
				"confidence":      problem.Prediction.Confidence,
				"reason":          problem.Prediction.Reason,
			}
		}

		result = append(result, p)
	}
	return result
}

func (f *JSONFormatter) formatRootCauses(causes []types.RootCause) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(causes))
	for _, cause := range causes {
		c := map[string]interface{}{
			"title":       cause.Title,
			"description": cause.Description,
			"confidence":  cause.Confidence,
		}

		if len(cause.Evidence) > 0 {
			c["evidence"] = cause.Evidence
		}

		result = append(result, c)
	}
	return result
}

func (f *JSONFormatter) formatSolutions(solutions []types.Solution) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(solutions))
	for _, solution := range solutions {
		s := map[string]interface{}{
			"title":       solution.Title,
			"description": solution.Description,
			"urgency":     string(solution.Urgency),
		}

		if len(solution.Commands) > 0 {
			s["commands"] = solution.Commands
		}

		result = append(result, s)
	}
	return result
}

// YAMLFormatter formats output as YAML
type YAMLFormatter struct {
	Writer io.Writer
}

// NewYAMLFormatter creates a new YAML formatter
func NewYAMLFormatter(w io.Writer) *YAMLFormatter {
	return &YAMLFormatter{Writer: w}
}

// Print formats and prints check results as YAML
func (f *YAMLFormatter) Print(result *types.CheckResult) error {
	// Create a well-structured YAML output
	output := map[string]interface{}{
		"summary": map[string]interface{}{
			"total_pods":    result.Summary.TotalPods,
			"healthy_pods":  result.Summary.HealthyPods,
			"warning_pods":  result.Summary.WarningPods,
			"critical_pods": result.Summary.CriticalPods,
		},
		"problems": formatProblemsForYAML(result.Problems),
	}

	if len(result.QuickFixes) > 0 {
		fixes := make([]map[string]interface{}, 0, len(result.QuickFixes))
		for _, fix := range result.QuickFixes {
			fixes = append(fixes, map[string]interface{}{
				"command":     fix.Command,
				"description": fix.Description,
				"urgency":     string(fix.Urgency),
			})
		}
		output["quick_fixes"] = fixes
	}

	encoder := yaml.NewEncoder(f.Writer)
	encoder.SetIndent(2)
	return encoder.Encode(output)
}

// PrintHealthCheck formats and prints health check results as YAML (deprecated compatibility method)
func (f *YAMLFormatter) PrintHealthCheck(result *types.CheckResult) error {
	// Use the standard Print method
	return f.Print(result)
}

// PrintExplanation formats and prints explanation as YAML
func (f *YAMLFormatter) PrintExplanation(explanation *types.Explanation) error {
	// Create a well-structured YAML output
	output := map[string]interface{}{
		"resource": map[string]interface{}{
			"kind":      explanation.Resource.Kind,
			"name":      explanation.Resource.Name,
			"namespace": explanation.Resource.Namespace,
		},
		"summary":     explanation.Summary,
		"root_causes": formatRootCausesForYAML(explanation.RootCauses),
		"solutions":   formatSolutionsForYAML(explanation.Solutions),
	}

	if explanation.Analysis != nil && explanation.Analysis.RealityCheck != nil {
		output["reality_check"] = map[string]interface{}{
			"actual_memory":   explanation.Analysis.RealityCheck.ActualMemory,
			"restart_pattern": explanation.Analysis.RealityCheck.RestartPattern,
			"error_patterns":  explanation.Analysis.RealityCheck.ErrorPatterns,
		}
	}

	encoder := yaml.NewEncoder(f.Writer)
	encoder.SetIndent(2)
	return encoder.Encode(output)
}

// Helper functions for YAML formatting (to avoid method receiver issues)

func formatProblemsForYAML(problems []types.Problem) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(problems))
	for _, problem := range problems {
		p := map[string]interface{}{
			"title":       problem.Title,
			"severity":    string(problem.Severity),
			"resource":    fmt.Sprintf("%s/%s", problem.Resource.Kind, problem.Resource.Name),
			"description": problem.Description,
		}

		if problem.Prediction != nil {
			p["prediction"] = map[string]interface{}{
				"time_to_failure": problem.Prediction.TimeToFailure.String(),
				"confidence":      problem.Prediction.Confidence,
				"reason":          problem.Prediction.Reason,
			}
		}

		result = append(result, p)
	}
	return result
}

func formatRootCausesForYAML(causes []types.RootCause) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(causes))
	for _, cause := range causes {
		c := map[string]interface{}{
			"title":       cause.Title,
			"description": cause.Description,
			"confidence":  cause.Confidence,
		}

		if len(cause.Evidence) > 0 {
			c["evidence"] = cause.Evidence
		}

		result = append(result, c)
	}
	return result
}

func formatSolutionsForYAML(solutions []types.Solution) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(solutions))
	for _, solution := range solutions {
		s := map[string]interface{}{
			"title":       solution.Title,
			"description": solution.Description,
			"urgency":     string(solution.Urgency),
		}

		if len(solution.Commands) > 0 {
			s["commands"] = solution.Commands
		}

		result = append(result, s)
	}
	return result
}
