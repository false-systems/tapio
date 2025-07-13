package opinions

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"
)

// MarkdownParser parses markdown documents into structured format
type MarkdownParser struct {
	patterns map[string]*regexp.Regexp
}

// NewMarkdownParser creates a new markdown parser
func NewMarkdownParser() *MarkdownParser {
	return &MarkdownParser{
		patterns: map[string]*regexp.Regexp{
			"heading":     regexp.MustCompile(`^(#{1,6})\s+(.+)$`),
			"list_item":   regexp.MustCompile(`^[-*+]\s+(.+)$`),
			"code_block":  regexp.MustCompile("^```(\\w*)$"),
			"table_row":   regexp.MustCompile(`^\|(.+)\|$`),
			"yaml_key":    regexp.MustCompile(`^\s*(\w+):\s*(.+)$`),
			"bold":        regexp.MustCompile(`\*\*([^*]+)\*\*`),
			"code_inline": regexp.MustCompile("`([^`]+)`"),
			"metadata":    regexp.MustCompile(`^(\w+):\s*(.+)$`),
		},
	}
}

// Parse converts markdown text into a structured document
func (p *MarkdownParser) Parse(markdown string) (*MarkdownDocument, error) {
	doc := &MarkdownDocument{
		Metadata: make(map[string]string),
		Sections: make([]*Section, 0),
	}

	scanner := bufio.NewScanner(strings.NewReader(markdown))
	var currentSection *Section
	var inCodeBlock bool
	var codeBlockContent strings.Builder
	var currentTable *Table

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Handle code blocks
		if p.patterns["code_block"].MatchString(line) {
			if inCodeBlock {
				// End of code block
				if currentSection != nil {
					currentSection.Content = append(currentSection.Content, ContentBlock{
						Type: "code",
						Code: strings.TrimSpace(codeBlockContent.String()),
					})
				}
				codeBlockContent.Reset()
				inCodeBlock = false
			} else {
				// Start of code block
				inCodeBlock = true
			}
			continue
		}

		if inCodeBlock {
			codeBlockContent.WriteString(line + "\n")
			continue
		}

		// Handle headings
		if matches := p.patterns["heading"].FindStringSubmatch(line); len(matches) > 0 {
			level := len(matches[1])
			title := strings.TrimSpace(matches[2])

			// First heading is the document title
			if doc.Title == "" && level == 1 {
				doc.Title = title
				continue
			}

			// Create new section
			currentSection = &Section{
				Level:   level,
				Title:   title,
				Content: make([]ContentBlock, 0),
			}
			doc.Sections = append(doc.Sections, currentSection)
			currentTable = nil
			continue
		}

		// Handle metadata (key: value at document start)
		if currentSection == nil && strings.TrimSpace(line) != "" {
			if matches := p.patterns["metadata"].FindStringSubmatch(line); len(matches) > 0 {
				key := strings.ToLower(strings.TrimSpace(matches[1]))
				value := strings.TrimSpace(matches[2])
				doc.Metadata[key] = value
				continue
			}
		}

		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			currentTable = nil
			continue
		}

		// Handle table rows
		if strings.HasPrefix(line, "|") && strings.HasSuffix(line, "|") {
			p.parseTableRow(line, currentSection, &currentTable)
			continue
		}

		// Handle list items
		if matches := p.patterns["list_item"].FindStringSubmatch(line); len(matches) > 0 {
			item := strings.TrimSpace(matches[1])
			
			// Check if this list item continues a previous list
			if currentSection != nil && len(currentSection.Content) > 0 {
				lastBlock := &currentSection.Content[len(currentSection.Content)-1]
				if lastBlock.Type == "list" {
					lastBlock.Items = append(lastBlock.Items, item)
					continue
				}
			}

			// Start new list
			if currentSection != nil {
				currentSection.Content = append(currentSection.Content, ContentBlock{
					Type:  "list",
					Items: []string{item},
				})
			}
			continue
		}

		// Default: paragraph
		if currentSection != nil && strings.TrimSpace(line) != "" {
			currentSection.Content = append(currentSection.Content, ContentBlock{
				Type: "paragraph",
				Text: line,
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning markdown: %w", err)
	}

	return doc, nil
}

// parseTableRow handles markdown table parsing
func (p *MarkdownParser) parseTableRow(line string, section *Section, currentTable **Table) {
	if section == nil {
		return
	}

	// Remove leading and trailing pipes
	line = strings.Trim(line, "|")
	cells := strings.Split(line, "|")
	
	// Clean cells
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}

	// Check if this is a separator row
	isSeparator := true
	for _, cell := range cells {
		if cell != "" && !strings.HasPrefix(cell, "---") && !strings.HasPrefix(cell, "===") {
			isSeparator = false
			break
		}
	}

	if isSeparator {
		return // Skip separator rows
	}

	// Initialize table if needed
	if *currentTable == nil {
		*currentTable = &Table{
			Headers: cells,
			Rows:    make([][]string, 0),
		}
		
		// Find last content block
		if len(section.Content) > 0 {
			lastIdx := len(section.Content) - 1
			if section.Content[lastIdx].Type == "table" {
				// Replace with new table
				section.Content[lastIdx].Table = *currentTable
				return
			}
		}
		
		// Add new table block
		section.Content = append(section.Content, ContentBlock{
			Type:  "table",
			Table: *currentTable,
		})
		return
	}

	// Add row to existing table
	(*currentTable).Rows = append((*currentTable).Rows, cells)
	
	// Update the table in the content block
	for i := len(section.Content) - 1; i >= 0; i-- {
		if section.Content[i].Type == "table" {
			section.Content[i].Table = *currentTable
			break
		}
	}
}

