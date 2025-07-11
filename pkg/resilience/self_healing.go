package resilience

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// SelfHealingManager manages self-healing components
type SelfHealingManager struct {
	components      map[string]*MonitoredComponent
	circuitBreakers *CircuitBreakerGroup
	healthCheckers  map[string]ComponentHealthChecker
	healers         map[string]Healer
	
	// Configuration
	config          *SelfHealingConfig
	
	// State
	isRunning       atomic.Bool
	healingAttempts uint64
	healingSuccess  uint64
	
	// Lifecycle
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	mutex           sync.RWMutex
}

// MonitoredComponent represents a monitored component
type MonitoredComponent struct {
	Name            string
	Type            ComponentType
	Status          ComponentStatus
	LastHealthCheck time.Time
	LastHealAttempt time.Time
	HealthHistory   []HealthRecord
	Dependencies    []string
	Metadata        map[string]interface{}
}

// ComponentType defines the type of component
type ComponentType string

const (
	ComponentTypeService   ComponentType = "service"
	ComponentTypeProcess   ComponentType = "process"
	ComponentTypeContainer ComponentType = "container"
	ComponentTypeNetwork   ComponentType = "network"
	ComponentTypeStorage   ComponentType = "storage"
)

// ComponentStatus represents the health status
type ComponentStatus string

const (
	StatusHealthy   ComponentStatus = "healthy"
	StatusDegraded  ComponentStatus = "degraded"
	StatusUnhealthy ComponentStatus = "unhealthy"
	StatusUnknown   ComponentStatus = "unknown"
	StatusHealing   ComponentStatus = "healing"
)

// HealthRecord represents a health check record
type HealthRecord struct {
	Timestamp time.Time
	Status    ComponentStatus
	Details   map[string]interface{}
	Error     error
}

// ComponentHealthChecker checks component health
type ComponentHealthChecker interface {
	Name() string
	Check(ctx context.Context, component *MonitoredComponent) (ComponentStatus, error)
	GetDetails() map[string]interface{}
}

// Healer performs healing actions
type Healer interface {
	Name() string
	CanHeal(component *MonitoredComponent) bool
	Heal(ctx context.Context, component *MonitoredComponent) error
	GetActions() []HealingAction
}

// HealingAction represents a healing action
type HealingAction struct {
	Name        string
	Description string
	Risk        RiskLevel
	Duration    time.Duration
	Prerequisites []string
}

// RiskLevel represents the risk level of a healing action
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

// SelfHealingConfig configures the self-healing manager
type SelfHealingConfig struct {
	// Health check settings
	HealthCheckInterval    time.Duration
	HealthCheckTimeout     time.Duration
	UnhealthyThreshold     int
	DegradedThreshold      int
	
	// Healing settings
	HealingEnabled         bool
	HealingDelay           time.Duration
	MaxHealingAttempts     int
	HealingCooldown        time.Duration
	
	// Circuit breaker settings
	EnableCircuitBreaker   bool
	MaxFailures            uint32
	ResetTimeout           time.Duration
	
	// History settings
	MaxHealthHistory       int
	HistoryRetention       time.Duration
}

// DefaultSelfHealingConfig returns default configuration
func DefaultSelfHealingConfig() *SelfHealingConfig {
	return &SelfHealingConfig{
		HealthCheckInterval:    30 * time.Second,
		HealthCheckTimeout:     10 * time.Second,
		UnhealthyThreshold:     3,
		DegradedThreshold:      2,
		HealingEnabled:         true,
		HealingDelay:           10 * time.Second,
		MaxHealingAttempts:     3,
		HealingCooldown:        5 * time.Minute,
		EnableCircuitBreaker:   true,
		MaxFailures:            5,
		ResetTimeout:           60 * time.Second,
		MaxHealthHistory:       100,
		HistoryRetention:       24 * time.Hour,
	}
}

