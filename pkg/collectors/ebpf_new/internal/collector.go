package internal

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf_new/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

// collector implements the core.Collector interface
type collector struct {
	// Configuration
	config core.Config

	// State management
	mu       sync.RWMutex
	started  atomic.Bool
	closed   atomic.Bool
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	// Event channels
	eventChan    chan domain.Event
	subscriptions map[string]*subscription

	// Components
	loader    core.ProgramLoader
	parser    core.EventParser
	manager   core.MapManager
	readers   map[string]core.RingBufferReader

	// Loaded programs
	programs map[string]core.Program

	// Statistics
	stats struct {
		eventsCollected atomic.Uint64
		eventsDropped   atomic.Uint64
		eventsFiltered  atomic.Uint64
		bytesProcessed  atomic.Uint64
		errors          atomic.Uint64
		lastCollection  atomic.Value // time.Time
	}

	// Rate limiting
	rateLimiter *rateLimiter

	// Health tracking
	health struct {
		mu        sync.RWMutex
		status    core.HealthStatus
		message   string
		issues    []core.HealthIssue
		lastCheck time.Time
	}

	// Start time
	startTime time.Time
}

// NewCollector creates a new eBPF collector
func NewCollector(config core.Config, loader core.ProgramLoader, parser core.EventParser, manager core.MapManager) (core.Collector, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := &collector{
		config:        config,
		ctx:           ctx,
		cancel:        cancel,
		eventChan:     make(chan domain.Event, config.EventBufferSize),
		subscriptions: make(map[string]*subscription),
		loader:        loader,
		parser:        parser,
		manager:       manager,
		readers:       make(map[string]core.RingBufferReader),
		programs:      make(map[string]core.Program),
		startTime:     time.Now(),
	}

	// Initialize rate limiter if configured
	if config.MaxEventsPerSecond > 0 {
		c.rateLimiter = newRateLimiter(config.MaxEventsPerSecond)
	}

	// Initialize health status
	c.health.status = core.HealthStatusHealthy
	c.health.message = "Collector initialized"
	c.health.lastCheck = time.Now()

	// Store initial stats
	c.stats.lastCollection.Store(time.Time{})

	return c, nil
}

// GetSourceType implements domain.EventSource
func (c *collector) GetSourceType() domain.SourceType {
	return domain.SourceType("ebpf")
}

// Subscribe implements domain.EventSource
func (c *collector) Subscribe(ctx context.Context, opts domain.SubscriptionOptions) (domain.EventStream, error) {
	if c.closed.Load() {
		return nil, core.CollectorClosedError{Operation: "subscribe"}
	}

	// Create stream
	bufferSize := opts.BufferSize
	if bufferSize == 0 {
		bufferSize = 1000
	}
	stream := newEventStream(bufferSize)

	// Create subscription
	sub := &subscription{
		id:       generateSubscriptionID(),
		options:  opts,
		stream:   stream,
		ctx:      ctx,
		created:  time.Now(),
	}

	// Register subscription
	c.mu.Lock()
	c.subscriptions[sub.id] = sub
	c.mu.Unlock()

	// Start subscription handler
	c.wg.Add(1)
	go c.handleSubscription(sub)

	return stream, nil
}

// Health implements domain.EventSource
func (c *collector) Health(ctx context.Context) domain.SourceHealth {
	health := c.GetHealth()
	
	// Convert to domain.SourceHealth
	status := domain.HealthHealthy
	switch health.Status {
	case core.HealthStatusDegraded:
		status = domain.HealthDegraded
	case core.HealthStatusUnhealthy:
		status = domain.HealthUnhealthy
	}
	
	// Calculate event rate
	eventRate := float64(0)
	if elapsed := time.Since(c.startTime).Seconds(); elapsed > 0 {
		eventRate = float64(c.stats.eventsCollected.Load()) / elapsed
	}
	
	return domain.SourceHealth{
		Status:    status,
		Message:   health.Message,
		LastSeen:  health.LastCheck,
		EventRate: eventRate,
		Errors:    int(c.stats.errors.Load()),
		Warnings:  len(health.Issues),
	}
}