// ExtractValue extracts structured values from text
func (p *MarkdownParser) ExtractValue(text string) interface{} {
	// Clean the text
	text = strings.TrimSpace(text)
	
	// Extract from bold text
	if matches := p.patterns["bold"].FindStringSubmatch(text); len(matches) > 0 {
		text = matches[1]
	}
	
	// Extract from inline code
	if matches := p.patterns["code_inline"].FindStringSubmatch(text); len(matches) > 0 {
		text = matches[1]
	}
	
	return text
}

// GetSectionByTitle finds a section by its title
func (d *MarkdownDocument) GetSectionByTitle(title string) *Section {
	lowerTitle := strings.ToLower(title)
	for _, section := range d.Sections {
		if strings.ToLower(section.Title) == lowerTitle {
			return section
		}
		// Also check if title contains the search term
		if strings.Contains(strings.ToLower(section.Title), lowerTitle) {
			return section
		}
	}
	return nil
}

// GetContentText extracts all text from content blocks
func (s *Section) GetContentText() string {
	var text strings.Builder
	
	for _, block := range s.Content {
		switch block.Type {
		case "paragraph":
			text.WriteString(block.Text + "\n")
		case "list":
			for _, item := range block.Items {
				text.WriteString("- " + item + "\n")
			}
		case "code":
			text.WriteString("```\n" + block.Code + "\n```\n")
		}
		text.WriteString("\n")
	}
	
	return text.String()
}

// FindCodeBlock finds the first code block in a section
func (s *Section) FindCodeBlock() *ContentBlock {
	for _, block := range s.Content {
		if block.Type == "code" {
			return &block
		}
	}
	return nil
}

// FindTable finds the first table in a section
func (s *Section) FindTable() *Table {
	for _, block := range s.Content {
		if block.Type == "table" && block.Table != nil {
			return block.Table
		}
	}
	return nil
}

// ParseEmphasis extracts emphasized values (bold, code) from text
func ParseEmphasis(text string) []string {
	var values []string
	
	// Find bold text
	boldPattern := regexp.MustCompile(`\*\*([^*]+)\*\*`)
	for _, match := range boldPattern.FindAllStringSubmatch(text, -1) {
		if len(match) > 1 {
			values = append(values, match[1])
		}
	}
	
	// Find inline code
	codePattern := regexp.MustCompile("`([^`]+)`")
	for _, match := range codePattern.FindAllStringSubmatch(text, -1) {
		if len(match) > 1 {
			values = append(values, match[1])
		}
	}
	
	return values
}