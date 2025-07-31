package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the main configuration structure
type Config struct {
	// Pipeline configuration
	Pipeline PipelineConfig `yaml:"pipeline" json:"pipeline"`
	
	// Collectors configuration - unified for all collectors
	Collectors CollectorsConfig `yaml:"collectors" json:"collectors"`
}

// PipelineConfig contains pipeline service settings
type PipelineConfig struct {
	Endpoint string `yaml:"endpoint" json:"endpoint"`
	Timeout  int    `yaml:"timeout" json:"timeout"` // seconds
	Retries  int    `yaml:"retries" json:"retries"`
}

// CollectorsConfig contains configuration for all collectors
type CollectorsConfig struct {
	// Which collectors to enable
	Enabled []string `yaml:"enabled" json:"enabled"`
	
	// Common configuration for ALL collectors
	BufferSize     int               `yaml:"buffer_size" json:"buffer_size"`
	MetricsEnabled bool              `yaml:"metrics_enabled" json:"metrics_enabled"`
	Labels         map[string]string `yaml:"labels" json:"labels"`
}

// LoadConfig loads configuration from a file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Determine format by extension
	ext := strings.ToLower(filepath.Ext(path))
	
	config := &Config{}
	switch ext {
	case ".yaml", ".yml":
		err = yaml.Unmarshal(data, config)
	case ".json":
		err = json.Unmarshal(data, config)
	default:
		// Try YAML first, then JSON
		err = yaml.Unmarshal(data, config)
		if err != nil {
			err = json.Unmarshal(data, config)
		}
	}
	
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	
	// Apply defaults
	config.applyDefaults()
	
	return config, nil
}

// applyDefaults sets default values for missing config fields
func (c *Config) applyDefaults() {
	// Pipeline defaults
	if c.Pipeline.Endpoint == "" {
		c.Pipeline.Endpoint = "localhost:50051"
	}
	if c.Pipeline.Timeout == 0 {
		c.Pipeline.Timeout = 30
	}
	if c.Pipeline.Retries == 0 {
		c.Pipeline.Retries = 3
	}
	
	// Collector defaults
	if c.Collectors.BufferSize == 0 {
		c.Collectors.BufferSize = 1000
	}
	if c.Collectors.Labels == nil {
		c.Collectors.Labels = make(map[string]string)
	}
	if len(c.Collectors.Enabled) == 0 {
		c.Collectors.Enabled = []string{"cni", "etcd", "k8s"}
	}
}

// ToCollectorConfig converts to the standard collector config format
func (c *CollectorsConfig) ToCollectorConfig() map[string]interface{} {
	config := make(map[string]interface{})
	config["buffer_size"] = c.BufferSize
	config["metrics_enabled"] = c.MetricsEnabled
	
	// Convert labels to interface map
	labels := make(map[string]interface{})
	for k, v := range c.Labels {
		labels[k] = v
	}
	config["labels"] = labels
	
	return config
}