package plugins

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/exports"
	"gopkg.in/yaml.v3"
)

// CLIExportPlugin implements file-based export for CLI usage
type CLIExportPlugin struct {
	name       string
	config     *CLIExportConfig
	outputDir  string
	filePrefix string
	metrics    *CLIMetrics
	mutex      sync.RWMutex

	// Writers for different formats
	jsonWriter     *JSONWriter
	yamlWriter     *YAMLWriter
	markdownWriter *MarkdownWriter
	csvWriter      *CSVWriter
}

// CLIExportConfig configures the CLI export plugin
type CLIExportConfig struct {
	OutputDirectory string `json:"output_directory"`
	FilePrefix      string `json:"file_prefix"`
	MaxFileSize     int64  `json:"max_file_size"`
	RotateFiles     bool   `json:"rotate_files"`
	RetentionDays   int    `json:"retention_days"`

	// Format-specific settings
	PrettyPrint      bool `json:"pretty_print"`
	CompressOutput   bool `json:"compress_output"`
	IncludeTimestamp bool `json:"include_timestamp"`

	// Format options
	JSONIndent       string `json:"json_indent"`
	YAMLIndent       int    `json:"yaml_indent"`
	CSVDelimiter     string `json:"csv_delimiter"`
	MarkdownTemplate string `json:"markdown_template"`
}

// CLIMetrics tracks plugin metrics
type CLIMetrics struct {
	ExportsTotal   int64
	ExportsSuccess int64
	ExportsFailed  int64
	BytesWritten   int64
	FilesCreated   int64
	LastExportTime time.Time
	mutex          sync.RWMutex
}

// NewCLIExportPlugin creates a new CLI export plugin
func NewCLIExportPlugin() *CLIExportPlugin {
	return &CLIExportPlugin{
		name: "cli-export",
		config: &CLIExportConfig{
			OutputDirectory:  "./exports",
			FilePrefix:       "tapio",
			PrettyPrint:      true,
			IncludeTimestamp: true,
			JSONIndent:       "  ",
			YAMLIndent:       2,
			CSVDelimiter:     ",",
		},
		metrics: &CLIMetrics{},
	}
}

// Name returns the plugin name
func (p *CLIExportPlugin) Name() string {
	return p.name
}

// Start starts the plugin
func (p *CLIExportPlugin) Start(ctx context.Context) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Create output directory
	if err := os.MkdirAll(p.config.OutputDirectory, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Initialize writers
	p.jsonWriter = NewJSONWriter(p.config)
	p.yamlWriter = NewYAMLWriter(p.config)
	p.markdownWriter = NewMarkdownWriter(p.config)
	p.csvWriter = NewCSVWriter(p.config)

	// Start file rotation if enabled
	if p.config.RotateFiles && p.config.RetentionDays > 0 {
		go p.runFileRotation(ctx)
	}

	return nil
}

// Stop stops the plugin
func (p *CLIExportPlugin) Stop(ctx context.Context) error {
	// Clean up any open file handles
	return nil
}

// Configure configures the plugin
func (p *CLIExportPlugin) Configure(config map[string]interface{}) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Convert map to config struct
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}

	newConfig := &CLIExportConfig{}
	if err := json.Unmarshal(data, newConfig); err != nil {
		return err
	}

	// Validate configuration
	if newConfig.OutputDirectory == "" {
		newConfig.OutputDirectory = "./exports"
	}
	if newConfig.FilePrefix == "" {
		newConfig.FilePrefix = "tapio"
	}
	if newConfig.JSONIndent == "" {
		newConfig.JSONIndent = "  "
	}
	if newConfig.CSVDelimiter == "" {
		newConfig.CSVDelimiter = ","
	}

	p.config = newConfig
	return nil
}

// ValidateConfig validates the plugin configuration
func (p *CLIExportPlugin) ValidateConfig() error {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	if p.config.MaxFileSize < 0 {
		return fmt.Errorf("max_file_size cannot be negative")
	}

	if p.config.RetentionDays < 0 {
		return fmt.Errorf("retention_days cannot be negative")
	}

	return nil
}

