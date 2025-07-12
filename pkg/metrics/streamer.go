package metrics

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// PrometheusMetricStreamer implements MetricStreamer with memory-efficient streaming and context propagation
type PrometheusMetricStreamer[T MetricType] struct {
	// Dependencies
	logger *slog.Logger

	// State management
	mu             sync.RWMutex
	running        int32
	shutdown       chan struct{}
	shutdownOnce   sync.Once
	streams        map[string]*activeStream[T]
	streamSequence int64

	// Configuration
	config StreamerConfig

	// Performance tracking
	stats StreamerStats

	// Memory management
	streamPool    sync.Pool
	bufferPool    sync.Pool
	compressionPool sync.Pool

	// Background workers
	workers       int
	workerQueue   chan streamWork[T]
	workerShutdown chan struct{}
	workerWg      sync.WaitGroup
}

// activeStream represents an active streaming session
type activeStream[T MetricType] struct {
	id             string
	ctx            context.Context
	cancel         context.CancelFunc
	options        StreamOptions
	resultChannel  chan StreamResult[T]
	buffer         *StreamBuffer[T]
	compressor     *StreamCompressor
	encoder        StreamEncoder[T]
	stats          StreamStats
	started        time.Time
	lastActivity   time.Time
	errorCount     int64
	bytesStreamed  int64
	itemsStreamed  int64
}

// StreamBuffer provides memory-efficient buffering with different strategies
type StreamBuffer[T MetricType] struct {
	strategy     BufferingStrategy
	options      BufferingOptions
	buffer       []T
	writeIndex   int
	readIndex    int
	size         int
	capacity     int
	memoryUsage  int64
	flushTicker  *time.Ticker
	mu           sync.RWMutex
	diskSpillover *DiskSpillover[T]
}

// StreamCompressor handles stream compression
type StreamCompressor struct {
	algorithm CompressionAlgorithm
	level     int
	enabled   bool
	writer    io.Writer
	stats     CompressionStats
}

// DiskSpillover handles memory overflow to disk
type DiskSpillover[T MetricType] struct {
	enabled       bool
	directory     string
	maxFileSize   int64
	maxFiles      int
	currentFile   string
	bytesWritten  int64
	filesCreated  int
	mu            sync.Mutex
}

// StreamEncoder handles metric encoding for different formats
type StreamEncoder[T MetricType] interface {
	Encode(item T) ([]byte, error)
	EncodeBatch(items []T) ([]byte, error)
	GetFormat() StreamFormat
	GetContentType() string
}

// streamWork represents work to be processed by workers
type streamWork[T MetricType] struct {
	streamID   string
	items      []T
	timestamp  time.Time
	sequence   int64
	priority   WorkPriority
	context    context.Context
}

// Supporting types and configurations
type (
	StreamerConfig struct {
		// Worker configuration
		WorkerCount      int
		WorkerQueueSize  int
		WorkerTimeout    time.Duration

		// Default stream settings
		DefaultBufferSize    int
		DefaultFlushInterval time.Duration
		DefaultCompression   CompressionAlgorithm
		DefaultFormat        StreamFormat

		// Memory management
		MaxStreams           int
		MaxMemoryUsage       int64
		MemoryCheckInterval  time.Duration
		GCThreshold          float64

		// Performance tuning
		EnableProfiling      bool
		EnableTracing        bool
		MetricsInterval      time.Duration

		// Error handling
		ErrorBufferSize      int
		MaxRetries           int
		RetryBackoff         time.Duration

		// Disk spillover
		EnableDiskSpillover  bool
		SpilloverDirectory   string
		SpilloverThreshold   int64
	}

	StreamerStats struct {
		ActiveStreams     int64
		TotalStreams      int64
		BytesStreamed     int64
		ItemsStreamed     int64
		ErrorCount        int64
		CompressionRatio  float64
		MemoryUsage       int64
		DiskUsage         int64
		AverageLatency    time.Duration
		LastActivity      time.Time
	}

	StreamStats struct {
		BytesStreamed     int64
		ItemsStreamed     int64
		CompressionRatio  float64
		AverageItemSize   int64
		BufferUtilization float64
		ErrorCount        int64
		StartTime         time.Time
		LastActivity      time.Time
	}

	CompressionStats struct {
		BytesIn           int64
		BytesOut          int64
		CompressionRatio  float64
		CompressionTime   time.Duration
		CompressionCount  int64
	}

	StreamFormat string
	WorkPriority string
)