// NewSelfHealingManager creates a new self-healing manager
func NewSelfHealingManager(config *SelfHealingConfig) *SelfHealingManager {
	if config == nil {
		config = DefaultSelfHealingConfig()
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	return &SelfHealingManager{
		components:      make(map[string]*MonitoredComponent),
		circuitBreakers: NewCircuitBreakerGroup(),
		healthCheckers:  make(map[string]ComponentHealthChecker),
		healers:         make(map[string]Healer),
		config:          config,
		ctx:             ctx,
		cancel:          cancel,
	}
}

// RegisterComponent registers a component for monitoring
func (m *SelfHealingManager) RegisterComponent(component *MonitoredComponent) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	if _, exists := m.components[component.Name]; exists {
		return fmt.Errorf("component %s already registered", component.Name)
	}
	
	if component.HealthHistory == nil {
		component.HealthHistory = make([]HealthRecord, 0, m.config.MaxHealthHistory)
	}
	
	m.components[component.Name] = component
	
	// Create circuit breaker if enabled
	if m.config.EnableCircuitBreaker {
		componentName := component.Name
		breaker := m.circuitBreakers.GetOrCreate(CircuitBreakerConfig{
			Name:          fmt.Sprintf("healing_%s", componentName),
			MaxFailures:   m.config.MaxFailures,
			ResetTimeout:  m.config.ResetTimeout,
			OnStateChange: func(oldState, newState State) {
				m.onCircuitBreakerStateChange(componentName, oldState, newState)
			},
		})
		m.circuitBreakers.Add(breaker)
	}
	
	return nil
}

// RegisterHealthChecker registers a health checker
func (m *SelfHealingManager) RegisterHealthChecker(checker ComponentHealthChecker) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.healthCheckers[checker.Name()] = checker
}

// RegisterHealer registers a healer
func (m *SelfHealingManager) RegisterHealer(healer Healer) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.healers[healer.Name()] = healer
}

// Start starts the self-healing manager
func (m *SelfHealingManager) Start() error {
	if !m.isRunning.CompareAndSwap(false, true) {
		return fmt.Errorf("self-healing manager already running")
	}
	
	// Start health check goroutine
	m.wg.Add(1)
	go m.runHealthChecks()
	
	// Start healing goroutine if enabled
	if m.config.HealingEnabled {
		m.wg.Add(1)
		go m.runHealing()
	}
	
	// Start cleanup goroutine
	m.wg.Add(1)
	go m.runCleanup()
	
	return nil
}

// Stop stops the self-healing manager
func (m *SelfHealingManager) Stop() error {
	if !m.isRunning.CompareAndSwap(true, false) {
		return nil
	}
	
	m.cancel()
	m.wg.Wait()
	
	return nil
}

// runHealthChecks runs periodic health checks
func (m *SelfHealingManager) runHealthChecks() {
	defer m.wg.Done()
	
	ticker := time.NewTicker(m.config.HealthCheckInterval)
	defer ticker.Stop()
	
	// Initial health check
	m.checkAllComponents()
	
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkAllComponents()
		}
	}
}

// checkAllComponents checks health of all components
func (m *SelfHealingManager) checkAllComponents() {
	m.mutex.RLock()
	components := make([]*MonitoredComponent, 0, len(m.components))
	for _, comp := range m.components {
		components = append(components, comp)
	}
	m.mutex.RUnlock()
	
	var wg sync.WaitGroup
	for _, component := range components {
		wg.Add(1)
		go func(comp *MonitoredComponent) {
			defer wg.Done()
			m.checkComponentHealth(comp)
		}(component)
	}
	wg.Wait()
}

// checkComponentHealth checks health of a single component
func (m *SelfHealingManager) checkComponentHealth(component *MonitoredComponent) {
	ctx, cancel := context.WithTimeout(m.ctx, m.config.HealthCheckTimeout)
	defer cancel()
	
	// Run all applicable health checkers
	var overallStatus ComponentStatus = StatusHealthy
	details := make(map[string]interface{})
	var lastError error
	
	m.mutex.RLock()
	checkers := make([]ComponentHealthChecker, 0, len(m.healthCheckers))
	for _, checker := range m.healthCheckers {
		checkers = append(checkers, checker)
	}
	m.mutex.RUnlock()
	
	for _, checker := range checkers {
		status, err := checker.Check(ctx, component)
		if err != nil {
			lastError = err
			status = StatusUnhealthy
		}
		
		// Update overall status (worst wins)
		if isWorse(status, overallStatus) {
			overallStatus = status
		}
		
		// Collect details
		for k, v := range checker.GetDetails() {
			details[checker.Name()+"_"+k] = v
		}
	}
	
	// Update component status
	m.updateComponentHealth(component, overallStatus, details, lastError)
}

// updateComponentHealth updates component health status
func (m *SelfHealingManager) updateComponentHealth(component *MonitoredComponent, status ComponentStatus, details map[string]interface{}, err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	component.Status = status
	component.LastHealthCheck = time.Now()
	
	// Add to health history
	record := HealthRecord{
		Timestamp: time.Now(),
		Status:    status,
		Details:   details,
		Error:     err,
	}
	
	component.HealthHistory = append(component.HealthHistory, record)
	
	// Trim history if needed
	if len(component.HealthHistory) > m.config.MaxHealthHistory {
		component.HealthHistory = component.HealthHistory[1:]
	}
}