// Query implements domain.EventSource
func (c *collector) Query(ctx context.Context, criteria domain.QueryCriteria) ([]domain.Event, error) {
	if c.closed.Load() {
		return nil, core.CollectorClosedError{Operation: "query"}
	}

	// Validate criteria
	if err := c.validateCriteria(criteria); err != nil {
		return nil, err
	}

	// For eBPF, we don't have historical data, so we collect for a short duration
	// This is a simplified implementation - a real one might maintain a circular buffer
	collectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var events []domain.Event
	opts := domain.SubscriptionOptions{
		BufferSize: 1000,
		EventTypes: criteria.EventTypes,
		Severities: criteria.Severities,
	}
	
	stream, err := c.Subscribe(collectCtx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary subscription: %w", err)
	}
	defer stream.Close()

	// Collect events until context expires or we have enough
	for {
		select {
		case <-collectCtx.Done():
			return events, nil
		case event, ok := <-stream.Events():
			if !ok {
				return events, nil
			}
			// Apply query criteria filters
			if c.matchesCriteria(event, criteria) {
				events = append(events, event)
				if len(events) >= 1000 {
					return events, nil
				}
			}
		case err := <-stream.Errors():
			// Log error but continue
			_ = err
		}
	}
}

// LoadPrograms implements core.Collector
func (c *collector) LoadPrograms(ctx context.Context) error {
	if c.closed.Load() {
		return core.CollectorClosedError{Operation: "load programs"}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Load each configured program
	for _, spec := range c.config.Programs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		program, err := c.loader.Load(ctx, spec)
		if err != nil {
			c.recordHealthIssue(spec.Name, fmt.Sprintf("Failed to load program: %v", err), domain.SeverityError)
			return core.ProgramLoadError{
				ProgramName: spec.Name,
				ProgramType: spec.Type,
				Cause:       err,
			}
		}

		c.programs[spec.Name] = program

		// Create ring buffer reader if this program uses ring buffer
		for _, mapSpec := range spec.Maps {
			if mapSpec.Type == core.MapTypeRingBuf {
				reader, err := c.createRingBufferReader(mapSpec.Name)
				if err != nil {
					return fmt.Errorf("failed to create ring buffer reader for %s: %w", mapSpec.Name, err)
				}
				c.readers[mapSpec.Name] = reader
			}
		}
	}

	c.updateHealthStatus(core.HealthStatusHealthy, "All programs loaded successfully")
	return nil
}

// UnloadPrograms implements core.Collector
func (c *collector) UnloadPrograms() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var errs []error

	// Close all ring buffer readers first
	for name, reader := range c.readers {
		if err := reader.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close reader %s: %w", name, err))
		}
		delete(c.readers, name)
	}

	// Unload all programs
	for name, program := range c.programs {
		if err := c.loader.Unload(program); err != nil {
			errs = append(errs, fmt.Errorf("failed to unload program %s: %w", name, err))
		}
		delete(c.programs, name)
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during unload: %v", errs)
	}

	return nil
}

// GetLoadedPrograms implements core.Collector
func (c *collector) GetLoadedPrograms() ([]core.ProgramInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var infos []core.ProgramInfo
	for _, program := range c.programs {
		info := core.ProgramInfo{
			Program: program,
			Maps:    []core.MapInfo{},
		}

		// Get associated maps
		for _, spec := range c.config.Programs {
			if spec.Name == program.Name {
				for _, mapSpec := range spec.Maps {
					mapInfo, err := c.getMapInfo(mapSpec.Name)
					if err == nil {
						info.Maps = append(info.Maps, mapInfo)
					}
				}
				break
			}
		}

		infos = append(infos, info)
	}

	return infos, nil
}

// SetFilter implements core.Collector
func (c *collector) SetFilter(filter core.Filter) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.config.Filter = filter
	return nil
}

// GetStats implements core.Collector
func (c *collector) GetStats() (core.Stats, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := core.Stats{
		Programs:           make(map[string]core.ProgramStats),
		EventsCollected:    c.stats.eventsCollected.Load(),
		EventsDropped:      c.stats.eventsDropped.Load(),
		EventsFiltered:     c.stats.eventsFiltered.Load(),
		BytesProcessed:     c.stats.bytesProcessed.Load(),
		CollectionErrors:   c.stats.errors.Load(),
		StartTime:          c.startTime,
	}

	// Get last collection time
	if lastTime, ok := c.stats.lastCollection.Load().(time.Time); ok {
		stats.LastCollectionTime = lastTime
	}

	// Get program stats
	for name, program := range c.programs {
		stats.Programs[name] = program.Stats
	}

	// Get ring buffer stats
	if len(c.readers) > 0 {
		stats.RingBufferStats = core.RingBufferStats{
			Size: c.config.RingBufferSize,
			// Real implementation would query actual buffer stats
		}
	}

	return stats, nil
}