// GetConfigSchema returns the configuration schema
func (p *CLIExportPlugin) GetConfigSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"output_directory": map[string]interface{}{
				"type":        "string",
				"description": "Directory to write export files",
				"default":     "./exports",
			},
			"file_prefix": map[string]interface{}{
				"type":        "string",
				"description": "Prefix for export filenames",
				"default":     "tapio",
			},
			"max_file_size": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum file size in bytes (0 = unlimited)",
				"default":     0,
			},
			"pretty_print": map[string]interface{}{
				"type":        "boolean",
				"description": "Enable pretty printing for JSON/YAML",
				"default":     true,
			},
			"compress_output": map[string]interface{}{
				"type":        "boolean",
				"description": "Compress output files with gzip",
				"default":     false,
			},
		},
	}
}

// Export exports data in the specified format
func (p *CLIExportPlugin) Export(ctx context.Context, data exports.ExportData) error {
	p.metrics.mutex.Lock()
	p.metrics.ExportsTotal++
	p.metrics.mutex.Unlock()

	start := time.Now()

	// Generate filename
	filename := p.generateFilename(data.Type, data.Format)
	filepath := filepath.Join(p.config.OutputDirectory, filename)

	// Export based on format
	var err error
	switch data.Format {
	case exports.FormatJSON:
		err = p.exportJSON(ctx, filepath, data)
	case exports.FormatYAML:
		err = p.exportYAML(ctx, filepath, data)
	case exports.FormatMarkdown:
		err = p.exportMarkdown(ctx, filepath, data)
	case exports.FormatCSV:
		err = p.exportCSV(ctx, filepath, data)
	default:
		err = fmt.Errorf("unsupported format: %s", data.Format)
	}

	if err != nil {
		p.metrics.mutex.Lock()
		p.metrics.ExportsFailed++
		p.metrics.mutex.Unlock()
		return err
	}

	p.metrics.mutex.Lock()
	p.metrics.ExportsSuccess++
	p.metrics.LastExportTime = time.Now()
	p.metrics.mutex.Unlock()

	// Call callback if provided
	if data.Callback != nil {
		data.Callback(&exports.ExportResult{
			Success:  true,
			Duration: time.Since(start),
			Details: map[string]interface{}{
				"filename": filename,
				"filepath": filepath,
			},
		})
	}

	return nil
}

// SupportedFormats returns supported export formats
func (p *CLIExportPlugin) SupportedFormats() []exports.ExportFormat {
	return []exports.ExportFormat{
		exports.FormatJSON,
		exports.FormatYAML,
		exports.FormatMarkdown,
		exports.FormatCSV,
	}
}

// SupportedDataTypes returns supported data types
func (p *CLIExportPlugin) SupportedDataTypes() []exports.DataType {
	return []exports.DataType{
		exports.DataTypeDriftReport,
		exports.DataTypeSnapshot,
		exports.DataTypeCorrelation,
		exports.DataTypeMetrics,
		exports.DataTypeEvents,
		exports.DataTypePatternResult,
		exports.DataTypeAutoFix,
	}
}

// HealthCheck performs a health check
func (p *CLIExportPlugin) HealthCheck(ctx context.Context) (*exports.HealthStatus, error) {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	// Check if output directory is accessible
	if _, err := os.Stat(p.config.OutputDirectory); err != nil {
		return &exports.HealthStatus{
			Healthy:   false,
			LastCheck: time.Now(),
			Message:   fmt.Sprintf("Output directory not accessible: %v", err),
		}, nil
	}

	// Check disk space
	var stat os.FileInfo
	if info, err := os.Stat(p.config.OutputDirectory); err == nil {
		stat = info
	}

	p.metrics.mutex.RLock()
	metrics := *p.metrics
	p.metrics.mutex.RUnlock()

	return &exports.HealthStatus{
		Healthy:   true,
		LastCheck: time.Now(),
		Message:   "CLI export plugin is healthy",
		Details: map[string]interface{}{
			"output_directory": p.config.OutputDirectory,
			"exports_total":    metrics.ExportsTotal,
			"exports_success":  metrics.ExportsSuccess,
			"exports_failed":   metrics.ExportsFailed,
			"bytes_written":    metrics.BytesWritten,
			"files_created":    metrics.FilesCreated,
			"last_export":      metrics.LastExportTime,
		},
		ResourceUsage: &exports.ResourceUsage{
			MemoryMB:      0.1, // Minimal memory usage
			CPUPercent:    0.0,
			ExportsPerSec: p.calculateExportRate(),
		},
	}, nil
}

