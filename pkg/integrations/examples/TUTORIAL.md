# Tutorial: Building Integrations with the Unified Configuration Framework

This tutorial walks through creating a complete integration using the unified configuration framework, from basic setup to advanced features.

## Prerequisites

- Go 1.19 or later
- Basic understanding of YAML/JSON
- Familiarity with Go structs and interfaces

## Tutorial Overview

We'll build a "File Processor" integration that:
1. Monitors a directory for new files
2. Processes files in batches
3. Supports TLS for secure communication
4. Includes circuit breaker for resilience
5. Provides comprehensive monitoring

## Step 1: Basic Integration Setup

### Create the Integration Package

```bash
mkdir -p pkg/integrations/fileprocessor
cd pkg/integrations/fileprocessor
```

### Define Configuration Structure

```go
// config.go
package fileprocessor

import (
    "fmt"
    "time"
    "github.com/yairfalse/tapio/pkg/integrations/config"
)

// Config defines the file processor configuration
type Config struct {
    // Embed base configuration
    config.BaseConfig `yaml:",inline" json:",inline"`
    
    // File processing settings
    WatchDirectory  string        `yaml:"watch_directory" json:"watch_directory"`
    OutputDirectory string        `yaml:"output_directory" json:"output_directory"`
    FilePattern     string        `yaml:"file_pattern" json:"file_pattern"`
    BatchSize       int           `yaml:"batch_size" json:"batch_size"`
    ProcessDelay    time.Duration `yaml:"process_delay" json:"process_delay"`
    
    // Optional framework aspects
    Security   *config.SecurityConfig   `yaml:"security,omitempty" json:"security,omitempty"`
    Resilience *config.ResilienceConfig `yaml:"resilience,omitempty" json:"resilience,omitempty"`
    Monitoring *config.MonitoringConfig `yaml:"monitoring,omitempty" json:"monitoring,omitempty"`
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
    cfg := Config{
        BaseConfig:      config.DefaultBaseConfig(),
        WatchDirectory:  "/var/data/input",
        OutputDirectory: "/var/data/output",
        FilePattern:     "*.txt",
        BatchSize:       10,
        ProcessDelay:    1 * time.Second,
    }
    
    // Set integration-specific defaults
    cfg.Name = "file-processor"
    cfg.Type = "processor"
    cfg.Version = "1.0.0"
    
    return cfg
}

// Validate validates the configuration
func (c Config) Validate() error {
    if c.WatchDirectory == "" {
        return fmt.Errorf("watch_directory is required")
    }
    if c.OutputDirectory == "" {
        return fmt.Errorf("output_directory is required")
    }
    if c.BatchSize <= 0 {
        return fmt.Errorf("batch_size must be positive")
    }
    if c.ProcessDelay < 0 {
        return fmt.Errorf("process_delay must be non-negative")
    }
    return nil
}
```

### Implement the Integration