// Stream format constants
const (
	StreamFormatJSON       StreamFormat = "json"
	StreamFormatProtobuf   StreamFormat = "protobuf"
	StreamFormatAvro       StreamFormat = "avro"
	StreamFormatMessagePack StreamFormat = "messagepack"
)

// Work priority constants
const (
	WorkPriorityHigh   WorkPriority = "high"
	WorkPriorityMedium WorkPriority = "medium"
	WorkPriorityLow    WorkPriority = "low"
)

// NewPrometheusMetricStreamer creates a new memory-efficient metric streamer
func NewPrometheusMetricStreamer[T MetricType](config StreamerConfig, logger *slog.Logger) *PrometheusMetricStreamer[T] {
	// Apply defaults
	applyStreamerDefaults(&config)

	if logger == nil {
		logger = slog.Default().With("component", "metric-streamer")
	}

	streamer := &PrometheusMetricStreamer[T]{
		logger:         logger,
		shutdown:       make(chan struct{}),
		streams:        make(map[string]*activeStream[T]),
		config:         config,
		workers:        config.WorkerCount,
		workerQueue:    make(chan streamWork[T], config.WorkerQueueSize),
		workerShutdown: make(chan struct{}),
		stats: StreamerStats{
			LastActivity: time.Now(),
		},
	}

	// Initialize object pools for memory efficiency
	streamer.initializePools()

	// Start background workers
	streamer.startWorkers()

	// Start monitoring
	go streamer.runMonitoring()

	atomic.StoreInt32(&streamer.running, 1)

	return streamer
}

// StartStream starts streaming metrics with context propagation
func (s *PrometheusMetricStreamer[T]) StartStream(ctx context.Context, opts StreamOptions) (<-chan StreamResult[T], error) {
	if atomic.LoadInt32(&s.running) == 0 {
		return nil, fmt.Errorf("streamer is not running")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check stream limit
	if len(s.streams) >= s.config.MaxStreams {
		return nil, fmt.Errorf("maximum number of streams (%d) reached", s.config.MaxStreams)
	}

	// Generate unique stream ID
	streamID := s.generateStreamID()

	// Create stream context with cancellation
	streamCtx, cancel := context.WithCancel(ctx)

	// Apply defaults to options
	s.applyStreamDefaults(&opts)

	// Create stream buffer
	buffer, err := s.createStreamBuffer(opts.BufferSize, opts)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stream buffer: %w", err)
	}

	// Create compressor if compression is enabled
	var compressor *StreamCompressor
	if opts.Compression {
		compressor = s.createCompressor(s.config.DefaultCompression)
	}

	// Create encoder based on format
	encoder, err := s.createEncoder(s.config.DefaultFormat)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create encoder: %w", err)
	}

	// Create result channel
	resultChannel := make(chan StreamResult[T], opts.BufferSize)

	// Create active stream
	stream := &activeStream[T]{
		id:            streamID,
		ctx:           streamCtx,
		cancel:        cancel,
		options:       opts,
		resultChannel: resultChannel,
		buffer:        buffer,
		compressor:    compressor,
		encoder:       encoder,
		started:       time.Now(),
		lastActivity:  time.Now(),
		stats: StreamStats{
			StartTime:    time.Now(),
			LastActivity: time.Now(),
		},
	}

	// Register stream
	s.streams[streamID] = stream

	// Start stream processor
	go s.processStream(stream)

	// Update statistics
	atomic.AddInt64(&s.stats.ActiveStreams, 1)
	atomic.AddInt64(&s.stats.TotalStreams, 1)
	s.stats.LastActivity = time.Now()

	s.logger.Info("Stream started",
		"stream_id", streamID,
		"buffer_size", opts.BufferSize,
		"compression", opts.Compression,
		"batching", opts.EnableBatching)

	return resultChannel, nil
}

// StopStream stops an active stream
func (s *PrometheusMetricStreamer[T]) StopStream(streamID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	stream, exists := s.streams[streamID]
	if !exists {
		return fmt.Errorf("stream %s not found", streamID)
	}

	// Cancel stream context
	stream.cancel()

	// Remove from active streams
	delete(s.streams, streamID)

	// Update statistics
	atomic.AddInt64(&s.stats.ActiveStreams, -1)

	s.logger.Info("Stream stopped",
		"stream_id", streamID,
		"duration", time.Since(stream.started),
		"items_streamed", stream.stats.ItemsStreamed,
		"bytes_streamed", stream.stats.BytesStreamed)

	return nil
}

