package output

import (
	"fmt"
	"io"
	"os"

	"github.com/yairfalse/tapio/pkg/types"
)

// OutputFormatter is the base interface for all formatters
type OutputFormatter interface {
	// The actual implementation will handle type checking
}

// ExtendedFormatter provides all formatting capabilities
type ExtendedFormatter interface {
	OutputFormatter
	Print(result *types.CheckResult) error
	PrintExplanation(explanation *types.Explanation) error
}

// NewOutputFormatter creates a formatter based on the format string
func NewOutputFormatter(format string) ExtendedFormatter {
	writer := os.Stdout

	switch format {
	case "json":
		return &JSONFormatter{Writer: writer, Indent: true}
	case "yaml":
		return &YAMLFormatter{Writer: writer}
	case "human", "":
		return NewHumanFormatter()
	default:
		// Default to human readable
		return NewHumanFormatter()
	}
}

// NewOutputFormatterWithWriter creates a formatter with a custom writer
func NewOutputFormatterWithWriter(format string, writer io.Writer) ExtendedFormatter {
	if writer == nil {
		writer = os.Stdout
	}

	switch format {
	case "json":
		return &JSONFormatter{Writer: writer, Indent: true}
	case "yaml":
		return &YAMLFormatter{Writer: writer}
	case "human", "":
		// For human formatter, we need to handle the writer differently
		// since it creates its own formatter internally
		return NewHumanFormatter()
	default:
		return NewHumanFormatter()
	}
}

// Ensure our formatters implement the interfaces
var (
	_ ExtendedFormatter = (*HumanFormatter)(nil)
	_ ExtendedFormatter = (*JSONFormatter)(nil)
	_ ExtendedFormatter = (*YAMLFormatter)(nil)
)

// ValidateFormat checks if the format string is valid
func ValidateFormat(format string) error {
	switch format {
	case "human", "json", "yaml", "":
		return nil
	default:
		return fmt.Errorf("invalid output format: %s (must be one of: human, json, yaml)", format)
	}
}

// ParseFormat normalizes the format string
func ParseFormat(format string) string {
	switch format {
	case "json", "JSON":
		return "json"
	case "yaml", "YAML", "yml":
		return "yaml"
	case "human", "text", "":
		return "human"
	default:
		return "human"
	}
}