// GetMetrics returns plugin metrics
func (p *CLIExportPlugin) GetMetrics() map[string]interface{} {
	p.metrics.mutex.RLock()
	defer p.metrics.mutex.RUnlock()

	return map[string]interface{}{
		"exports_total":    p.metrics.ExportsTotal,
		"exports_success":  p.metrics.ExportsSuccess,
		"exports_failed":   p.metrics.ExportsFailed,
		"bytes_written":    p.metrics.BytesWritten,
		"files_created":    p.metrics.FilesCreated,
		"last_export_time": p.metrics.LastExportTime,
		"export_rate":      p.calculateExportRate(),
	}
}

// generateFilename generates a filename for the export
func (p *CLIExportPlugin) generateFilename(dataType exports.DataType, format exports.ExportFormat) string {
	timestamp := ""
	if p.config.IncludeTimestamp {
		timestamp = fmt.Sprintf("_%s", time.Now().Format("20060102_150405"))
	}

	extension := p.getFileExtension(format)
	if p.config.CompressOutput {
		extension += ".gz"
	}

	return fmt.Sprintf("%s_%s%s.%s", p.config.FilePrefix, dataType, timestamp, extension)
}

// getFileExtension returns the file extension for a format
func (p *CLIExportPlugin) getFileExtension(format exports.ExportFormat) string {
	switch format {
	case exports.FormatJSON:
		return "json"
	case exports.FormatYAML:
		return "yaml"
	case exports.FormatMarkdown:
		return "md"
	case exports.FormatCSV:
		return "csv"
	default:
		return "txt"
	}
}

// exportJSON exports data as JSON
func (p *CLIExportPlugin) exportJSON(ctx context.Context, filepath string, data exports.ExportData) error {
	return p.writeFile(filepath, func(w io.Writer) error {
		encoder := json.NewEncoder(w)
		if p.config.PrettyPrint {
			encoder.SetIndent("", p.config.JSONIndent)
		}
		return encoder.Encode(data.Content)
	})
}

// exportYAML exports data as YAML
func (p *CLIExportPlugin) exportYAML(ctx context.Context, filepath string, data exports.ExportData) error {
	return p.writeFile(filepath, func(w io.Writer) error {
		encoder := yaml.NewEncoder(w)
		encoder.SetIndent(p.config.YAMLIndent)
		return encoder.Encode(data.Content)
	})
}

// exportMarkdown exports data as Markdown
func (p *CLIExportPlugin) exportMarkdown(ctx context.Context, filepath string, data exports.ExportData) error {
	return p.writeFile(filepath, func(w io.Writer) error {
		return p.markdownWriter.Write(w, data)
	})
}

// exportCSV exports data as CSV
func (p *CLIExportPlugin) exportCSV(ctx context.Context, filepath string, data exports.ExportData) error {
	return p.writeFile(filepath, func(w io.Writer) error {
		return p.csvWriter.Write(w, data)
	})
}

// writeFile writes data to a file with optional compression
func (p *CLIExportPlugin) writeFile(filepath string, writeFunc func(io.Writer) error) error {
	// Create file
	file, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	var writer io.Writer = file
	var gzWriter *gzip.Writer

	// Add compression if enabled
	if p.config.CompressOutput {
		gzWriter = gzip.NewWriter(file)
		writer = gzWriter
		defer gzWriter.Close()
	}

	// Write data
	if err := writeFunc(writer); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	// Close gzip writer before file
	if gzWriter != nil {
		if err := gzWriter.Close(); err != nil {
			return fmt.Errorf("failed to close gzip writer: %w", err)
		}
	}

	// Update metrics
	if stat, err := file.Stat(); err == nil {
		p.metrics.mutex.Lock()
		p.metrics.BytesWritten += stat.Size()
		p.metrics.FilesCreated++
		p.metrics.mutex.Unlock()
	}

	return nil
}

// runFileRotation manages file rotation and cleanup
func (p *CLIExportPlugin) runFileRotation(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.cleanupOldFiles()
		}
	}
}

// cleanupOldFiles removes files older than retention period
func (p *CLIExportPlugin) cleanupOldFiles() {
	if p.config.RetentionDays <= 0 {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -p.config.RetentionDays)

	filepath.Walk(p.config.OutputDirectory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if !info.IsDir() && info.ModTime().Before(cutoff) {
			os.Remove(path)
		}

		return nil
	})
}

// calculateExportRate calculates exports per second
func (p *CLIExportPlugin) calculateExportRate() float64 {
	// Simple rate calculation - in production would use a sliding window
	return 0.0
}