// GetActiveStreams returns information about active streams
func (s *PrometheusMetricStreamer[T]) GetActiveStreams() []StreamInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	streams := make([]StreamInfo, 0, len(s.streams))
	for _, stream := range s.streams {
		streams = append(streams, StreamInfo{
			StreamID:    stream.id,
			StartTime:   stream.started,
			MetricCount: stream.stats.ItemsStreamed,
			BytesSent:   stream.stats.BytesStreamed,
			ErrorCount:  stream.stats.ErrorCount,
			Status:      s.getStreamStatus(stream),
			Options:     stream.options,
		})
	}

	return streams
}

// SetBuffering configures stream buffering strategy
func (s *PrometheusMetricStreamer[T]) SetBuffering(strategy BufferingStrategy, options BufferingOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate buffering configuration
	if err := s.validateBufferingConfig(strategy, options); err != nil {
		return fmt.Errorf("invalid buffering config: %w", err)
	}

	// Update all active streams
	for _, stream := range s.streams {
		stream.buffer.strategy = strategy
		stream.buffer.options = options

		// Resize buffer if needed
		if options.BufferSize != stream.buffer.capacity {
			s.resizeStreamBuffer(stream.buffer, options.BufferSize)
		}
	}

	s.logger.Info("Buffering configuration updated",
		"strategy", strategy,
		"buffer_size", options.BufferSize,
		"flush_interval", options.FlushInterval)

	return nil
}

// SetCompression configures stream compression
func (s *PrometheusMetricStreamer[T]) SetCompression(enabled bool, algorithm CompressionAlgorithm) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update configuration
	s.config.DefaultCompression = algorithm

	// Update all active streams
	for _, stream := range s.streams {
		if enabled && stream.compressor == nil {
			stream.compressor = s.createCompressor(algorithm)
		} else if !enabled && stream.compressor != nil {
			stream.compressor = nil
		} else if enabled && stream.compressor != nil {
			stream.compressor.algorithm = algorithm
		}
	}

	s.logger.Info("Compression configuration updated",
		"enabled", enabled,
		"algorithm", algorithm)

	return nil
}

// GetStats returns streamer statistics
func (s *PrometheusMetricStreamer[T]) GetStats() StreamerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := s.stats
	stats.ActiveStreams = int64(len(s.streams))

	// Calculate memory usage
	var memoryUsage int64
	for _, stream := range s.streams {
		memoryUsage += stream.buffer.memoryUsage
	}
	stats.MemoryUsage = memoryUsage

	return stats
}

// Close gracefully shuts down the streamer
func (s *PrometheusMetricStreamer[T]) Close(ctx context.Context) error {
	var closeError error

	s.shutdownOnce.Do(func() {
		s.logger.Info("Starting streamer shutdown")

		// Stop accepting new streams
		atomic.StoreInt32(&s.running, 0)

		// Stop all active streams
		closeError = s.stopAllStreams(ctx)

		// Stop background workers
		s.stopWorkers(ctx)

		s.logger.Info("Streamer shutdown completed", "error", closeError)
	})

	return closeError
}

// Private methods

func (s *PrometheusMetricStreamer[T]) generateStreamID() string {
	sequence := atomic.AddInt64(&s.streamSequence, 1)
	return fmt.Sprintf("stream-%d-%d", time.Now().Unix(), sequence)
}

func (s *PrometheusMetricStreamer[T]) applyStreamDefaults(opts *StreamOptions) {
	if opts.BufferSize == 0 {
		opts.BufferSize = s.config.DefaultBufferSize
	}
	if opts.FlushInterval == 0 {
		opts.FlushInterval = s.config.DefaultFlushInterval
	}
	if opts.BatchSize == 0 {
		opts.BatchSize = 100
	}
}

func (s *PrometheusMetricStreamer[T]) createStreamBuffer(size int, opts StreamOptions) (*StreamBuffer[T], error) {
	buffer := &StreamBuffer[T]{
		strategy:      BufferingStrategyMemory, // Default strategy
		buffer:        make([]T, size),
		capacity:      size,
		flushTicker:   time.NewTicker(opts.FlushInterval),
	}

	// Initialize disk spillover if enabled
	if s.config.EnableDiskSpillover {
		spillover := &DiskSpillover[T]{
			enabled:   true,
			directory: s.config.SpilloverDirectory,
			maxFileSize: 100 * 1024 * 1024, // 100MB
			maxFiles:    10,
		}
		buffer.diskSpillover = spillover
	}

	return buffer, nil
}