```go
// integration.go
package fileprocessor

import (
    "context"
    "fmt"
    "log"
    "os"
    "path/filepath"
    "sync"
    "time"
    
    "github.com/yairfalse/tapio/pkg/integrations/config"
)

// FileProcessor implements the config.Integration interface
type FileProcessor struct {
    config Config
    
    // Runtime state
    ctx        context.Context
    cancel     context.CancelFunc
    wg         sync.WaitGroup
    started    bool
    mu         sync.RWMutex
    
    // Statistics
    startTime      time.Time
    filesProcessed uint64
    errors         uint64
    lastActivity   time.Time
}

// NewFileProcessor creates a new file processor
func NewFileProcessor(cfg Config) (*FileProcessor, error) {
    if err := cfg.Validate(); err != nil {
        return nil, fmt.Errorf("invalid configuration: %w", err)
    }
    
    return &FileProcessor{
        config: cfg,
    }, nil
}

// Start implements config.Integration
func (fp *FileProcessor) Start() error {
    fp.mu.Lock()
    defer fp.mu.Unlock()
    
    if fp.started {
        return fmt.Errorf("integration already started")
    }
    
    if !fp.config.Enabled {
        return fmt.Errorf("integration is disabled")
    }
    
    // Initialize context
    fp.ctx, fp.cancel = context.WithCancel(context.Background())
    fp.startTime = time.Now()
    fp.started = true
    
    // Create directories if they don't exist
    if err := os.MkdirAll(fp.config.WatchDirectory, 0755); err != nil {
        return fmt.Errorf("failed to create watch directory: %w", err)
    }
    if err := os.MkdirAll(fp.config.OutputDirectory, 0755); err != nil {
        return fmt.Errorf("failed to create output directory: %w", err)
    }
    
    // Start file watcher
    fp.wg.Add(1)
    go fp.fileWatcher()
    
    log.Printf("File processor started: watching %s, output to %s", 
        fp.config.WatchDirectory, fp.config.OutputDirectory)
    
    return nil
}

// Stop implements config.Integration
func (fp *FileProcessor) Stop() error {
    fp.mu.Lock()
    defer fp.mu.Unlock()
    
    if !fp.started {
        return nil
    }
    
    log.Println("Stopping file processor...")
    
    // Cancel context and wait for goroutines
    fp.cancel()
    fp.wg.Wait()
    
    fp.started = false
    
    log.Printf("File processor stopped. Processed %d files, %d errors", 
        fp.filesProcessed, fp.errors)
    
    return nil
}

// Reload implements config.Integration
func (fp *FileProcessor) Reload(newConfig interface{}) error {
    cfg, ok := newConfig.(Config)
    if !ok {
        return fmt.Errorf("invalid configuration type")
    }
    
    if err := cfg.Validate(); err != nil {
        return fmt.Errorf("invalid configuration: %w", err)
    }
    
    fp.mu.Lock()
    defer fp.mu.Unlock()
    
    // Update runtime-safe configuration
    fp.config.BatchSize = cfg.BatchSize
    fp.config.ProcessDelay = cfg.ProcessDelay
    fp.config.BaseConfig.Observability = cfg.BaseConfig.Observability
    
    log.Printf("Configuration reloaded: batch_size=%d, process_delay=%v", 
        fp.config.BatchSize, fp.config.ProcessDelay)
    
    return nil
}

// Health implements config.Integration
func (fp *FileProcessor) Health() config.HealthStatus {
    fp.mu.RLock()
    defer fp.mu.RUnlock()
    
    healthy := fp.started
    status := "running"
    message := ""
    
    if !fp.started {
        healthy = false
        status = "stopped"
    } else if fp.errors > fp.filesProcessed/2 {
        healthy = false
        status = "unhealthy"
        message = "High error rate detected"
    } else if time.Since(fp.lastActivity) > 10*time.Minute {
        status = "idle"
        message = "No recent file activity"
    }
    
    return config.HealthStatus{
        Healthy:   healthy,
        Status:    status,
        Message:   message,
        LastCheck: time.Now(),
        Details: map[string]interface{}{
            "files_processed": fp.filesProcessed,
            "error_count":     fp.errors,
            "last_activity":   fp.lastActivity,
            "watch_directory": fp.config.WatchDirectory,
            "uptime":          time.Since(fp.startTime),
        },
    }
}

// Statistics implements config.Integration
func (fp *FileProcessor) Statistics() config.Statistics {
    fp.mu.RLock()
    defer fp.mu.RUnlock()
    
    return config.Statistics{
        StartTime:      fp.startTime,
        Uptime:         time.Since(fp.startTime),
        ProcessedCount: fp.filesProcessed,
        ErrorCount:     fp.errors,
        LastActivity:   fp.lastActivity,
        Custom: map[string]interface{}{
            "batch_size":        fp.config.BatchSize,
            "watch_directory":   fp.config.WatchDirectory,
            "output_directory":  fp.config.OutputDirectory,
            "files_per_minute":  fp.calculateFilesPerMinute(),
            "error_rate":        fp.calculateErrorRate(),
        },
    }
}

// GetConfig implements config.Integration
func (fp *FileProcessor) GetConfig() interface{} {
    fp.mu.RLock()
    defer fp.mu.RUnlock()
    return fp.config
}

// ValidateConfig implements config.Integration
func (fp *FileProcessor) ValidateConfig() error {
    return fp.config.Validate()
}

// Internal methods

func (fp *FileProcessor) fileWatcher() {
    defer fp.wg.Done()
    
    ticker := time.NewTicker(fp.config.ProcessDelay)
    defer ticker.Stop()
    
    for {
        select {
        case <-fp.ctx.Done():
            return
        case <-ticker.C:
            if err := fp.processBatch(); err != nil {
                fp.mu.Lock()
                fp.errors++
                fp.mu.Unlock()
                log.Printf("Error processing batch: %v", err)
            }
        }
    }
}

func (fp *FileProcessor) processBatch() error {
    // Find files matching pattern
    pattern := filepath.Join(fp.config.WatchDirectory, fp.config.FilePattern)
    files, err := filepath.Glob(pattern)
    if err != nil {
        return fmt.Errorf("failed to glob files: %w", err)
    }
    
    if len(files) == 0 {
        return nil // No files to process
    }
    
    // Process up to batch_size files
    batchSize := fp.config.BatchSize
    if len(files) < batchSize {
        batchSize = len(files)
    }
    
    for i := 0; i < batchSize; i++ {
        if err := fp.processFile(files[i]); err != nil {
            log.Printf("Error processing file %s: %v", files[i], err)
            fp.mu.Lock()
            fp.errors++
            fp.mu.Unlock()
        } else {
            fp.mu.Lock()
            fp.filesProcessed++
            fp.lastActivity = time.Now()
            fp.mu.Unlock()
        }
    }
    
    return nil
}

func (fp *FileProcessor) processFile(filePath string) error {
    // Simple file processing: copy to output directory
    filename := filepath.Base(filePath)
    outputPath := filepath.Join(fp.config.OutputDirectory, filename)
    
    input, err := os.ReadFile(filePath)
    if err != nil {
        return fmt.Errorf("failed to read file: %w", err)
    }
    
    if err := os.WriteFile(outputPath, input, 0644); err != nil {
        return fmt.Errorf("failed to write output file: %w", err)
    }
    
    // Remove original file
    if err := os.Remove(filePath); err != nil {
        return fmt.Errorf("failed to remove input file: %w", err)
    }
    
    return nil
}

func (fp *FileProcessor) calculateFilesPerMinute() float64 {
    uptime := time.Since(fp.startTime).Minutes()
    if uptime == 0 {
        return 0
    }
    return float64(fp.filesProcessed) / uptime
}

func (fp *FileProcessor) calculateErrorRate() float64 {
    total := fp.filesProcessed + fp.errors
    if total == 0 {
        return 0
    }
    return float64(fp.errors) / float64(total)
}
```

