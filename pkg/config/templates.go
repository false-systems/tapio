package config

import (
	"fmt"
	"time"
)

// ConfigTemplate represents a configuration template
type ConfigTemplate struct {
	Name        string
	Description string
	Config      *Config
	Examples    []string
	UseCases    []string
}

// GetAllTemplates returns all available configuration templates
func GetAllTemplates() map[string]*ConfigTemplate {
	return map[string]*ConfigTemplate{
		"minimal":     GetMinimalTemplate(),
		"default":     GetDefaultTemplate(),
		"production":  GetProductionTemplate(),
		"development": GetDevelopmentTemplate(),
		"ci":          GetCITemplate(),
		"monitoring":  GetMonitoringTemplate(),
		"security":    GetSecurityTemplate(),
		"performance": GetPerformanceTemplate(),
	}
}

// GetMinimalTemplate returns a minimal configuration template
func GetMinimalTemplate() *ConfigTemplate {
	config := &Config{
		Version:        "1.0",
		LogLevel:       "info",
		LogFormat:      "text",
		UpdateInterval: 30 * time.Second,

		Features: FeaturesConfig{
			EnableEBPF:        false,
			EnablePrediction:  false,
			EnableMetrics:     false,
			EnableCorrelation: false,
		},

		Output: OutputConfig{
			Format: "human",
			Color:  true,
		},

		Kubernetes: KubernetesConfig{
			InCluster: false,
		},

		Resources: ResourcesConfig{
			MaxMemoryUsage: 256,
			MaxCPUPercent:  25,
		},
	}

	return &ConfigTemplate{
		Name:        "minimal",
		Description: "Minimal configuration for basic usage with zero dependencies",
		Config:      config,
		Examples: []string{
			"tapio check",
			"tapio why my-pod",
		},
		UseCases: []string{
			"Quick cluster health checks",
			"Learning and experimentation",
			"CI/CD pipeline basic checks",
			"Resource-constrained environments",
		},
	}
}

// GetDefaultTemplate returns the default configuration template
func GetDefaultTemplate() *ConfigTemplate {
	config := DefaultConfig()

	return &ConfigTemplate{
		Name:        "default",
		Description: "Balanced configuration suitable for most use cases",
		Config:      config,
		Examples: []string{
			"tapio check --all",
			"tapio prometheus",
			"tapio doctor",
		},
		UseCases: []string{
			"General cluster monitoring",
			"Development environments",
			"Small to medium production clusters",
			"Learning advanced features",
		},
	}
}

// GetProductionTemplate returns a production-ready configuration template
func GetProductionTemplate() *ConfigTemplate {
	config := DefaultConfig()

	// Production overrides
	config.LogLevel = "warn"
	config.LogFormat = "json"
	config.Features.EnableEBPF = true
	config.Features.EnableMetrics = true
	config.Features.EnableCorrelation = true
	config.Features.EnablePrediction = true

	config.Metrics.Enabled = true
	config.Metrics.Port = 9090

	config.Resources.MaxMemoryUsage = 2048
	config.Resources.MaxCPUPercent = 75
	config.Resources.DataRetentionPeriod = 7 * 24 * time.Hour

	config.Output.Format = "json"
	config.Output.Color = false

	config.EBPF.Enabled = true
	config.EBPF.SamplingRate = 0.1
	config.EBPF.RetentionPeriod = "1h"

	config.Advanced.ProfilerEnabled = true
	config.Advanced.ProfilerPort = 6060

	return &ConfigTemplate{
		Name:        "production",
		Description: "Production-ready configuration with all features enabled",
		Config:      config,
		Examples: []string{
			"tapio check --format json",
			"tapio prometheus --metrics-port 9090",
			"tapio sniff --sampling-rate 0.1",
		},
		UseCases: []string{
			"Large production clusters",
			"24/7 monitoring and alerting",
			"Advanced troubleshooting",
			"Performance optimization",
			"Compliance and auditing",
		},
	}
}

// GetDevelopmentTemplate returns a development-friendly configuration template
func GetDevelopmentTemplate() *ConfigTemplate {
	config := DefaultConfig()

	// Development overrides
	config.LogLevel = "debug"
	config.Features.EnablePrediction = true
	config.Output.Verbose = true
	config.Output.Color = true
	config.Advanced.DebugMode = true
	config.Advanced.ProfilerEnabled = true

	// More generous resource limits for development
	config.Resources.MaxMemoryUsage = 1024
	config.Resources.MaxCPUPercent = 50

	return &ConfigTemplate{
		Name:        "development",
		Description: "Development-friendly configuration with verbose output and debugging",
		Config:      config,
		Examples: []string{
			"tapio check --verbose",
			"tapio why my-pod --debug",
			"tapio doctor --verbose",
		},
		UseCases: []string{
			"Local development clusters",
			"Feature development and testing",
			"Debugging Tapio itself",
			"Learning cluster internals",
		},
	}
}