func (s *PrometheusMetricStreamer[T]) createCompressor(algorithm CompressionAlgorithm) *StreamCompressor {
	return &StreamCompressor{
		algorithm: algorithm,
		level:     6, // Default compression level
		enabled:   true,
	}
}

func (s *PrometheusMetricStreamer[T]) createEncoder(format StreamFormat) (StreamEncoder[T], error) {
	switch format {
	case StreamFormatJSON:
		return NewJSONStreamEncoder[T](), nil
	case StreamFormatProtobuf:
		return NewProtobufStreamEncoder[T](), nil
	case StreamFormatAvro:
		return NewAvroStreamEncoder[T](), nil
	case StreamFormatMessagePack:
		return NewMessagePackStreamEncoder[T](), nil
	default:
		return nil, fmt.Errorf("unsupported stream format: %s", format)
	}
}

func (s *PrometheusMetricStreamer[T]) processStream(stream *activeStream[T]) {
	defer func() {
		// Cleanup on stream completion
		stream.buffer.flushTicker.Stop()
		close(stream.resultChannel)

		// Final flush
		s.flushStreamBuffer(stream)

		s.logger.Debug("Stream processor completed", "stream_id", stream.id)
	}()

	flushTicker := time.NewTicker(stream.options.FlushInterval)
	defer flushTicker.Stop()

	for {
		select {
		case <-stream.ctx.Done():
			return
		case <-flushTicker.C:
			s.flushStreamBuffer(stream)
		case work := <-s.workerQueue:
			if work.streamID == stream.id {
				s.processStreamWork(stream, work)
			}
		}
	}
}

func (s *PrometheusMetricStreamer[T]) processStreamWork(stream *activeStream[T], work streamWork[T]) {
	start := time.Now()

	// Add items to buffer
	for _, item := range work.items {
		if !s.addToStreamBuffer(stream.buffer, item) {
			// Buffer is full, handle overflow
			s.handleBufferOverflow(stream, item)
		}
	}

	// Check if buffer should be flushed
	if s.shouldFlushBuffer(stream.buffer) {
		s.flushStreamBuffer(stream)
	}

	// Update stream statistics
	duration := time.Since(start)
	atomic.AddInt64(&stream.stats.ItemsStreamed, int64(len(work.items)))
	stream.stats.LastActivity = time.Now()
	stream.lastActivity = time.Now()

	// Update average latency
	s.updateStreamLatency(stream, duration)
}

func (s *PrometheusMetricStreamer[T]) addToStreamBuffer(buffer *StreamBuffer[T], item T) bool {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	if buffer.size >= buffer.capacity {
		return false // Buffer is full
	}

	buffer.buffer[buffer.writeIndex] = item
	buffer.writeIndex = (buffer.writeIndex + 1) % buffer.capacity
	buffer.size++

	// Update memory usage estimate
	buffer.memoryUsage += s.estimateItemSize(item)

	return true
}