// GetHealth implements core.Collector
func (c *collector) GetHealth() core.Health {
	c.health.mu.RLock()
	defer c.health.mu.RUnlock()

	return core.Health{
		Status:          c.health.status,
		Message:         c.health.message,
		LastCheck:       c.health.lastCheck,
		ProgramsLoaded:  len(c.programs),
		ProgramsHealthy: c.countHealthyPrograms(),
		Issues:          append([]core.HealthIssue{}, c.health.issues...),
	}
}

// Start starts the event collection
func (c *collector) Start(ctx context.Context) error {
	if !c.started.CompareAndSwap(false, true) {
		return fmt.Errorf("collector already started")
	}

	// Load programs if not already loaded
	c.mu.RLock()
	programsLoaded := len(c.programs) > 0
	c.mu.RUnlock()

	if !programsLoaded {
		if err := c.LoadPrograms(ctx); err != nil {
			c.started.Store(false)
			return fmt.Errorf("failed to load programs: %w", err)
		}
	}

	// Start collection workers
	c.wg.Add(1)
	go c.collectionWorker()

	// Start health checker
	c.wg.Add(1)
	go c.healthChecker()

	c.updateHealthStatus(core.HealthStatusHealthy, "Collector started successfully")
	return nil
}

// Stop stops the event collection
func (c *collector) Stop() error {
	if !c.started.CompareAndSwap(true, false) {
		return fmt.Errorf("collector not started")
	}

	// Cancel context to stop workers
	c.cancel()

	// Wait for workers to finish
	c.wg.Wait()

	// Close all subscriptions
	c.mu.Lock()
	for _, sub := range c.subscriptions {
		sub.stream.Close()
	}
	c.subscriptions = make(map[string]*subscription)
	c.mu.Unlock()

	// Unload programs
	if err := c.UnloadPrograms(); err != nil {
		return fmt.Errorf("error during stop: %w", err)
	}

	c.updateHealthStatus(core.HealthStatusHealthy, "Collector stopped")
	return nil
}

// Close closes the collector and releases all resources
func (c *collector) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}

	// Stop if running
	if c.started.Load() {
		if err := c.Stop(); err != nil {
			return err
		}
	}

	// Close event channel
	close(c.eventChan)

	return nil
}

// Private methods

func (c *collector) validateCriteria(criteria domain.QueryCriteria) error {
	// Basic validation
	if criteria.TimeWindow.Start.After(criteria.TimeWindow.End) {
		return core.ValidationError{
			Field:   "time_window",
			Message: "start time must be before end time",
		}
	}

	return nil
}

func (c *collector) handleSubscription(sub *subscription) {
	defer c.wg.Done()
	defer sub.stream.Close()

	// Remove subscription when done
	defer func() {
		c.mu.Lock()
		delete(c.subscriptions, sub.id)
		c.mu.Unlock()
	}()

	for {
		select {
		case <-sub.ctx.Done():
			return
		case <-c.ctx.Done():
			return
		case event := <-c.eventChan:
			// Apply subscription filters
			if !c.applySubscriptionFilters(event, sub.options) {
				c.stats.eventsFiltered.Add(1)
				continue
			}

			// Send event to subscriber
			if !sub.stream.sendEvent(event) {
				// Stream is closed or buffer full
				c.stats.eventsDropped.Add(1)
			}
		}
	}
}

func (c *collector) collectionWorker() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.CollectionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.collectEvents()
		}
	}
}