## Step 2: Create Configuration File

```yaml
# config.yaml
name: file-processor
type: processor
version: 1.0.0
environment: production
enabled: true
timeout: 30s

# Base framework configuration
retry:
  enabled: true
  max_attempts: 3
  initial_wait: 100ms
  max_wait: 10s
  multiplier: 2.0

observability:
  tracing_enabled: true
  tracing_sampling: 0.1
  metrics_enabled: true
  metrics_interval: 60s
  log_level: info
  log_format: json

limits:
  max_connections: 50
  max_concurrency: 5
  max_memory_mb: 256
  connection_timeout: 10s
  shutdown_timeout: 30s

labels:
  team: data-processing
  service: file-processor

# Integration-specific configuration
watch_directory: /var/data/input
output_directory: /var/data/output
file_pattern: "*.txt"
batch_size: 10
process_delay: 1s
```

## Step 3: Add Security Configuration

Let's add TLS support for secure communication:

```yaml
# Add to config.yaml
security:
  tls:
    enabled: true
    cert_file: /etc/certs/fileprocessor.crt
    key_file: /etc/certs/fileprocessor.key
    ca_file: /etc/certs/ca.crt
    min_version: TLS1.2
  
  auth:
    enabled: true
    method: jwt
    jwt:
      secret: ${JWT_SECRET}
      issuer: tapio
      expiration: 1h
  
  rate_limit:
    enabled: true
    global:
      rate: 100
      burst: 200
      period: 1s
```

