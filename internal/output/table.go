package output

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// TableConfig configures table display
type TableConfig struct {
	Headers      []string
	Rows         [][]string
	MaxColWidths []int
	MinColWidths []int
	Alignment    []Alignment
	ShowBorders  bool
	CompactMode  bool
}

// Alignment specifies column alignment
type Alignment int

const (
	AlignLeft Alignment = iota
	AlignCenter
	AlignRight
)

// Table provides formatted table output
type Table struct {
	writer        io.Writer
	terminalWidth int
	config        *TableConfig
	colWidths     []int
}

// NewTable creates a new table formatter
func NewTable(writer io.Writer, terminalWidth int) *Table {
	return &Table{
		writer:        writer,
		terminalWidth: terminalWidth,
	}
}

// Render displays the table
func (t *Table) Render(config *TableConfig) error {
	if config == nil || len(config.Headers) == 0 {
		return fmt.Errorf("table configuration is required")
	}

	t.config = config
	t.calculateColumnWidths()

	// Print header
	t.printHeader()

	// Print separator
	if !config.CompactMode {
		t.printSeparator()
	}

	// Print rows
	for _, row := range config.Rows {
		t.printRow(row)
	}

	// Print bottom border if enabled
	if config.ShowBorders {
		t.printBorder()
	}

	return nil
}

// calculateColumnWidths determines optimal column widths
func (t *Table) calculateColumnWidths() {
	numCols := len(t.config.Headers)
	t.colWidths = make([]int, numCols)

	// Start with header widths
	for i, header := range t.config.Headers {
		t.colWidths[i] = len(header)
	}

	// Check all row widths
	for _, row := range t.config.Rows {
		for i, cell := range row {
			if i < numCols && len(cell) > t.colWidths[i] {
				t.colWidths[i] = len(cell)
			}
		}
	}

	// Apply min/max constraints
	for i := range t.colWidths {
		if t.config.MinColWidths != nil && i < len(t.config.MinColWidths) {
			if t.colWidths[i] < t.config.MinColWidths[i] {
				t.colWidths[i] = t.config.MinColWidths[i]
			}
		}
		if t.config.MaxColWidths != nil && i < len(t.config.MaxColWidths) {
			if t.colWidths[i] > t.config.MaxColWidths[i] {
				t.colWidths[i] = t.config.MaxColWidths[i]
			}
		}
	}

	// Adjust for terminal width
	t.fitToTerminal()
}

// fitToTerminal adjusts column widths to fit terminal
func (t *Table) fitToTerminal() {
	// Calculate total width needed
	totalWidth := 0
	for _, w := range t.colWidths {
		totalWidth += w + 3 // Include spacing
	}

	// If table fits, we're done
	if totalWidth <= t.terminalWidth {
		return
	}

	// Scale down proportionally
	scale := float64(t.terminalWidth-len(t.colWidths)*3) / float64(totalWidth-len(t.colWidths)*3)
	for i := range t.colWidths {
		newWidth := int(float64(t.colWidths[i]) * scale)
		if newWidth < 3 {
			newWidth = 3
		}
		t.colWidths[i] = newWidth
	}
}

// printHeader prints the table header
func (t *Table) printHeader() {
	var builder strings.Builder

	if t.config.ShowBorders {
		builder.WriteString("│ ")
	}

	for i, header := range t.config.Headers {
		formatted := t.formatCell(header, i, true)
		builder.WriteString(formatted)

		if i < len(t.config.Headers)-1 {
			if t.config.ShowBorders {
				builder.WriteString(" │ ")
			} else {
				builder.WriteString("  ")
			}
		}
	}

	if t.config.ShowBorders {
		builder.WriteString(" │")
	}

	fmt.Fprintln(t.writer, Colors.Heading(builder.String()))
}

// printSeparator prints a separator line
func (t *Table) printSeparator() {
	var builder strings.Builder

	if t.config.ShowBorders {
		builder.WriteString("├")
	}

	for i, width := range t.colWidths {
		builder.WriteString(strings.Repeat("─", width+2))

		if i < len(t.colWidths)-1 {
			if t.config.ShowBorders {
				builder.WriteString("┼")
			} else {
				builder.WriteString("─")
			}
		}
	}

	if t.config.ShowBorders {
		builder.WriteString("┤")
	}

	fmt.Fprintln(t.writer, builder.String())
}

// printBorder prints a table border
func (t *Table) printBorder() {
	var builder strings.Builder

	builder.WriteString("└")
	for i, width := range t.colWidths {
		builder.WriteString(strings.Repeat("─", width+2))

		if i < len(t.colWidths)-1 {
			builder.WriteString("┴")
		}
	}
	builder.WriteString("┘")

	fmt.Fprintln(t.writer, builder.String())
}

// printRow prints a single row
func (t *Table) printRow(row []string) {
	var builder strings.Builder

	if t.config.ShowBorders {
		builder.WriteString("│ ")
	}

	for i := 0; i < len(t.config.Headers); i++ {
		cell := ""
		if i < len(row) {
			cell = row[i]
		}

		formatted := t.formatCell(cell, i, false)
		builder.WriteString(formatted)

		if i < len(t.config.Headers)-1 {
			if t.config.ShowBorders {
				builder.WriteString(" │ ")
			} else {
				builder.WriteString("  ")
			}
		}
	}

	if t.config.ShowBorders {
		builder.WriteString(" │")
	}

	fmt.Fprintln(t.writer, builder.String())
}

// formatCell formats a cell with proper width and alignment
func (t *Table) formatCell(content string, colIndex int, isHeader bool) string {
	width := t.colWidths[colIndex]

	// Truncate if necessary
	if len(content) > width {
		if width > 3 {
			content = content[:width-3] + "..."
		} else {
			content = content[:width]
		}
	}

	// Apply alignment
	alignment := AlignLeft
	if t.config.Alignment != nil && colIndex < len(t.config.Alignment) {
		alignment = t.config.Alignment[colIndex]
	}

	switch alignment {
	case AlignCenter:
		return t.padCenter(content, width)
	case AlignRight:
		return t.padRight(content, width)
	default:
		return t.padLeft(content, width)
	}
}

// padLeft pads content to the left
func (t *Table) padLeft(content string, width int) string {
	return content + strings.Repeat(" ", width-len(content))
}

// padRight pads content to the right
func (t *Table) padRight(content string, width int) string {
	return strings.Repeat(" ", width-len(content)) + content
}

// padCenter centers content
func (t *Table) padCenter(content string, width int) string {
	totalPadding := width - len(content)
	leftPadding := totalPadding / 2
	rightPadding := totalPadding - leftPadding
	return strings.Repeat(" ", leftPadding) + content + strings.Repeat(" ", rightPadding)
}

// SimpleTable creates a basic table from headers and rows
func SimpleTable(writer io.Writer, headers []string, rows [][]string) {
	termWidth := 80
	if width, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		termWidth = width
	}

	table := NewTable(writer, termWidth)
	config := &TableConfig{
		Headers: headers,
		Rows:    rows,
	}
	table.Render(config)
}

// PrettyTable creates a bordered table
func PrettyTable(writer io.Writer, headers []string, rows [][]string) {
	termWidth := 80
	if width, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		termWidth = width
	}

	table := NewTable(writer, termWidth)
	config := &TableConfig{
		Headers:     headers,
		Rows:        rows,
		ShowBorders: true,
	}
	table.Render(config)
}
