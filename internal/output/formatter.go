package output

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/falseyair/tapio/pkg/types"
)

// Formatter interface for different output formats
type Formatter interface {
	Print(result *types.CheckResult) error
}

// NewFormatter creates a formatter based on the specified format
func NewFormatter(format string) Formatter {
	switch format {
	case "json":
		return &JSONFormatter{}
	case "yaml":
		return &YAMLFormatter{}
	default:
		return NewHumanFormatter()
	}
}

// JSONFormatter outputs results as JSON
type JSONFormatter struct{}

func (f *JSONFormatter) Print(result *types.CheckResult) error {
	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal result to JSON: %w", err)
	}
	fmt.Println(string(output))
	return nil
}

// YAMLFormatter outputs results as YAML
type YAMLFormatter struct{}

func (f *YAMLFormatter) Print(result *types.CheckResult) error {
	output, err := yaml.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result to YAML: %w", err)
	}
	fmt.Println(string(output))
	return nil
}