// runHealing runs the healing process
func (m *SelfHealingManager) runHealing() {
	defer m.wg.Done()
	
	ticker := time.NewTicker(m.config.HealingDelay)
	defer ticker.Stop()
	
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.healUnhealthyComponents()
		}
	}
}

// healUnhealthyComponents heals unhealthy components
func (m *SelfHealingManager) healUnhealthyComponents() {
	m.mutex.RLock()
	var unhealthyComponents []*MonitoredComponent
	for _, comp := range m.components {
		if shouldHeal(comp, m.config) {
			unhealthyComponents = append(unhealthyComponents, comp)
		}
	}
	m.mutex.RUnlock()
	
	for _, component := range unhealthyComponents {
		if m.canHealComponent(component) {
			go m.healComponent(component)
		}
	}
}

// canHealComponent checks if a component can be healed
func (m *SelfHealingManager) canHealComponent(component *MonitoredComponent) bool {
	// Check circuit breaker
	if m.config.EnableCircuitBreaker {
		breaker, exists := m.circuitBreakers.Get(fmt.Sprintf("healing_%s", component.Name))
		if exists && breaker.GetState() == StateOpen {
			return false
		}
	}
	
	// Check cooldown
	if !component.LastHealAttempt.IsZero() && 
	   time.Since(component.LastHealAttempt) < m.config.HealingCooldown {
		return false
	}
	
	return true
}

// healComponent attempts to heal a component
func (m *SelfHealingManager) healComponent(component *MonitoredComponent) {
	atomic.AddUint64(&m.healingAttempts, 1)
	
	m.mutex.Lock()
	component.Status = StatusHealing
	component.LastHealAttempt = time.Now()
	m.mutex.Unlock()
	
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Minute)
	defer cancel()
	
	// Execute healing with circuit breaker
	var healErr error
	if m.config.EnableCircuitBreaker {
		breaker, _ := m.circuitBreakers.Get(fmt.Sprintf("healing_%s", component.Name))
		healErr = breaker.Execute(ctx, func() error {
			return m.executeHealing(ctx, component)
		})
	} else {
		healErr = m.executeHealing(ctx, component)
	}
	
	if healErr == nil {
		atomic.AddUint64(&m.healingSuccess, 1)
		m.mutex.Lock()
		component.Status = StatusHealthy
		m.mutex.Unlock()
	} else {
		m.mutex.Lock()
		component.Status = StatusUnhealthy
		m.mutex.Unlock()
	}
}

// executeHealing executes healing actions
func (m *SelfHealingManager) executeHealing(ctx context.Context, component *MonitoredComponent) error {
	m.mutex.RLock()
	healers := make([]Healer, 0)
	for _, healer := range m.healers {
		if healer.CanHeal(component) {
			healers = append(healers, healer)
		}
	}
	m.mutex.RUnlock()
	
	if len(healers) == 0 {
		return fmt.Errorf("no healers available for component %s", component.Name)
	}
	
	// Try each healer
	var lastErr error
	for _, healer := range healers {
		if err := healer.Heal(ctx, component); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	
	return fmt.Errorf("all healers failed: %v", lastErr)
}

// runCleanup runs periodic cleanup
func (m *SelfHealingManager) runCleanup() {
	defer m.wg.Done()
	
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.cleanupHealthHistory()
		}
	}
}

// cleanupHealthHistory cleans up old health history
func (m *SelfHealingManager) cleanupHealthHistory() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	cutoff := time.Now().Add(-m.config.HistoryRetention)
	
	for _, component := range m.components {
		var newHistory []HealthRecord
		for _, record := range component.HealthHistory {
			if record.Timestamp.After(cutoff) {
				newHistory = append(newHistory, record)
			}
		}
		component.HealthHistory = newHistory
	}
}

// GetComponentStatus returns the status of a component
func (m *SelfHealingManager) GetComponentStatus(name string) (*MonitoredComponent, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	component, exists := m.components[name]
	if !exists {
		return nil, fmt.Errorf("component %s not found", name)
	}
	
	// Return a copy
	compCopy := *component
	compCopy.HealthHistory = make([]HealthRecord, len(component.HealthHistory))
	copy(compCopy.HealthHistory, component.HealthHistory)
	
	return &compCopy, nil
}