func (s *PrometheusMetricStreamer[T]) flushStreamBuffer(stream *activeStream[T]) {
	buffer := stream.buffer
	buffer.mu.Lock()

	if buffer.size == 0 {
		buffer.mu.Unlock()
		return
	}

	// Extract items from buffer
	items := make([]T, buffer.size)
	for i := 0; i < buffer.size; i++ {
		index := (buffer.readIndex + i) % buffer.capacity
		items[i] = buffer.buffer[index]
	}

	// Reset buffer
	buffer.readIndex = buffer.writeIndex
	buffer.size = 0
	buffer.memoryUsage = 0

	buffer.mu.Unlock()

	// Encode items
	data, err := stream.encoder.EncodeBatch(items)
	if err != nil {
		atomic.AddInt64(&stream.stats.ErrorCount, 1)
		s.logger.Error("Failed to encode batch", "stream_id", stream.id, "error", err)
		return
	}

	// Compress if enabled
	if stream.compressor != nil && stream.compressor.enabled {
		compressedData, compressionRatio, err := s.compressData(stream.compressor, data)
		if err != nil {
			s.logger.Warn("Compression failed, sending uncompressed", "stream_id", stream.id, "error", err)
		} else {
			data = compressedData
			stream.stats.CompressionRatio = compressionRatio
		}
	}

	// Create stream result
	result := StreamResult[T]{
		Metrics:      items,
		StreamID:     stream.id,
		Timestamp:    time.Now(),
		Sequence:     atomic.AddInt64(&s.streamSequence, 1),
		EndOfStream:  false,
	}

	// Send result
	select {
	case stream.resultChannel <- result:
		atomic.AddInt64(&stream.stats.BytesStreamed, int64(len(data)))
		atomic.AddInt64(&s.stats.BytesStreamed, int64(len(data)))
		atomic.AddInt64(&s.stats.ItemsStreamed, int64(len(items)))
	case <-stream.ctx.Done():
		return
	default:
		// Channel is full, log warning
		s.logger.Warn("Stream result channel full, dropping batch", "stream_id", stream.id)
		atomic.AddInt64(&stream.stats.ErrorCount, 1)
		atomic.AddInt64(&s.stats.ErrorCount, 1)
	}
}

func (s *PrometheusMetricStreamer[T]) shouldFlushBuffer(buffer *StreamBuffer[T]) bool {
	buffer.mu.RLock()
	defer buffer.mu.RUnlock()

	// Flush based on size threshold
	if buffer.size >= buffer.options.FlushThreshold {
		return true
	}

	// Flush based on memory usage
	if buffer.memoryUsage >= buffer.options.MemoryLimit {
		return true
	}

	return false
}

func (s *PrometheusMetricStreamer[T]) handleBufferOverflow(stream *activeStream[T], item T) {
	switch stream.buffer.strategy {
	case BufferingStrategyMemory:
		// Drop oldest item
		s.dropOldestFromBuffer(stream.buffer)
		s.addToStreamBuffer(stream.buffer, item)

	case BufferingStrategyDisk:
		// Spill to disk
		if stream.buffer.diskSpillover != nil && stream.buffer.diskSpillover.enabled {
			s.spillToDisk(stream.buffer.diskSpillover, item)
		}

	case BufferingStrategyDropNewest:
		// Drop the new item
		atomic.AddInt64(&stream.stats.ErrorCount, 1)
		s.logger.Warn("Dropping newest item due to buffer overflow", "stream_id", stream.id)

	default:
		// Default: drop oldest
		s.dropOldestFromBuffer(stream.buffer)
		s.addToStreamBuffer(stream.buffer, item)
	}
}

func (s *PrometheusMetricStreamer[T]) dropOldestFromBuffer(buffer *StreamBuffer[T]) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	if buffer.size == 0 {
		return
	}

	// Clear oldest item
	var zero T
	buffer.buffer[buffer.readIndex] = zero
	buffer.readIndex = (buffer.readIndex + 1) % buffer.capacity
	buffer.size--
}

func (s *PrometheusMetricStreamer[T]) spillToDisk(spillover *DiskSpillover[T], item T) {
	spillover.mu.Lock()
	defer spillover.mu.Unlock()

	// Implementation would write item to disk file
	// This is a simplified version
	spillover.bytesWritten += s.estimateItemSize(item)
	s.logger.Debug("Item spilled to disk", "bytes_written", spillover.bytesWritten)
}

func (s *PrometheusMetricStreamer[T]) compressData(compressor *StreamCompressor, data []byte) ([]byte, float64, error) {
	start := time.Now()

	// Implementation would compress data using the specified algorithm
	// This is a simplified version that returns the original data
	compressedData := data
	compressionRatio := 1.0

	// Update compression statistics
	compressor.stats.BytesIn += int64(len(data))
	compressor.stats.BytesOut += int64(len(compressedData))
	compressor.stats.CompressionTime += time.Since(start)
	compressor.stats.CompressionCount++

	if len(data) > 0 {
		compressionRatio = float64(len(compressedData)) / float64(len(data))
	}

	return compressedData, compressionRatio, nil
}

func (s *PrometheusMetricStreamer[T]) estimateItemSize(item T) int64 {
	// Rough size estimation - could be more sophisticated
	return 1024 // Default estimate: 1KB per item
}