Update the integration to use security configuration:

```go
// Add to integration.go
import (
    "crypto/tls"
    "crypto/x509"
)

func (fp *FileProcessor) setupTLS() (*tls.Config, error) {
    if fp.config.Security == nil || !fp.config.Security.TLS.Enabled {
        return nil, nil
    }
    
    tlsConfig := fp.config.Security.TLS
    
    // Load certificate and key
    cert, err := tls.LoadX509KeyPair(tlsConfig.CertFile, tlsConfig.KeyFile)
    if err != nil {
        return nil, fmt.Errorf("failed to load TLS certificate: %w", err)
    }
    
    // Load CA certificate if provided
    var caCertPool *x509.CertPool
    if tlsConfig.CAFile != "" {
        caCert, err := os.ReadFile(tlsConfig.CAFile)
        if err != nil {
            return nil, fmt.Errorf("failed to load CA certificate: %w", err)
        }
        
        caCertPool = x509.NewCertPool()
        if !caCertPool.AppendCertsFromPEM(caCert) {
            return nil, fmt.Errorf("failed to parse CA certificate")
        }
    }
    
    return &tls.Config{
        Certificates: []tls.Certificate{cert},
        RootCAs:      caCertPool,
        MinVersion:   fp.parseTLSVersion(tlsConfig.MinVersion),
    }, nil
}

func (fp *FileProcessor) parseTLSVersion(version string) uint16 {
    switch version {
    case "TLS1.0":
        return tls.VersionTLS10
    case "TLS1.1":
        return tls.VersionTLS11
    case "TLS1.2":
        return tls.VersionTLS12
    case "TLS1.3":
        return tls.VersionTLS13
    default:
        return tls.VersionTLS12 // Default to TLS 1.2
    }
}
```

## Step 4: Add Resilience Configuration

Add circuit breaker for resilient file processing:

```yaml
# Add to config.yaml
resilience:
  circuit_breaker:
    enabled: true
    failure_threshold: 5
    success_threshold: 2
    timeout: 30s
    half_open_max_requests: 3
  
  timeout:
    default: 30s
    connect: 10s
    read: 30s
    write: 30s
  
  health_check:
    enabled: true
    interval: 30s
    timeout: 5s
    failure_threshold: 3
    endpoints:
      - name: filesystem
        url: file:///var/data/input
        critical: true
```

Implement circuit breaker in the integration:

```go
// Add circuit breaker support
type CircuitBreaker struct {
    failures    int
    lastFailure time.Time
    state       string // "closed", "open", "half-open"
    config      config.CircuitBreakerConfig
}

func (fp *FileProcessor) processFileWithCircuitBreaker(filePath string) error {
    if fp.config.Resilience == nil || !fp.config.Resilience.CircuitBreaker.Enabled {
        return fp.processFile(filePath)
    }
    
    cb := fp.getCircuitBreaker()
    
    // Check circuit breaker state
    if cb.state == "open" {
        if time.Since(cb.lastFailure) < fp.config.Resilience.CircuitBreaker.Timeout {
            return fmt.Errorf("circuit breaker is open")
        }
        cb.state = "half-open"
    }
    
    // Process file
    err := fp.processFile(filePath)
    
    if err != nil {
        cb.failures++
        cb.lastFailure = time.Now()
        
        if cb.failures >= fp.config.Resilience.CircuitBreaker.FailureThreshold {
            cb.state = "open"
            log.Println("Circuit breaker opened due to failures")
        }
        return err
    }
    
    // Success - reset circuit breaker
    if cb.state == "half-open" {
        cb.state = "closed"
        cb.failures = 0
        log.Println("Circuit breaker closed after successful operation")
    }
    
    return nil
}
```

## Step 5: Add Monitoring Configuration