// GetCITemplate returns a CI/CD-friendly configuration template
func GetCITemplate() *ConfigTemplate {
	config := DefaultConfig()

	// CI overrides
	config.LogLevel = "info"
	config.LogFormat = "json"
	config.Output.Color = false
	config.Output.Verbose = false
	config.Features.EnableEBPF = false // Usually not available in CI
	config.Features.EnableMetrics = false

	// Conservative resource limits for CI
	config.Resources.MaxMemoryUsage = 256
	config.Resources.MaxCPUPercent = 25
	config.UpdateInterval = 30 * time.Second

	return &ConfigTemplate{
		Name:        "ci",
		Description: "CI/CD pipeline configuration with conservative resource usage",
		Config:      config,
		Examples: []string{
			"tapio check --format json --no-color",
			"tapio doctor --report",
		},
		UseCases: []string{
			"GitHub Actions workflows",
			"GitLab CI pipelines",
			"Jenkins jobs",
			"Automated testing",
			"Health checks in pipelines",
		},
	}
}

// GetMonitoringTemplate returns a monitoring-focused configuration template
func GetMonitoringTemplate() *ConfigTemplate {
	config := DefaultConfig()

	// Monitoring overrides
	config.Features.EnableMetrics = true
	config.Features.EnableCorrelation = true
	config.Features.EnablePrediction = true

	config.Metrics.Enabled = true
	config.Metrics.Port = 9090
	config.Metrics.Interval = 15 * time.Second

	config.UpdateInterval = 10 * time.Second

	// Monitor all namespaces by default
	config.Kubernetes.Namespaces.AllowAll = true

	return &ConfigTemplate{
		Name:        "monitoring",
		Description: "Comprehensive monitoring configuration with metrics and correlation",
		Config:      config,
		Examples: []string{
			"tapio prometheus --interval 15s",
			"tapio check --all --format json",
		},
		UseCases: []string{
			"Prometheus integration",
			"Grafana dashboards",
			"Alert manager rules",
			"SRE monitoring stacks",
			"Observability platforms",
		},
	}
}

// GetSecurityTemplate returns a security-focused configuration template
func GetSecurityTemplate() *ConfigTemplate {
	config := DefaultConfig()

	// Security overrides
	config.LogLevel = "info"
	config.LogFormat = "json"
	config.Features.EnableCorrelation = true

	// Focus on security-relevant namespaces
	config.Kubernetes.Namespaces.Exclude = []string{
		"kube-system", "kube-public", "kube-node-lease",
	}

	// Security-focused output
	config.Output.Fields = []string{
		"name", "namespace", "status", "security_context", "capabilities",
	}

	return &ConfigTemplate{
		Name:        "security",
		Description: "Security-focused configuration for compliance and audit scenarios",
		Config:      config,
		Examples: []string{
			"tapio check --security-focus",
			"tapio why suspicious-pod",
		},
		UseCases: []string{
			"Security audits",
			"Compliance checking",
			"Vulnerability assessment",
			"Security policy validation",
			"Incident response",
		},
	}
}

// GetPerformanceTemplate returns a performance-optimized configuration template
func GetPerformanceTemplate() *ConfigTemplate {
	config := DefaultConfig()

	// Performance overrides
	config.Features.EnableEBPF = true
	config.Features.EnablePrediction = true

	config.EBPF.Enabled = true
	config.EBPF.SamplingRate = 0.05 // Lower sampling for performance
	config.EBPF.EventBufferSize = 2048

	config.Resources.MaxMemoryUsage = 4096
	config.Resources.MaxCPUPercent = 80
	config.Resources.ParallelWorkers = 8

	config.UpdateInterval = 5 * time.Second

	return &ConfigTemplate{
		Name:        "performance",
		Description: "High-performance configuration for large clusters and intensive monitoring",
		Config:      config,
		Examples: []string{
			"tapio sniff --high-performance",
			"tapio check --parallel",
		},
		UseCases: []string{
			"Large enterprise clusters",
			"High-frequency monitoring",
			"Performance troubleshooting",
			"Capacity planning",
			"Resource optimization",
		},
	}
}

// GetTemplateByName returns a specific template by name
func GetTemplateByName(name string) (*ConfigTemplate, error) {
	templates := GetAllTemplates()
	template, exists := templates[name]
	if !exists {
		return nil, fmt.Errorf("template '%s' not found", name)
	}
	return template, nil
}

// ListTemplateNames returns all available template names
func ListTemplateNames() []string {
	templates := GetAllTemplates()
	names := make([]string, 0, len(templates))
	for name := range templates {
		names = append(names, name)
	}
	return names
}

// GetTemplateDescription returns a description of all templates
func GetTemplateDescription() string {
	templates := GetAllTemplates()
	description := "Available configuration templates:\n\n"

	for name, template := range templates {
		description += fmt.Sprintf("• %s: %s\n", name, template.Description)
		description += fmt.Sprintf("  Use cases: %v\n", template.UseCases)
		description += "\n"
	}

	return description
}