func (s *PrometheusMetricStreamer[T]) updateStreamLatency(stream *activeStream[T], duration time.Duration) {
	// Simple moving average
	if stream.stats.AverageLatency == 0 {
		stream.stats.AverageLatency = duration
	} else {
		stream.stats.AverageLatency = (stream.stats.AverageLatency + duration) / 2
	}
}

func (s *PrometheusMetricStreamer[T]) getStreamStatus(stream *activeStream[T]) StreamStatus {
	select {
	case <-stream.ctx.Done():
		return StreamStatusStopped
	default:
		if stream.stats.ErrorCount > 10 {
			return StreamStatusError
		}
		return StreamStatusActive
	}
}

func (s *PrometheusMetricStreamer[T]) validateBufferingConfig(strategy BufferingStrategy, options BufferingOptions) error {
	if options.BufferSize <= 0 {
		return fmt.Errorf("buffer size must be positive")
	}
	if options.FlushInterval <= 0 {
		return fmt.Errorf("flush interval must be positive")
	}
	if options.FlushThreshold <= 0 {
		return fmt.Errorf("flush threshold must be positive")
	}
	return nil
}

func (s *PrometheusMetricStreamer[T]) resizeStreamBuffer(buffer *StreamBuffer[T], newSize int) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	// Create new buffer
	newBuffer := make([]T, newSize)

	// Copy existing items
	copyCount := buffer.size
	if copyCount > newSize {
		copyCount = newSize
	}

	for i := 0; i < copyCount; i++ {
		index := (buffer.readIndex + i) % buffer.capacity
		newBuffer[i] = buffer.buffer[index]
	}

	// Update buffer
	buffer.buffer = newBuffer
	buffer.capacity = newSize
	buffer.readIndex = 0
	buffer.writeIndex = copyCount
	buffer.size = copyCount
}

func (s *PrometheusMetricStreamer[T]) stopAllStreams(ctx context.Context) error {
	s.mu.Lock()
	streams := make([]*activeStream[T], 0, len(s.streams))
	for _, stream := range s.streams {
		streams = append(streams, stream)
	}
	s.mu.Unlock()

	// Stop all streams
	for _, stream := range streams {
		stream.cancel()
	}

	// Wait for all streams to complete
	timeout := time.After(5 * time.Second)
	for len(s.streams) > 0 {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for streams to stop")
		case <-time.After(100 * time.Millisecond):
			// Check again
		}
	}

	return nil
}

func (s *PrometheusMetricStreamer[T]) initializePools() {
	s.streamPool.New = func() interface{} {
		return &activeStream[T]{}
	}

	s.bufferPool.New = func() interface{} {
		return make([]T, s.config.DefaultBufferSize)
	}

	s.compressionPool.New = func() interface{} {
		return &StreamCompressor{}
	}
}

func (s *PrometheusMetricStreamer[T]) startWorkers() {
	for i := 0; i < s.workers; i++ {
		s.workerWg.Add(1)
		go s.worker(i)
	}
}

func (s *PrometheusMetricStreamer[T]) stopWorkers(ctx context.Context) {
	close(s.workerShutdown)

	// Wait for workers to finish
	done := make(chan struct{})
	go func() {
		s.workerWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("All workers stopped gracefully")
	case <-ctx.Done():
		s.logger.Warn("Worker shutdown timeout")
	}
}

func (s *PrometheusMetricStreamer[T]) worker(id int) {
	defer s.workerWg.Done()

	logger := s.logger.With("worker_id", id)
	logger.Debug("Stream worker started")

	for {
		select {
		case <-s.workerShutdown:
			logger.Debug("Stream worker stopped")
			return
		case work := <-s.workerQueue:
			s.processWorkerTask(work)
		}
	}
}

func (s *PrometheusMetricStreamer[T]) processWorkerTask(work streamWork[T]) {
	// Find the target stream
	s.mu.RLock()
	stream, exists := s.streams[work.streamID]
	s.mu.RUnlock()

	if !exists {
		return // Stream no longer exists
	}

	// Process the work
	s.processStreamWork(stream, work)
}

func (s *PrometheusMetricStreamer[T]) runMonitoring() {
	ticker := time.NewTicker(s.config.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.shutdown:
			return
		case <-ticker.C:
			s.reportMetrics()
		}
	}
}

func (s *PrometheusMetricStreamer[T]) reportMetrics() {
	stats := s.GetStats()

	s.logger.Info("Streamer metrics",
		"active_streams", stats.ActiveStreams,
		"total_streams", stats.TotalStreams,
		"bytes_streamed", stats.BytesStreamed,
		"items_streamed", stats.ItemsStreamed,
		"memory_usage", stats.MemoryUsage,
		"compression_ratio", stats.CompressionRatio)
}