```yaml
# Add to config.yaml
monitoring:
  metrics:
    enabled: true
    provider: prometheus
    endpoint: http://prometheus:9090
    interval: 60s
    labels:
      service: file-processor
      version: 1.0.0
  
  performance:
    enabled: true
    slo:
      enabled: true
      availability_target: 99.9
      latency_targets:
        p95: 5s
        p99: 10s
    
    thresholds:
      error_rate_warning: 0.05
      error_rate_critical: 0.1
  
  alerting:
    enabled: true
    providers:
      - name: slack
        type: webhook
        endpoint: https://hooks.slack.com/services/XXX/YYY/ZZZ
    
    rules:
      - name: high_error_rate
        expression: error_rate > 0.1
        duration: 5m
        severity: critical
```

Implement metrics collection:

```go
// Add metrics support
import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
    filesProcessed prometheus.Counter
    processingTime prometheus.Histogram
    errorRate      prometheus.Gauge
    activeFiles    prometheus.Gauge
}

func (fp *FileProcessor) setupMetrics() *Metrics {
    if fp.config.Monitoring == nil || !fp.config.Monitoring.Metrics.Enabled {
        return nil
    }
    
    metrics := &Metrics{
        filesProcessed: prometheus.NewCounter(prometheus.CounterOpts{
            Name: "fileprocessor_files_processed_total",
            Help: "Total number of files processed",
        }),
        processingTime: prometheus.NewHistogram(prometheus.HistogramOpts{
            Name:    "fileprocessor_processing_duration_seconds",
            Help:    "Time spent processing files",
            Buckets: prometheus.DefBuckets,
        }),
        errorRate: prometheus.NewGauge(prometheus.GaugeOpts{
            Name: "fileprocessor_error_rate",
            Help: "Current error rate",
        }),
        activeFiles: prometheus.NewGauge(prometheus.GaugeOpts{
            Name: "fileprocessor_active_files",
            Help: "Number of files currently being processed",
        }),
    }
    
    // Register metrics
    prometheus.MustRegister(metrics.filesProcessed)
    prometheus.MustRegister(metrics.processingTime)
    prometheus.MustRegister(metrics.errorRate)
    prometheus.MustRegister(metrics.activeFiles)
    
    return metrics
}

func (fp *FileProcessor) processFileWithMetrics(filePath string) error {
    start := time.Now()
    
    if fp.metrics != nil {
        fp.metrics.activeFiles.Inc()
        defer fp.metrics.activeFiles.Dec()
    }
    
    err := fp.processFileWithCircuitBreaker(filePath)
    
    if fp.metrics != nil {
        fp.metrics.processingTime.Observe(time.Since(start).Seconds())
        
        if err != nil {
            fp.metrics.errorRate.Set(fp.calculateErrorRate())
        } else {
            fp.metrics.filesProcessed.Inc()
        }
    }
    
    return err
}
```

## Step 6: Add Comprehensive Tests

