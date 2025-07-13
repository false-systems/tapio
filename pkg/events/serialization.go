package events

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/golang/snappy"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
	"google.golang.org/protobuf/proto"
)

// SerializationConfig controls serialization behavior
type SerializationConfig struct {
	Compression     CompressionType
	CompressionLevel int
	MaxBatchSize    int
	EnableChecksum  bool
	BufferSize      int
}

// DefaultSerializationConfig returns default serialization settings
func DefaultSerializationConfig() SerializationConfig {
	return SerializationConfig{
		Compression:     CompressionType_COMPRESSION_LZ4,
		CompressionLevel: 4,
		MaxBatchSize:    1000,
		EnableChecksum:  true,
		BufferSize:      64 * 1024, // 64KB
	}
}

// Serializer provides high-performance event serialization
type Serializer struct {
	config    SerializationConfig
	bufferPool sync.Pool
	
	// Compression encoders
	gzipWriter   *gzip.Writer
	zstdEncoder  *zstd.Encoder
	lz4Writer    *lz4.Writer
	
	// Statistics
	stats SerializationStats
	mu    sync.RWMutex
}

// SerializationStats tracks serialization performance
type SerializationStats struct {
	TotalBytes       uint64
	CompressedBytes  uint64
	EventsSerialized uint64
	BatchesSerialized uint64
	SerializationTime time.Duration
	CompressionTime  time.Duration
	ErrorCount       uint64
}

// NewSerializer creates a new high-performance serializer
func NewSerializer(config SerializationConfig) (*Serializer, error) {
	s := &Serializer{
		config: config,
		bufferPool: sync.Pool{
			New: func() interface{} {
				return bytes.NewBuffer(make([]byte, 0, config.BufferSize))
			},
		},
	}
	
	// Initialize compression encoders
	if err := s.initializeEncoders(); err != nil {
		return nil, fmt.Errorf("failed to initialize encoders: %w", err)
	}
	
	return s, nil
}

// initializeEncoders sets up compression encoders
func (s *Serializer) initializeEncoders() error {
	var err error
	
	// Initialize Zstd encoder
	if s.zstdEncoder, err = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(s.config.CompressionLevel))); err != nil {
		return fmt.Errorf("failed to create zstd encoder: %w", err)
	}
	
	return nil
}

// SerializeEvent serializes a single event
func (s *Serializer) SerializeEvent(event *UnifiedEvent) ([]byte, error) {
	start := time.Now()
	defer func() {
		s.mu.Lock()
		s.stats.SerializationTime += time.Since(start)
		s.stats.EventsSerialized++
		s.mu.Unlock()
	}()
	
	// Validate event
	if err := event.Validate(); err != nil {
		s.mu.Lock()
		s.stats.ErrorCount++
		s.mu.Unlock()
		return nil, fmt.Errorf("event validation failed: %w", err)
	}
	
	// Marshal to protobuf
	data, err := proto.Marshal(event)
	if err != nil {
		s.mu.Lock()
		s.stats.ErrorCount++
		s.mu.Unlock()
		return nil, fmt.Errorf("protobuf marshaling failed: %w", err)
	}
	
	s.mu.Lock()
	s.stats.TotalBytes += uint64(len(data))
	s.mu.Unlock()
	
	// Apply compression if configured
	if s.config.Compression != CompressionType_COMPRESSION_NONE {
		compressed, err := s.compress(data)
		if err != nil {
			s.mu.Lock()
			s.stats.ErrorCount++
			s.mu.Unlock()
			return nil, fmt.Errorf("compression failed: %w", err)
		}
		
		s.mu.Lock()
		s.stats.CompressedBytes += uint64(len(compressed))
		s.mu.Unlock()
		
		return compressed, nil
	}
	
	return data, nil
}

// SerializeBatch serializes multiple events efficiently
func (s *Serializer) SerializeBatch(events []*UnifiedEvent) ([]byte, error) {
	if len(events) == 0 {
		return nil, nil
	}
	
	start := time.Now()
	defer func() {
		s.mu.Lock()
		s.stats.SerializationTime += time.Since(start)
		s.stats.BatchesSerialized++
		s.mu.Unlock()
	}()
	
	// Create batch
	batch := &EventBatch{
		BatchId:   events[0].Id + "_batch",
		CreatedAt: events[0].Timestamp,
		Events:    events,
		Source:    events[0].Source.Type,
	}
	
	// Set compression hint
	batch.Compression = s.config.Compression
	
	// Marshal batch
	data, err := proto.Marshal(batch)
	if err != nil {
		s.mu.Lock()
		s.stats.ErrorCount++
		s.mu.Unlock()
		return nil, fmt.Errorf("batch marshaling failed: %w", err)
	}
	
	s.mu.Lock()
	s.stats.TotalBytes += uint64(len(data))
	s.mu.Unlock()
	
	// Apply compression
	if s.config.Compression != CompressionType_COMPRESSION_NONE {
		compressed, err := s.compress(data)
		if err != nil {
			s.mu.Lock()
			s.stats.ErrorCount++
			s.mu.Unlock()
			return nil, fmt.Errorf("batch compression failed: %w", err)
		}
		
		s.mu.Lock()
		s.stats.CompressedBytes += uint64(len(compressed))
		s.mu.Unlock()
		
		return compressed, nil
	}
	
	return data, nil
}