// Utility functions

func applyStreamerDefaults(config *StreamerConfig) {
	if config.WorkerCount == 0 {
		config.WorkerCount = 5
	}
	if config.WorkerQueueSize == 0 {
		config.WorkerQueueSize = 1000
	}
	if config.WorkerTimeout == 0 {
		config.WorkerTimeout = 30 * time.Second
	}
	if config.DefaultBufferSize == 0 {
		config.DefaultBufferSize = 1000
	}
	if config.DefaultFlushInterval == 0 {
		config.DefaultFlushInterval = time.Second
	}
	if config.MaxStreams == 0 {
		config.MaxStreams = 100
	}
	if config.MaxMemoryUsage == 0 {
		config.MaxMemoryUsage = 500 * 1024 * 1024 // 500MB
	}
	if config.MemoryCheckInterval == 0 {
		config.MemoryCheckInterval = 30 * time.Second
	}
	if config.MetricsInterval == 0 {
		config.MetricsInterval = time.Minute
	}
	if config.ErrorBufferSize == 0 {
		config.ErrorBufferSize = 100
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.RetryBackoff == 0 {
		config.RetryBackoff = time.Second
	}
	if config.SpilloverThreshold == 0 {
		config.SpilloverThreshold = 100 * 1024 * 1024 // 100MB
	}
}

// Encoder implementations (simplified interfaces)

func NewJSONStreamEncoder[T MetricType]() StreamEncoder[T] {
	return &jsonStreamEncoder[T]{}
}

func NewProtobufStreamEncoder[T MetricType]() StreamEncoder[T] {
	return &protobufStreamEncoder[T]{}
}

func NewAvroStreamEncoder[T MetricType]() StreamEncoder[T] {
	return &avroStreamEncoder[T]{}
}

func NewMessagePackStreamEncoder[T MetricType]() StreamEncoder[T] {
	return &messagePackStreamEncoder[T]{}
}

// Simplified encoder implementations
type jsonStreamEncoder[T MetricType] struct{}
type protobufStreamEncoder[T MetricType] struct{}
type avroStreamEncoder[T MetricType] struct{}
type messagePackStreamEncoder[T MetricType] struct{}

func (e *jsonStreamEncoder[T]) Encode(item T) ([]byte, error) {
	return []byte("{}"), nil // Simplified
}

func (e *jsonStreamEncoder[T]) EncodeBatch(items []T) ([]byte, error) {
	return []byte("[]"), nil // Simplified
}

func (e *jsonStreamEncoder[T]) GetFormat() StreamFormat {
	return StreamFormatJSON
}

func (e *jsonStreamEncoder[T]) GetContentType() string {
	return "application/json"
}

func (e *protobufStreamEncoder[T]) Encode(item T) ([]byte, error) {
	return []byte{}, nil // Simplified
}

func (e *protobufStreamEncoder[T]) EncodeBatch(items []T) ([]byte, error) {
	return []byte{}, nil // Simplified
}

func (e *protobufStreamEncoder[T]) GetFormat() StreamFormat {
	return StreamFormatProtobuf
}

func (e *protobufStreamEncoder[T]) GetContentType() string {
	return "application/x-protobuf"
}

func (e *avroStreamEncoder[T]) Encode(item T) ([]byte, error) {
	return []byte{}, nil // Simplified
}

func (e *avroStreamEncoder[T]) EncodeBatch(items []T) ([]byte, error) {
	return []byte{}, nil // Simplified
}

func (e *avroStreamEncoder[T]) GetFormat() StreamFormat {
	return StreamFormatAvro
}

func (e *avroStreamEncoder[T]) GetContentType() string {
	return "application/avro"
}

func (e *messagePackStreamEncoder[T]) Encode(item T) ([]byte, error) {
	return []byte{}, nil // Simplified
}

func (e *messagePackStreamEncoder[T]) EncodeBatch(items []T) ([]byte, error) {
	return []byte{}, nil // Simplified
}

func (e *messagePackStreamEncoder[T]) GetFormat() StreamFormat {
	return StreamFormatMessagePack
}

func (e *messagePackStreamEncoder[T]) GetContentType() string {
	return "application/msgpack"
}