```go
// integration_test.go
package fileprocessor

import (
    "os"
    "path/filepath"
    "testing"
    "time"
    
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "github.com/yairfalse/tapio/pkg/integrations/config"
)

func TestFileProcessor_Basic(t *testing.T) {
    // Setup test directories
    tmpDir := t.TempDir()
    inputDir := filepath.Join(tmpDir, "input")
    outputDir := filepath.Join(tmpDir, "output")
    
    cfg := DefaultConfig()
    cfg.WatchDirectory = inputDir
    cfg.OutputDirectory = outputDir
    cfg.ProcessDelay = 100 * time.Millisecond
    
    // Create integration
    processor, err := NewFileProcessor(cfg)
    require.NoError(t, err)
    
    // Start processor
    err = processor.Start()
    require.NoError(t, err)
    defer processor.Stop()
    
    // Create test file
    testFile := filepath.Join(inputDir, "test.txt")
    err = os.WriteFile(testFile, []byte("test content"), 0644)
    require.NoError(t, err)
    
    // Wait for processing
    time.Sleep(200 * time.Millisecond)
    
    // Verify file was processed
    outputFile := filepath.Join(outputDir, "test.txt")
    content, err := os.ReadFile(outputFile)
    require.NoError(t, err)
    assert.Equal(t, "test content", string(content))
    
    // Verify input file was removed
    _, err = os.Stat(testFile)
    assert.True(t, os.IsNotExist(err))
}

func TestFileProcessor_Configuration(t *testing.T) {
    tests := []struct {
        name    string
        config  Config
        wantErr bool
    }{
        {
            name: "valid config",
            config: Config{
                BaseConfig:      config.DefaultBaseConfig(),
                WatchDirectory:  "/tmp/input",
                OutputDirectory: "/tmp/output",
                BatchSize:       10,
            },
            wantErr: false,
        },
        {
            name: "missing watch directory",
            config: Config{
                BaseConfig:      config.DefaultBaseConfig(),
                OutputDirectory: "/tmp/output",
                BatchSize:       10,
            },
            wantErr: true,
        },
        {
            name: "invalid batch size",
            config: Config{
                BaseConfig:      config.DefaultBaseConfig(),
                WatchDirectory:  "/tmp/input",
                OutputDirectory: "/tmp/output",
                BatchSize:       -1,
            },
            wantErr: true,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, err := NewFileProcessor(tt.config)
            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}

func TestFileProcessor_IntegrationInterface(t *testing.T) {
    cfg := DefaultConfig()
    cfg.WatchDirectory = t.TempDir()
    cfg.OutputDirectory = t.TempDir()
    
    processor, err := NewFileProcessor(cfg)
    require.NoError(t, err)
    
    // Test interface compliance
    var _ config.Integration = processor
    
    // Test health when stopped
    health := processor.Health()
    assert.False(t, health.Healthy)
    assert.Equal(t, "stopped", health.Status)
    
    // Test start
    err = processor.Start()
    require.NoError(t, err)
    
    // Test health when running
    health = processor.Health()
    assert.True(t, health.Healthy)
    assert.Equal(t, "running", health.Status)
    
    // Test statistics
    stats := processor.Statistics()
    assert.NotZero(t, stats.StartTime)
    assert.Equal(t, uint64(0), stats.ProcessedCount)
    
    // Test config access
    retrievedConfig := processor.GetConfig().(Config)
    assert.Equal(t, cfg.WatchDirectory, retrievedConfig.WatchDirectory)
    
    // Test validation
    err = processor.ValidateConfig()
    assert.NoError(t, err)
    
    // Test reload
    newCfg := cfg
    newCfg.BatchSize = 20
    err = processor.Reload(newCfg)
    assert.NoError(t, err)
    
    updatedConfig := processor.GetConfig().(Config)
    assert.Equal(t, 20, updatedConfig.BatchSize)
    
    // Test stop
    err = processor.Stop()
    assert.NoError(t, err)
}

func TestFileProcessor_WithSecurityConfig(t *testing.T) {
    cfg := DefaultConfig()
    cfg.WatchDirectory = t.TempDir()
    cfg.OutputDirectory = t.TempDir()
    
    // Add security configuration
    cfg.Security = &config.SecurityConfig{
        BaseConfig: cfg.BaseConfig,
        TLS: config.TLSConfig{
            Enabled:    true,
            MinVersion: "TLS1.2",
        },
    }
    
    processor, err := NewFileProcessor(cfg)
    require.NoError(t, err)
    
    // Verify security config is preserved
    retrievedConfig := processor.GetConfig().(Config)
    require.NotNil(t, retrievedConfig.Security)
    assert.True(t, retrievedConfig.Security.TLS.Enabled)
    assert.Equal(t, "TLS1.2", retrievedConfig.Security.TLS.MinVersion)
}
```

## Step 7: Configuration Loading and Builder Usage

