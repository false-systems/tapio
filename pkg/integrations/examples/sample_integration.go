package examples

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/integrations/config"
	"go.opentelemetry.io/otel/trace"
)

// SampleIntegrationConfig demonstrates how to use the unified config framework
type SampleIntegrationConfig struct {
	config.BaseConfig `yaml:",inline" json:",inline"`

	// Custom configuration fields
	Endpoint   string        `yaml:"endpoint" json:"endpoint"`
	BatchSize  int           `yaml:"batch_size" json:"batch_size"`
	BufferSize int           `yaml:"buffer_size" json:"buffer_size"`
	Workers    int           `yaml:"workers" json:"workers"`
	BatchDelay time.Duration `yaml:"batch_delay" json:"batch_delay"`

	// Optional embedded configs
	Security   *config.SecurityConfig   `yaml:"security,omitempty" json:"security,omitempty"`
	Resilience *config.ResilienceConfig `yaml:"resilience,omitempty" json:"resilience,omitempty"`
	Monitoring *config.MonitoringConfig `yaml:"monitoring,omitempty" json:"monitoring,omitempty"`
}

// SampleIntegration implements the Integration interface
type SampleIntegration struct {
	config  SampleIntegrationConfig
	tracer  trace.Tracer
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	started bool
	mu      sync.RWMutex

	// Runtime state
	processedCount uint64
	errorCount     uint64
	lastActivity   time.Time
	startTime      time.Time
}

// NewSampleIntegration creates a new sample integration
func NewSampleIntegration(cfg SampleIntegrationConfig) (*SampleIntegration, error) {
	// Validate configuration
	if err := validateSampleConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Set defaults if not provided
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 100
	}
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 1000
	}
	if cfg.Workers == 0 {
		cfg.Workers = 5
	}
	if cfg.BatchDelay == 0 {
		cfg.BatchDelay = 5 * time.Second
	}

	return &SampleIntegration{
		config: cfg,
	}, nil
}

// Start implements the Integration interface
func (s *SampleIntegration) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("integration already started")
	}

	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.startTime = time.Now()

	// Start workers
	for i := 0; i < s.config.Workers; i++ {
		s.wg.Add(1)
		go s.worker(i)
	}

	s.started = true
	log.Printf("Integration started: workers=%d, endpoint=%s, batch_size=%d",
		s.config.Workers, s.config.Endpoint, s.config.BatchSize)

	return nil
}

// Stop implements the Integration interface
func (s *SampleIntegration) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return fmt.Errorf("integration not started")
	}

	log.Println("Stopping integration")

	// Cancel context to signal workers to stop
	s.cancel()

	// Wait for all workers to finish
	s.wg.Wait()

	s.started = false
	log.Printf("Integration stopped: processed=%d, errors=%d",
		s.processedCount, s.errorCount)

	return nil
}

// Reload implements the Integration interface
func (s *SampleIntegration) Reload(newConfig interface{}) error {
	cfg, ok := newConfig.(SampleIntegrationConfig)
	if !ok {
		return fmt.Errorf("invalid configuration type")
	}

	if err := validateSampleConfig(cfg); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Update configuration that can be changed at runtime
	s.config.BatchSize = cfg.BatchSize
	s.config.BatchDelay = cfg.BatchDelay
	s.config.Observability = cfg.Observability
	s.config.Limits = cfg.Limits

	log.Printf("Configuration reloaded: batch_size=%d, batch_delay=%v",
		s.config.BatchSize, s.config.BatchDelay)

	return nil
}

// Health implements the Integration interface
func (s *SampleIntegration) Health() config.HealthStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	healthy := s.started && s.errorCount < 100
	status := "running"
	message := ""

	if !s.started {
		healthy = false
		status = "stopped"
	} else if s.errorCount > 50 {
		status = "degraded"
		message = "High error rate detected"
	} else if time.Since(s.lastActivity) > 5*time.Minute {
		status = "idle"
		message = "No recent activity"
	}

	return config.HealthStatus{
		Healthy:   healthy,
		Status:    status,
		Message:   message,
		LastCheck: time.Now(),
		Details: map[string]interface{}{
			"processed_count": s.processedCount,
			"error_count":     s.errorCount,
			"last_activity":   s.lastActivity,
			"workers":         s.config.Workers,
			"uptime":          time.Since(s.startTime),
		},
	}
}

// Statistics implements the Integration interface
func (s *SampleIntegration) Statistics() config.Statistics {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return config.Statistics{
		StartTime:      s.startTime,
		Uptime:         time.Since(s.startTime),
		ProcessedCount: s.processedCount,
		ErrorCount:     s.errorCount,
		LastActivity:   s.lastActivity,
		Custom: map[string]interface{}{
			"endpoint":        s.config.Endpoint,
			"batch_size":      s.config.BatchSize,
			"buffer_size":     s.config.BufferSize,
			"workers":         s.config.Workers,
			"events_per_sec":  s.calculateEventsPerSecond(),
			"error_rate":      s.calculateErrorRate(),
		},
	}
}

// GetConfig implements the Integration interface
func (s *SampleIntegration) GetConfig() interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// ValidateConfig implements the Integration interface
func (s *SampleIntegration) ValidateConfig() error {
	return validateSampleConfig(s.config)
}

// worker simulates processing work
func (s *SampleIntegration) worker(id int) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.BatchDelay)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			// Simulate processing a batch
			if err := s.processBatch(); err != nil {
				s.mu.Lock()
				s.errorCount++
				s.mu.Unlock()
				log.Printf("Failed to process batch: worker=%d, error=%v", id, err)
			} else {
				s.mu.Lock()
				s.processedCount += uint64(s.config.BatchSize)
				s.lastActivity = time.Now()
				s.mu.Unlock()
			}
		}
	}
}

// processBatch simulates processing a batch of items
func (s *SampleIntegration) processBatch() error {
	// Simulate work
	time.Sleep(100 * time.Millisecond)

	// Simulate occasional errors
	if time.Now().Unix()%10 == 0 {
		return fmt.Errorf("simulated processing error")
	}

	return nil
}

// calculateEventsPerSecond calculates the current event processing rate
func (s *SampleIntegration) calculateEventsPerSecond() float64 {
	uptime := time.Since(s.startTime).Seconds()
	if uptime == 0 {
		return 0
	}
	return float64(s.processedCount) / uptime
}

// calculateErrorRate calculates the current error rate
func (s *SampleIntegration) calculateErrorRate() float64 {
	total := s.processedCount + s.errorCount
	if total == 0 {
		return 0
	}
	return float64(s.errorCount) / float64(total)
}

// validateSampleConfig validates the sample configuration
func validateSampleConfig(cfg SampleIntegrationConfig) error {
	if cfg.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}

	if cfg.Workers < 0 || cfg.Workers > 100 {
		return fmt.Errorf("workers must be between 0 and 100")
	}

	if cfg.BatchSize < 0 || cfg.BatchSize > 10000 {
		return fmt.Errorf("batch_size must be between 0 and 10000")
	}

	return nil
}

// DefaultSampleConfig returns a default configuration
func DefaultSampleConfig() SampleIntegrationConfig {
	return SampleIntegrationConfig{
		BaseConfig:  config.DefaultBaseConfig(),
		Endpoint:    "http://localhost:8080",
		BatchSize:   100,
		BufferSize:  1000,
		Workers:     5,
		BatchDelay:  5 * time.Second,
		Security:    nil, // Optional
		Resilience:  nil, // Optional
		Monitoring:  nil, // Optional
	}
}