// compress applies the configured compression algorithm
func (s *Serializer) compress(data []byte) ([]byte, error) {
	start := time.Now()
	defer func() {
		s.mu.Lock()
		s.stats.CompressionTime += time.Since(start)
		s.mu.Unlock()
	}()
	
	switch s.config.Compression {
	case CompressionType_COMPRESSION_GZIP:
		return s.compressGzip(data)
	case CompressionType_COMPRESSION_ZSTD:
		return s.compressZstd(data)
	case CompressionType_COMPRESSION_LZ4:
		return s.compressLZ4(data)
	case CompressionType_COMPRESSION_SNAPPY:
		return s.compressSnappy(data)
	default:
		return data, nil
	}
}

// compressGzip compresses data using gzip
func (s *Serializer) compressGzip(data []byte) ([]byte, error) {
	buf := s.bufferPool.Get().(*bytes.Buffer)
	defer s.bufferPool.Put(buf)
	buf.Reset()
	
	writer, err := gzip.NewWriterLevel(buf, s.config.CompressionLevel)
	if err != nil {
		return nil, err
	}
	
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	
	if err := writer.Close(); err != nil {
		return nil, err
	}
	
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result, nil
}

// compressZstd compresses data using Zstandard
func (s *Serializer) compressZstd(data []byte) ([]byte, error) {
	return s.zstdEncoder.EncodeAll(data, nil), nil
}

// compressLZ4 compresses data using LZ4
func (s *Serializer) compressLZ4(data []byte) ([]byte, error) {
	buf := s.bufferPool.Get().(*bytes.Buffer)
	defer s.bufferPool.Put(buf)
	buf.Reset()
	
	writer := lz4.NewWriter(buf)
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	
	if err := writer.Close(); err != nil {
		return nil, err
	}
	
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result, nil
}

// compressSnappy compresses data using Snappy
func (s *Serializer) compressSnappy(data []byte) ([]byte, error) {
	return snappy.Encode(nil, data), nil
}

// Deserializer provides high-performance event deserialization
type Deserializer struct {
	config    SerializationConfig
	bufferPool sync.Pool
	
	// Decompression decoders
	zstdDecoder *zstd.Decoder
	
	// Statistics
	stats DeserializationStats
	mu    sync.RWMutex
}

// DeserializationStats tracks deserialization performance
type DeserializationStats struct {
	BytesDeserialized    uint64
	EventsDeserialized   uint64
	BatchesDeserialized  uint64
	DeserializationTime  time.Duration
	DecompressionTime    time.Duration
	ErrorCount           uint64
}

// NewDeserializer creates a new high-performance deserializer
func NewDeserializer(config SerializationConfig) (*Deserializer, error) {
	d := &Deserializer{
		config: config,
		bufferPool: sync.Pool{
			New: func() interface{} {
				return bytes.NewBuffer(make([]byte, 0, config.BufferSize))
			},
		},
	}
	
	// Initialize decompression decoders
	if err := d.initializeDecoders(); err != nil {
		return nil, fmt.Errorf("failed to initialize decoders: %w", err)
	}
	
	return d, nil
}

// initializeDecoders sets up decompression decoders
func (d *Deserializer) initializeDecoders() error {
	var err error
	
	// Initialize Zstd decoder
	if d.zstdDecoder, err = zstd.NewReader(nil); err != nil {
		return fmt.Errorf("failed to create zstd decoder: %w", err)
	}
	
	return nil
}

// DeserializeEvent deserializes a single event
func (d *Deserializer) DeserializeEvent(data []byte, compression CompressionType) (*UnifiedEvent, error) {
	start := time.Now()
	defer func() {
		d.mu.Lock()
		d.stats.DeserializationTime += time.Since(start)
		d.stats.EventsDeserialized++
		d.mu.Unlock()
	}()
	
	d.mu.Lock()
	d.stats.BytesDeserialized += uint64(len(data))
	d.mu.Unlock()
	
	// Decompress if needed
	var eventData []byte
	var err error
	
	if compression != CompressionType_COMPRESSION_NONE {
		eventData, err = d.decompress(data, compression)
		if err != nil {
			d.mu.Lock()
			d.stats.ErrorCount++
			d.mu.Unlock()
			return nil, fmt.Errorf("decompression failed: %w", err)
		}
	} else {
		eventData = data
	}
	
	// Unmarshal protobuf
	event := NewEvent()
	if err := proto.Unmarshal(eventData, event); err != nil {
		ReleaseEvent(event)
		d.mu.Lock()
		d.stats.ErrorCount++
		d.mu.Unlock()
		return nil, fmt.Errorf("protobuf unmarshaling failed: %w", err)
	}
	
	return event, nil
}

