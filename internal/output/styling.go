package output

import (
	"github.com/fatih/color"
)

// Icons contains all icons used throughout the application
var Icons = struct {
	Success    string
	Error      string
	Warning    string
	Info       string
	Critical   string
	Debug      string
	InProgress string
	Clock      string
	Separator  string
}{
	Success:    "✓",
	Error:      "✗",
	Warning:    "⚠",
	Info:       "ℹ",
	Critical:   "🔥",
	Debug:      "🐛",
	InProgress: "⏳",
	Clock:      "⏱",
	Separator:  "─",
}

// Colors contains all color functions used throughout the application
var Colors = struct {
	Success func(a ...interface{}) string
	Error   func(a ...interface{}) string
	Warning func(a ...interface{}) string
	Info    func(a ...interface{}) string
	Heading func(a ...interface{}) string
}{
	Success: color.New(color.FgGreen).SprintFunc(),
	Error:   color.New(color.FgRed).SprintFunc(),
	Warning: color.New(color.FgYellow).SprintFunc(),
	Info:    color.New(color.FgCyan).SprintFunc(),
	Heading: color.New(color.FgWhite, color.Bold).SprintFunc(),
}