```go
// main.go - Example usage
package main

import (
    "log"
    "os"
    "os/signal"
    "syscall"
    
    "github.com/yairfalse/tapio/pkg/integrations/config"
    "your-project/pkg/integrations/fileprocessor"
)

func main() {
    // Method 1: Load from file
    cfg, err := loadConfigFromFile("config.yaml")
    if err != nil {
        log.Fatal(err)
    }
    
    // Method 2: Use builder (programmatic)
    // cfg := buildConfigProgrammatically()
    
    // Create and start integration
    processor, err := fileprocessor.NewFileProcessor(cfg)
    if err != nil {
        log.Fatalf("Failed to create file processor: %v", err)
    }
    
    if err := processor.Start(); err != nil {
        log.Fatalf("Failed to start file processor: %v", err)
    }
    
    log.Println("File processor started successfully")
    
    // Wait for shutdown signal
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    <-sigChan
    
    log.Println("Shutting down...")
    if err := processor.Stop(); err != nil {
        log.Printf("Error stopping processor: %v", err)
    }
    
    log.Println("Shutdown complete")
}

func loadConfigFromFile(path string) (fileprocessor.Config, error) {
    // Load raw configuration
    rawConfig, err := config.LoadFromFile(path)
    if err != nil {
        return fileprocessor.Config{}, err
    }
    
    // Validate configuration
    if err := config.Validate(rawConfig); err != nil {
        return fileprocessor.Config{}, err
    }
    
    // Convert to typed configuration
    // In real implementation, you'd unmarshal the raw config
    // to your typed config struct
    cfg := fileprocessor.DefaultConfig()
    
    // Example: manual mapping (in practice, use YAML unmarshaling)
    if name, ok := rawConfig["name"].(string); ok {
        cfg.Name = name
    }
    if watchDir, ok := rawConfig["watch_directory"].(string); ok {
        cfg.WatchDirectory = watchDir
    }
    
    return cfg, nil
}

func buildConfigProgrammatically() fileprocessor.Config {
    // Use framework builder for base config
    baseConfigMap, err := config.NewBuilder().
        WithName("file-processor").
        WithType("processor").
        WithEnvironment("production").
        WithRetry(config.RetryConfig{
            Enabled:     true,
            MaxAttempts: 5,
            InitialWait: 100 * time.Millisecond,
        }).
        WithSecurity(config.SecurityConfig{
            TLS: config.TLSConfig{
                Enabled:    true,
                MinVersion: "TLS1.2",
            },
        }).
        Build()
    
    if err != nil {
        log.Fatal(err)
    }
    
    // Create integration config
    cfg := fileprocessor.DefaultConfig()
    cfg.Name = baseConfigMap["name"].(string)
    cfg.Type = baseConfigMap["type"].(string)
    
    // Set custom fields
    cfg.WatchDirectory = "/var/data/input"
    cfg.OutputDirectory = "/var/data/output"
    cfg.BatchSize = 25
    
    return cfg
}
```

## Step 8: Docker and Deployment

```dockerfile
# Dockerfile
FROM golang:1.19-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o fileprocessor ./cmd/fileprocessor

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/fileprocessor .
COPY config.yaml .

# Create directories
RUN mkdir -p /var/data/input /var/data/output

CMD ["./fileprocessor"]
```

```yaml
# docker-compose.yml
version: '3.8'
services:
  fileprocessor:
    build: .
    volumes:
      - ./data/input:/var/data/input
      - ./data/output:/var/data/output
      - ./certs:/etc/certs:ro
    environment:
      - JWT_SECRET=your-secret-here
    ports:
      - "8080:8080"  # Health check endpoint
    restart: unless-stopped
```

## Summary

This tutorial demonstrated:

1. **Basic Integration Setup** - Creating configuration struct and implementing the Integration interface
2. **Framework Integration** - Using BaseConfig and aspect-specific configurations
3. **Security Features** - TLS configuration and JWT authentication
4. **Resilience Patterns** - Circuit breaker implementation
5. **Monitoring Integration** - Metrics collection and health checks
6. **Comprehensive Testing** - Unit tests and integration interface testing
7. **Configuration Loading** - File-based and programmatic configuration
8. **Deployment** - Docker containerization

## Next Steps

1. **Add More Resilience** - Implement retry mechanisms, bulkhead isolation
2. **Enhance Monitoring** - Add custom alerts, SLO tracking
3. **Performance Optimization** - Add profiling, memory management
4. **Advanced Security** - Add OAuth2, API key authentication
5. **Operational Features** - Add graceful shutdown, signal handling
6. **Configuration Validation** - Add custom validation rules
7. **Documentation** - Add API documentation, operational runbooks

The unified configuration framework provides a solid foundation for building robust, observable, and maintainable integrations while following consistent patterns across the Tapio platform.