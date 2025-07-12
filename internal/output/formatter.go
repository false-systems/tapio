package output

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/yairfalse/tapio/pkg/types"
)

// Formatter handles different output formats
type Formatter struct {
	format string
}

// NewFormatter creates a new formatter for the specified format
func NewFormatter(format string) *Formatter {
	return &Formatter{format: format}
}

// Print prints a check result in the specified format
func (f *Formatter) Print(result *types.CheckResult) error {
	switch f.format {
	case "json":
		return f.printJSON(result)
	case "yaml":
		return f.printYAML(result)
	default:
		humanFormatter := NewHumanFormatter()
		return humanFormatter.Print(result)
	}
}

// PrintExplanation prints an explanation in the specified format
func (f *Formatter) PrintExplanation(explanation *types.Explanation) error {
	switch f.format {
	case "json":
		return f.printExplanationJSON(explanation)
	case "yaml":
		return f.printExplanationYAML(explanation)
	default:
		humanFormatter := NewHumanFormatter()
		return humanFormatter.PrintExplanation(explanation)
	}
}

func (f *Formatter) printJSON(result *types.CheckResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func (f *Formatter) printYAML(result *types.CheckResult) error {
	data, err := yaml.Marshal(result)
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func (f *Formatter) printExplanationJSON(explanation *types.Explanation) error {
	data, err := json.MarshalIndent(explanation, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func (f *Formatter) printExplanationYAML(explanation *types.Explanation) error {
	data, err := yaml.Marshal(explanation)
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}