// GetAllComponentStatus returns status of all components
func (m *SelfHealingManager) GetAllComponentStatus() map[string]*MonitoredComponent {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	result := make(map[string]*MonitoredComponent)
	for name, component := range m.components {
		compCopy := *component
		compCopy.HealthHistory = make([]HealthRecord, len(component.HealthHistory))
		copy(compCopy.HealthHistory, component.HealthHistory)
		result[name] = &compCopy
	}
	
	return result
}

// GetMetrics returns self-healing metrics
func (m *SelfHealingManager) GetMetrics() SelfHealingMetrics {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	var healthy, degraded, unhealthy, healing int
	for _, comp := range m.components {
		switch comp.Status {
		case StatusHealthy:
			healthy++
		case StatusDegraded:
			degraded++
		case StatusUnhealthy:
			unhealthy++
		case StatusHealing:
			healing++
		}
	}
	
	return SelfHealingMetrics{
		TotalComponents:   len(m.components),
		HealthyComponents: healthy,
		DegradedComponents: degraded,
		UnhealthyComponents: unhealthy,
		HealingComponents: healing,
		HealingAttempts:   atomic.LoadUint64(&m.healingAttempts),
		HealingSuccess:    atomic.LoadUint64(&m.healingSuccess),
		CircuitBreakers:   m.circuitBreakers.GetMetrics(),
	}
}

// SelfHealingMetrics contains self-healing metrics
type SelfHealingMetrics struct {
	TotalComponents     int
	HealthyComponents   int
	DegradedComponents  int
	UnhealthyComponents int
	HealingComponents   int
	HealingAttempts     uint64
	HealingSuccess      uint64
	CircuitBreakers     []CircuitBreakerMetrics
}

// onCircuitBreakerStateChange handles circuit breaker state changes
func (m *SelfHealingManager) onCircuitBreakerStateChange(name string, from, to State) {
	// Log or handle state change
}

// Helper functions

func isWorse(status1, status2 ComponentStatus) bool {
	priority := map[ComponentStatus]int{
		StatusHealthy:   0,
		StatusDegraded:  1,
		StatusUnhealthy: 2,
		StatusUnknown:   3,
		StatusHealing:   1,
	}
	
	return priority[status1] > priority[status2]
}

func shouldHeal(component *MonitoredComponent, config *SelfHealingConfig) bool {
	if component.Status != StatusUnhealthy && component.Status != StatusDegraded {
		return false
	}
	
	// Count consecutive unhealthy checks
	unhealthyCount := 0
	for i := len(component.HealthHistory) - 1; i >= 0; i-- {
		if component.HealthHistory[i].Status == StatusUnhealthy {
			unhealthyCount++
		} else {
			break
		}
	}
	
	return unhealthyCount >= config.UnhealthyThreshold
}

// CircuitBreakerGroup manages multiple circuit breakers
type CircuitBreakerGroup struct {
	breakers map[string]*CircuitBreaker
	mutex    sync.RWMutex
}

// NewCircuitBreakerGroup creates a new circuit breaker group
func NewCircuitBreakerGroup() *CircuitBreakerGroup {
	return &CircuitBreakerGroup{
		breakers: make(map[string]*CircuitBreaker),
	}
}

// Add adds a circuit breaker to the group
func (g *CircuitBreakerGroup) Add(breaker *CircuitBreaker) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.breakers[breaker.name] = breaker
}

// Get gets a circuit breaker by name
func (g *CircuitBreakerGroup) Get(name string) (*CircuitBreaker, bool) {
	g.mutex.RLock()
	defer g.mutex.RUnlock()
	breaker, exists := g.breakers[name]
	return breaker, exists
}

// GetOrCreate gets or creates a circuit breaker
func (g *CircuitBreakerGroup) GetOrCreate(config CircuitBreakerConfig) *CircuitBreaker {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	
	if breaker, exists := g.breakers[config.Name]; exists {
		return breaker
	}
	
	breaker := NewCircuitBreaker(config)
	g.breakers[config.Name] = breaker
	return breaker
}

// GetMetrics returns metrics for all circuit breakers
func (g *CircuitBreakerGroup) GetMetrics() []CircuitBreakerMetrics {
	g.mutex.RLock()
	defer g.mutex.RUnlock()
	
	var metrics []CircuitBreakerMetrics
	for _, breaker := range g.breakers {
		metrics = append(metrics, CircuitBreakerMetrics{
			Name:  breaker.name,
			State: breaker.GetState().String(),
		})
	}
	return metrics
}

// CircuitBreakerMetrics for compatibility
type CircuitBreakerMetrics struct {
	Name  string
	State string
}