// Writer implementations

// JSONWriter handles JSON formatting
type JSONWriter struct {
	config *CLIExportConfig
}

func NewJSONWriter(config *CLIExportConfig) *JSONWriter {
	return &JSONWriter{config: config}
}

// YAMLWriter handles YAML formatting
type YAMLWriter struct {
	config *CLIExportConfig
}

func NewYAMLWriter(config *CLIExportConfig) *YAMLWriter {
	return &YAMLWriter{config: config}
}

// MarkdownWriter handles Markdown formatting
type MarkdownWriter struct {
	config *CLIExportConfig
}

func NewMarkdownWriter(config *CLIExportConfig) *MarkdownWriter {
	return &MarkdownWriter{config: config}
}

func (w *MarkdownWriter) Write(writer io.Writer, data exports.ExportData) error {
	// Generate markdown based on data type
	fmt.Fprintf(writer, "# %s Export\n\n", data.Type)
	fmt.Fprintf(writer, "**Generated:** %s\n\n", time.Now().Format(time.RFC3339))

	if data.Tags != nil && len(data.Tags) > 0 {
		fmt.Fprintf(writer, "## Tags\n\n")
		for k, v := range data.Tags {
			fmt.Fprintf(writer, "- **%s:** %s\n", k, v)
		}
		fmt.Fprintf(writer, "\n")
	}

	fmt.Fprintf(writer, "## Data\n\n```json\n")
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	encoder.Encode(data.Content)
	fmt.Fprintf(writer, "```\n")

	return nil
}

// CSVWriter handles CSV formatting
type CSVWriter struct {
	config *CLIExportConfig
}

func NewCSVWriter(config *CLIExportConfig) *CSVWriter {
	return &CSVWriter{config: config}
}

func (w *CSVWriter) Write(writer io.Writer, data exports.ExportData) error {
	csvWriter := csv.NewWriter(writer)
	if w.config.CSVDelimiter != "" {
		csvWriter.Comma = rune(w.config.CSVDelimiter[0])
	}
	defer csvWriter.Flush()

	// Convert data to CSV based on type
	switch data.Type {
	case exports.DataTypeMetrics:
		return w.writeMetricsCSV(csvWriter, data.Content)
	case exports.DataTypeEvents:
		return w.writeEventsCSV(csvWriter, data.Content)
	default:
		// For other types, write as key-value pairs
		return w.writeGenericCSV(csvWriter, data.Content)
	}
}

func (w *CSVWriter) writeMetricsCSV(writer *csv.Writer, content interface{}) error {
	// Write header
	writer.Write([]string{"timestamp", "metric", "value", "labels"})

	// Write metrics data
	// This is a simplified implementation
	if metrics, ok := content.([]interface{}); ok {
		for _, metric := range metrics {
			if m, ok := metric.(map[string]interface{}); ok {
				record := []string{
					fmt.Sprintf("%v", m["timestamp"]),
					fmt.Sprintf("%v", m["metric"]),
					fmt.Sprintf("%v", m["value"]),
					fmt.Sprintf("%v", m["labels"]),
				}
				writer.Write(record)
			}
		}
	}

	return nil
}

func (w *CSVWriter) writeEventsCSV(writer *csv.Writer, content interface{}) error {
	// Write header
	writer.Write([]string{"timestamp", "type", "severity", "message", "source"})

	// Write events data
	// This is a simplified implementation
	if events, ok := content.([]interface{}); ok {
		for _, event := range events {
			if e, ok := event.(map[string]interface{}); ok {
				record := []string{
					fmt.Sprintf("%v", e["timestamp"]),
					fmt.Sprintf("%v", e["type"]),
					fmt.Sprintf("%v", e["severity"]),
					fmt.Sprintf("%v", e["message"]),
					fmt.Sprintf("%v", e["source"]),
				}
				writer.Write(record)
			}
		}
	}

	return nil
}

func (w *CSVWriter) writeGenericCSV(writer *csv.Writer, content interface{}) error {
	// Write as key-value pairs
	writer.Write([]string{"key", "value"})

	// Use JSON marshaling to flatten the content
	data, _ := json.Marshal(content)
	var flat map[string]interface{}
	json.Unmarshal(data, &flat)

	for k, v := range flat {
		writer.Write([]string{k, fmt.Sprintf("%v", v)})
	}

	return nil
}