func (c *collector) collectEvents() {
	c.mu.RLock()
	readers := make(map[string]core.RingBufferReader)
	for k, v := range c.readers {
		readers[k] = v
	}
	c.mu.RUnlock()

	for name, reader := range readers {
		// Read batch of events
		rawEvents, err := reader.ReadBatch(c.config.BatchSize)
		if err != nil {
			c.stats.errors.Add(1)
			c.recordHealthIssue(name, fmt.Sprintf("Failed to read events: %v", err), domain.SeverityWarn)
			continue
		}

		// Process each raw event
		for _, rawEvent := range rawEvents {
			c.stats.bytesProcessed.Add(uint64(len(rawEvent)))

			// Parse event
			event, err := c.parseRawEvent(rawEvent, name)
			if err != nil {
				c.stats.errors.Add(1)
				continue
			}

			// Apply rate limiting
			if c.rateLimiter != nil && !c.rateLimiter.Allow() {
				c.stats.eventsDropped.Add(1)
				continue
			}

			// Apply collector-level filter
			if !c.applyCollectorFilter(event) {
				c.stats.eventsFiltered.Add(1)
				continue
			}

			// Send to subscribers
			select {
			case c.eventChan <- event:
				c.stats.eventsCollected.Add(1)
			default:
				// Channel full, drop event
				c.stats.eventsDropped.Add(1)
			}
		}
	}

	c.stats.lastCollection.Store(time.Now())
}

func (c *collector) parseRawEvent(data []byte, source string) (domain.Event, error) {
	// Determine event type based on source
	eventType := c.getEventTypeForSource(source)

	// Parse using the parser
	event, err := c.parser.Parse(data, eventType)
	if err != nil {
		return domain.Event{}, core.ParseError{
			EventType: eventType,
			DataSize:  len(data),
			Cause:     err,
		}
	}

	// Set source information
	event.Source = domain.SourceType("ebpf")

	return event, nil
}

func (c *collector) getEventTypeForSource(source string) core.EventType {
	// Map source names to event types
	// This is a simplified implementation
	switch source {
	case "syscall_events":
		return core.EventTypeSyscall
	case "connection_events":
		return core.EventTypeNetworkIn
	case "exec_events":
		return core.EventTypeProcessExec
	case "alloc_events":
		return core.EventTypeMemoryAlloc
	case "file_events":
		return core.EventTypeFileIO
	default:
		return core.EventTypeCustom
	}
}