// DeserializeBatch deserializes a batch of events
func (d *Deserializer) DeserializeBatch(data []byte, compression CompressionType) ([]*UnifiedEvent, error) {
	start := time.Now()
	defer func() {
		d.mu.Lock()
		d.stats.DeserializationTime += time.Since(start)
		d.stats.BatchesDeserialized++
		d.mu.Unlock()
	}()
	
	d.mu.Lock()
	d.stats.BytesDeserialized += uint64(len(data))
	d.mu.Unlock()
	
	// Decompress if needed
	var batchData []byte
	var err error
	
	if compression != CompressionType_COMPRESSION_NONE {
		batchData, err = d.decompress(data, compression)
		if err != nil {
			d.mu.Lock()
			d.stats.ErrorCount++
			d.mu.Unlock()
			return nil, fmt.Errorf("batch decompression failed: %w", err)
		}
	} else {
		batchData = data
	}
	
	// Unmarshal batch
	batch := &EventBatch{}
	if err := proto.Unmarshal(batchData, batch); err != nil {
		d.mu.Lock()
		d.stats.ErrorCount++
		d.mu.Unlock()
		return nil, fmt.Errorf("batch unmarshaling failed: %w", err)
	}
	
	return batch.Events, nil
}

// decompress decompresses data using the specified algorithm
func (d *Deserializer) decompress(data []byte, compression CompressionType) ([]byte, error) {
	start := time.Now()
	defer func() {
		d.mu.Lock()
		d.stats.DecompressionTime += time.Since(start)
		d.mu.Unlock()
	}()
	
	switch compression {
	case CompressionType_COMPRESSION_GZIP:
		return d.decompressGzip(data)
	case CompressionType_COMPRESSION_ZSTD:
		return d.decompressZstd(data)
	case CompressionType_COMPRESSION_LZ4:
		return d.decompressLZ4(data)
	case CompressionType_COMPRESSION_SNAPPY:
		return d.decompressSnappy(data)
	default:
		return data, nil
	}
}

// decompressGzip decompresses gzip data
func (d *Deserializer) decompressGzip(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	
	buf := d.bufferPool.Get().(*bytes.Buffer)
	defer d.bufferPool.Put(buf)
	buf.Reset()
	
	if _, err := io.Copy(buf, reader); err != nil {
		return nil, err
	}
	
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result, nil
}

// decompressZstd decompresses Zstandard data
func (d *Deserializer) decompressZstd(data []byte) ([]byte, error) {
	return d.zstdDecoder.DecodeAll(data, nil)
}

// decompressLZ4 decompresses LZ4 data
func (d *Deserializer) decompressLZ4(data []byte) ([]byte, error) {
	reader := lz4.NewReader(bytes.NewReader(data))
	
	buf := d.bufferPool.Get().(*bytes.Buffer)
	defer d.bufferPool.Put(buf)
	buf.Reset()
	
	if _, err := io.Copy(buf, reader); err != nil {
		return nil, err
	}
	
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result, nil
}

// decompressSnappy decompresses Snappy data
func (d *Deserializer) decompressSnappy(data []byte) ([]byte, error) {
	return snappy.Decode(nil, data)
}

// GetStats returns serialization statistics
func (s *Serializer) GetStats() SerializationStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

// GetStats returns deserialization statistics
func (d *Deserializer) GetStats() DeserializationStats {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.stats
}

// StreamingSerializer provides streaming serialization for large datasets
type StreamingSerializer struct {
	serializer *Serializer
	writer     io.Writer
	batchSize  int
	buffer     []*UnifiedEvent
	ctx        context.Context
}

// NewStreamingSerializer creates a new streaming serializer
func NewStreamingSerializer(ctx context.Context, writer io.Writer, config SerializationConfig) (*StreamingSerializer, error) {
	serializer, err := NewSerializer(config)
	if err != nil {
		return nil, err
	}
	
	return &StreamingSerializer{
		serializer: serializer,
		writer:     writer,
		batchSize:  config.MaxBatchSize,
		buffer:     make([]*UnifiedEvent, 0, config.MaxBatchSize),
		ctx:        ctx,
	}, nil
}

// WriteEvent adds an event to the stream
func (ss *StreamingSerializer) WriteEvent(event *UnifiedEvent) error {
	ss.buffer = append(ss.buffer, event)
	
	if len(ss.buffer) >= ss.batchSize {
		return ss.Flush()
	}
	
	return nil
}

// Flush writes any buffered events
func (ss *StreamingSerializer) Flush() error {
	if len(ss.buffer) == 0 {
		return nil
	}
	
	// Serialize batch
	data, err := ss.serializer.SerializeBatch(ss.buffer)
	if err != nil {
		return fmt.Errorf("failed to serialize batch: %w", err)
	}
	
	// Write to stream
	if _, err := ss.writer.Write(data); err != nil {
		return fmt.Errorf("failed to write batch: %w", err)
	}
	
	// Clear buffer
	ss.buffer = ss.buffer[:0]
	
	return nil
}

// Close flushes any remaining events and closes the stream
func (ss *StreamingSerializer) Close() error {
	return ss.Flush()
}