# Tapio Installation System

A sophisticated, plugin-based installation system for Tapio demonstrating advanced Go patterns and best practices.

## Architecture

The installation system is built with the following design patterns:

- **Strategy Pattern**: Different installation methods (binary, Docker, Kubernetes)
- **Command Pattern**: Reversible installation steps with rollback support
- **Factory Pattern**: Platform-specific installer creation
- **Pipeline Pattern**: Orchestrated installation flow with generics
- **Circuit Breaker Pattern**: Resilient network operations
- **Observer Pattern**: Progress reporting and metrics collection

## Key Features

1. **Multiple Installation Strategies**
   - Binary installation with platform-specific handling
   - Docker container deployment
   - Kubernetes manifest application

2. **Advanced Error Handling**
   - Circuit breaker for network operations
   - Automatic retry with exponential backoff
   - Comprehensive rollback on failure

3. **Progress Tracking**
   - Real-time terminal UI with progress bars
   - Concurrent operation tracking
   - Detailed metrics collection

4. **Platform Detection**
   - OS and architecture detection
   - Container environment awareness
   - Distribution-specific handling

5. **Post-Installation Validation**
   - Binary integrity checks
   - Service health verification
   - Network connectivity testing

## Usage

### CLI Installation

```bash
# Install with default settings
tapio-install install

# Install specific version
tapio-install install --version v1.2.3

# Docker installation
tapio-install install --strategy docker

# Dry run
tapio-install install --dry-run

# Custom paths
tapio-install install \
  --install-path /opt/custom/tapio \
  --config-path /etc/custom/tapio \
  --data-path /var/lib/custom/tapio
```

### Programmatic Usage

```go
// Detect platform
detector := platform.NewDetector()
platformInfo := detector.Detect()

// Create installer
factory := platform.NewFactory(platformInfo)
installer, err := factory.Create(installer.StrategyBinary)

// Configure installation
opts := installer.InstallOptions{
    Version:     "latest",
    InstallPath: "/opt/tapio",
    Progress:    progress.NewTerminalReporter(),
}

// Install
if err := installer.Install(ctx, opts); err != nil {
    log.Fatal(err)
}
```

## Components

### Core Interfaces

- `Installer`: Main installation interface
- `Step`: Individual installation step
- `Pipeline`: Step orchestration
- `ProgressReporter`: Progress reporting
- `CircuitBreaker`: Network resilience
- `Validator`: Post-install validation

### Installation Pipeline

1. **Download**: Fetch binary with resume support
2. **Verify**: Checksum validation
3. **Extract**: Archive extraction
4. **Install**: Binary placement and permissions
5. **Configure**: Default configuration creation
6. **Service**: System service setup
7. **Validate**: Installation verification

### Platform Support

- **Linux**: systemd/sysvinit service management
- **macOS**: launchd service management
- **Windows**: Windows service management
- **Container**: Docker and Kubernetes aware

## Advanced Features

### Concurrent Downloads

Large files are downloaded in chunks concurrently:

```go
downloader := installer.NewDownloader(httpClient)
err := downloader.DownloadWithProgress(ctx, opts, dst, func(current, total int64) {
    fmt.Printf("Progress: %d/%d bytes\n", current, total)
})
```

### Pipeline with Rollback

```go
pipeline := installer.NewPipeline[*InstallData]().
    AddStep(&DownloadStep{}).
    AddStep(&ExtractStep{}).
    AddStep(&InstallStep{}).
    WithRollback(true).
    WithMetrics(metricsCollector)

result, err := pipeline.Execute(ctx, initialData)
```

### Circuit Breaker

```go
cb := installer.NewAdaptiveCircuitBreaker(5, 1*time.Minute)
err := cb.Execute(func() error {
    return performNetworkOperation()
})
```

### Custom Steps

```go
type CustomStep struct{}

func (s *CustomStep) Name() string { return "custom" }

func (s *CustomStep) Execute(ctx context.Context, data *Data) (*Data, error) {
    // Perform step
    return data, nil
}

func (s *CustomStep) Rollback(ctx context.Context, data *Data) error {
    // Undo step
    return nil
}

func (s *CustomStep) Validate(ctx context.Context, data *Data) error {
    // Validate step
    return nil
}
```

## Error Handling

The system provides comprehensive error handling:

- Custom error types for different failure scenarios
- Detailed error context with wrapped errors
- Automatic rollback on failure
- Retry logic for transient failures

## Metrics and Telemetry

Installation metrics are collected and can be exported:

```go
collector := progress.NewMetricsCollector()
// ... perform installation ...
report := collector.GetReport()
jsonData, _ := collector.ExportJSON()
```

## Testing

Run the example to see the installation system in action:

```bash
cd cmd/install/example
go run main.go
```

## Future Enhancements

- Helm chart generation for Kubernetes
- Automatic update checking
- Plugin system for custom installers
- Web-based installation UI
- Installation profiles
- Multi-version management