func (c *collector) matchesCriteria(event domain.Event, criteria domain.QueryCriteria) bool {
	// Check time window
	if !event.Timestamp.After(criteria.TimeWindow.Start) || !event.Timestamp.Before(criteria.TimeWindow.End) {
		return false
	}

	// Check event types
	if len(criteria.EventTypes) > 0 {
		found := false
		for _, t := range criteria.EventTypes {
			if event.Type == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check severities
	if len(criteria.Severities) > 0 {
		found := false
		for _, s := range criteria.Severities {
			if event.Severity == s {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check sources
	if len(criteria.Sources) > 0 {
		found := false
		for _, s := range criteria.Sources {
			if event.Source == s {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

func (c *collector) applySubscriptionFilters(event domain.Event, opts domain.SubscriptionOptions) bool {
	// Check event types
	if len(opts.EventTypes) > 0 {
		found := false
		for _, t := range opts.EventTypes {
			if event.Type == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check severities
	if len(opts.Severities) > 0 {
		found := false
		for _, s := range opts.Severities {
			if event.Severity == s {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check resources
	if len(opts.Resources) > 0 {
		// For eBPF collector, we don't have resource refs in events
		// This would need to be implemented based on your resource model
	}

	return true
}

func (c *collector) applyCollectorFilter(event domain.Event) bool {
	filter := c.config.Filter

	// Check event types
	if len(filter.EventTypes) > 0 {
		found := false
		eventType := c.domainEventTypeToCore(event.Type)
		for _, t := range filter.EventTypes {
			if eventType == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check process IDs
	if len(filter.ProcessIDs) > 0 {
		found := false
		for _, pid := range filter.ProcessIDs {
			if event.Context.PID != nil && uint32(*event.Context.PID) == pid {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check container IDs
	if len(filter.ContainerIDs) > 0 {
		found := false
		for _, cid := range filter.ContainerIDs {
			if event.Context.Container == cid {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check namespaces
	if len(filter.Namespaces) > 0 {
		found := false
		for _, ns := range filter.Namespaces {
			if event.Context.Namespace == ns {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check severity
	if filter.MinSeverity != "" && event.Severity < filter.MinSeverity {
		return false
	}

	// Check system processes
	if filter.ExcludeSystemProcesses && event.Context.PID != nil {
		processName := ""
		if event.Context.Labels != nil {
			processName = event.Context.Labels["process_name"]
		}
		info := core.ProcessInfo{
			PID:  uint32(*event.Context.PID),
			Name: processName,
		}
		if event.Context.UID != nil {
			info.UID = uint32(*event.Context.UID)
		}
		if event.Context.GID != nil {
			info.GID = uint32(*event.Context.GID)
		}
		if isSystemProcess(info) {
			return false
		}
	}

	return true
}

func (c *collector) domainEventTypeToCore(eventType domain.EventType) core.EventType {
	// Map domain event types to core event types
	switch eventType {
	case domain.EventTypeSystem:
		return core.EventTypeSyscall
	case domain.EventTypeService:
		return core.EventTypeProcessExec
	default:
		return core.EventTypeCustom
	}
}

func (c *collector) createRingBufferReader(mapName string) (core.RingBufferReader, error) {
	// This would be implemented by the platform-specific code
	// For now, return a placeholder error
	return nil, fmt.Errorf("ring buffer reader creation not implemented in internal package")
}

func (c *collector) getMapInfo(mapName string) (core.MapInfo, error) {
	// Get map information from the map manager
	maps, err := c.manager.ListMaps()
	if err != nil {
		return core.MapInfo{}, err
	}

	for _, info := range maps {
		if info.Name == mapName {
			return info, nil
		}
	}

	return core.MapInfo{}, fmt.Errorf("map %s not found", mapName)
}

func (c *collector) healthChecker() {
	defer c.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.performHealthCheck()
		}
	}
}

func (c *collector) performHealthCheck() {
	c.health.mu.Lock()
	defer c.health.mu.Unlock()

	c.health.lastCheck = time.Now()

	// Check if events are being collected
	lastCollection, ok := c.stats.lastCollection.Load().(time.Time)
	if ok && time.Since(lastCollection) > 5*c.config.CollectionInterval {
		c.health.status = core.HealthStatusDegraded
		c.health.message = "No events collected recently"
		c.health.issues = append(c.health.issues, core.HealthIssue{
			Component: "collection",
			Issue:     fmt.Sprintf("No events collected for %v", time.Since(lastCollection)),
			Severity:  domain.SeverityWarn,
			Since:     lastCollection,
		})
	}

	// Check error rate
	errors := c.stats.errors.Load()
	collected := c.stats.eventsCollected.Load()
	if collected > 0 && float64(errors)/float64(collected) > 0.1 {
		c.health.status = core.HealthStatusDegraded
		c.health.message = "High error rate detected"
	}

	// Clean up old issues
	var activeIssues []core.HealthIssue
	for _, issue := range c.health.issues {
		if time.Since(issue.Since) < 5*time.Minute {
			activeIssues = append(activeIssues, issue)
		}
	}
	c.health.issues = activeIssues

	// Update overall status
	if len(c.health.issues) == 0 {
		c.health.status = core.HealthStatusHealthy
		c.health.message = "All systems operational"
	}
}

func (c *collector) recordHealthIssue(component, issue string, severity domain.Severity) {
	c.health.mu.Lock()
	defer c.health.mu.Unlock()

	c.health.issues = append(c.health.issues, core.HealthIssue{
		Component: component,
		Issue:     issue,
		Severity:  severity,
		Since:     time.Now(),
	})

	// Update status based on severity
	if severity == domain.SeverityError || severity == domain.SeverityCritical {
		c.health.status = core.HealthStatusUnhealthy
		c.health.message = fmt.Sprintf("Critical issue: %s", issue)
	} else if c.health.status == core.HealthStatusHealthy {
		c.health.status = core.HealthStatusDegraded
		c.health.message = fmt.Sprintf("Issue detected: %s", issue)
	}
}

func (c *collector) updateHealthStatus(status core.HealthStatus, message string) {
	c.health.mu.Lock()
	defer c.health.mu.Unlock()

	c.health.status = status
	c.health.message = message
	c.health.lastCheck = time.Now()
}

func (c *collector) countHealthyPrograms() int {
	// In a real implementation, this would check each program's health
	// For now, assume all loaded programs are healthy
	return len(c.programs)
}