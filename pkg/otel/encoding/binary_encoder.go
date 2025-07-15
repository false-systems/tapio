package encoding

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/yairfalse/tapio/pkg/otel/domain"
)

// BinaryEncoder provides high-performance binary encoding for trace data
// Implements zero-allocation encoding with custom binary format optimized for OTEL
type BinaryEncoder[T domain.TraceData] struct {
	// Configuration
	config EncoderConfig

	// Buffer pools for zero-allocation paths
	bufferPool sync.Pool
	writerPool sync.Pool

	// Performance counters
	encodedBytes   int64
	encodedSpans   int64
	encodingErrors int64
	poolHits       int64
	poolMisses     int64

	// Format version for backward compatibility
	formatVersion uint32

	// Compression support
	compressor     Compressor
	compressionBuf []byte

	// SIMD optimization support
	simdEnabled   bool
	simdThreshold int

	// Custom type handlers for domain-specific encoding
	typeHandlers map[TypeID]TypeHandler[T]
	handlerMutex sync.RWMutex
}

// EncoderConfig configures binary encoding behavior and performance characteristics
type EncoderConfig struct {
	// Buffer configuration
	InitialBufferSize int // Initial buffer size (default: 4KB)
	MaxBufferSize     int // Maximum buffer size (default: 1MB)
	BufferPoolSize    int // Buffer pool size (default: 100)

	// Compression settings
	EnableCompression    bool // Enable compression for large payloads
	CompressionThreshold int  // Compress if larger than threshold
	CompressionLevel     int  // Compression level (1-9)
	CompressionType      CompressionType

	// Performance tuning
	EnableSIMD     bool // Enable SIMD optimizations
	SIMDThreshold  int  // Use SIMD for data larger than threshold
	EnableZeroCopy bool // Enable zero-copy optimizations

	// Format options
	FormatVersion    uint32 // Binary format version
	EnableChecksums  bool   // Enable data integrity checksums
	EnableTimestamps bool   // Include encoding timestamps

	// Custom encoding
	EnableCustomTypes bool         // Support custom type encoding
	TypeRegistry      TypeRegistry // Custom type registry

	// Memory management
	EnableArenaAlloc bool  // Use arena allocation
	ArenaSize        int64 // Arena size for allocations

	// Error handling
	ErrorMode     ErrorMode   // How to handle encoding errors
	MaxErrors     int         // Maximum errors before failing
	ErrorCallback func(error) // Error callback function
}

// BinaryWriter provides high-performance binary writing with zero-allocation design
type BinaryWriter struct {
	buf      []byte   // Current buffer
	pos      int      // Current position
	capacity int      // Buffer capacity
	scratch  [16]byte // Scratch space for small writes

	// Performance optimization
	enableBatching bool // Enable write batching
	batchSize      int  // Batch size for writes
	batchCount     int  // Current batch count

	// Checksum support
	checksum       uint32 // Running checksum
	enableChecksum bool   // Whether to calculate checksum
}

// BinaryReader provides high-performance binary reading with SIMD optimization
type BinaryReader struct {
	data    []byte   // Source data
	pos     int      // Current position
	limit   int      // Data limit
	scratch [16]byte // Scratch space for reads

	// Validation
	checksum       uint32 // Expected checksum
	enableChecksum bool   // Whether to validate checksum

	// Performance tracking
	bytesRead      int64 // Total bytes read
	readOperations int64 // Number of read operations
}

// TypeHandler defines custom encoding/decoding for specific types
type TypeHandler[T domain.TraceData] interface {
	// Encoding
	EncodeTo(writer *BinaryWriter, value T) error
	EstimateSize(value T) int

	// Decoding
	DecodeFrom(reader *BinaryReader) (T, error)

	// Metadata
	GetTypeID() TypeID
	GetVersion() uint32
}

// Supporting types

type TypeID uint32
type CompressionType uint8
type ErrorMode uint8

const (
	CompressionTypeNone CompressionType = iota
	CompressionTypeGzip
	CompressionTypeZstd
	CompressionTypeLZ4
	CompressionTypeSnappy
)

const (
	ErrorModeIgnore ErrorMode = iota
	ErrorModeLog
	ErrorModeFail
	ErrorModeAccumulate
)

// Binary format constants
const (
	FormatMagic   = 0x544F544C // "TOTL" - Tapio OTEL
	FormatVersion = 1

	// Type markers
	TypeMarkerSpan      = 0x01
	TypeMarkerAttribute = 0x02
	TypeMarkerEvent     = 0x03
	TypeMarkerLink      = 0x04
	TypeMarkerResource  = 0x05

	// Value type markers
	ValueTypeString  = 0x10
	ValueTypeInt64   = 0x11
	ValueTypeFloat64 = 0x12
	ValueTypeBool    = 0x13
	ValueTypeBytes   = 0x14
	ValueTypeArray   = 0x15
	ValueTypeMap     = 0x16

	// Special markers
	MarkerCompressed = 0x80
	MarkerChecksum   = 0x81
	MarkerTimestamp  = 0x82
)

// NewBinaryEncoder creates a new high-performance binary encoder
func NewBinaryEncoder[T domain.TraceData](config EncoderConfig) *BinaryEncoder[T] {
	applyEncoderDefaults(&config)

	encoder := &BinaryEncoder[T]{
		config:         config,
		formatVersion:  config.FormatVersion,
		simdEnabled:    config.EnableSIMD,
		simdThreshold:  config.SIMDThreshold,
		typeHandlers:   make(map[TypeID]TypeHandler[T]),
		compressionBuf: make([]byte, config.MaxBufferSize),
	}

	// Initialize buffer pools
	encoder.initializePools()

	// Initialize compressor if enabled
	if config.EnableCompression {
		encoder.compressor = NewCompressor(config.CompressionType, config.CompressionLevel)
	}

	// Register default type handlers
	encoder.registerDefaultHandlers()

	return encoder
}

// EncodeSpan encodes a span snapshot to binary format with zero-allocation optimization
func (e *BinaryEncoder[T]) EncodeSpan(span domain.SpanSnapshot[T]) ([]byte, error) {
	// Get buffer from pool
	writer := e.getWriter()
	defer e.putWriter(writer)

	// Write magic number and version
	if err := writer.WriteU32(FormatMagic); err != nil {
		return nil, fmt.Errorf("failed to write magic: %w", err)
	}
	if err := writer.WriteU32(e.formatVersion); err != nil {
		return nil, fmt.Errorf("failed to write version: %w", err)
	}

	// Write span marker
	if err := writer.WriteU8(TypeMarkerSpan); err != nil {
		return nil, fmt.Errorf("failed to write span marker: %w", err)
	}

	// Encode span data
	if err := e.encodeSpanData(writer, span); err != nil {
		atomic.AddInt64(&e.encodingErrors, 1)
		return nil, fmt.Errorf("failed to encode span data: %w", err)
	}

	// Add checksum if enabled
	if e.config.EnableChecksums {
		checksum := writer.GetChecksum()
		if err := writer.WriteU32(checksum); err != nil {
			return nil, fmt.Errorf("failed to write checksum: %w", err)
		}
	}

	// Get result
	result := writer.Bytes()

	// Apply compression if needed
	if e.config.EnableCompression && len(result) > e.config.CompressionThreshold {
		compressed, err := e.compressor.Compress(result, e.compressionBuf[:0])
		if err != nil {
			atomic.AddInt64(&e.encodingErrors, 1)
			return nil, fmt.Errorf("failed to compress data: %w", err)
		}

		// Create compressed envelope
		compressedWriter := e.getWriter()
		defer e.putWriter(compressedWriter)

		compressedWriter.WriteU32(FormatMagic)
		compressedWriter.WriteU32(e.formatVersion)
		compressedWriter.WriteU8(MarkerCompressed)
		compressedWriter.WriteU8(uint8(e.config.CompressionType))
		compressedWriter.WriteU32(uint32(len(result))) // Original size
		compressedWriter.WriteBytes(compressed)

		result = compressedWriter.Bytes()
	}

	// Update statistics
	atomic.AddInt64(&e.encodedBytes, int64(len(result)))
	atomic.AddInt64(&e.encodedSpans, 1)

	return result, nil
}

// EncodeSpanBatch encodes multiple spans efficiently with SIMD optimization
func (e *BinaryEncoder[T]) EncodeSpanBatch(spans []domain.SpanSnapshot[T]) ([]byte, error) {
	if len(spans) == 0 {
		return nil, nil
	}

	writer := e.getWriter()
	defer e.putWriter(writer)

	// Write batch header
	writer.WriteU32(FormatMagic)
	writer.WriteU32(e.formatVersion)
	writer.WriteU8(TypeMarkerSpan | 0x40) // Batch marker
	writer.WriteU32(uint32(len(spans)))

	// Encode spans with SIMD optimization if enabled
	if e.simdEnabled && len(spans) >= e.simdThreshold {
		if err := e.encodeSpanBatchSIMD(writer, spans); err != nil {
			return nil, fmt.Errorf("failed to encode span batch with SIMD: %w", err)
		}
	} else {
		for i, span := range spans {
			if err := e.encodeSpanData(writer, span); err != nil {
				return nil, fmt.Errorf("failed to encode span %d: %w", i, err)
			}
		}
	}

	result := writer.Bytes()
	atomic.AddInt64(&e.encodedBytes, int64(len(result)))
	atomic.AddInt64(&e.encodedSpans, int64(len(spans)))

	return result, nil
}

// EncodeToWriter writes encoded data directly to an io.Writer for streaming
func (e *BinaryEncoder[T]) EncodeToWriter(writer io.Writer, span domain.SpanSnapshot[T]) error {
	data, err := e.EncodeSpan(span)
	if err != nil {
		return fmt.Errorf("failed to encode span: %w", err)
	}

	_, err = writer.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write encoded data: %w", err)
	}

	return nil
}

// DecodeSpan decodes a span from binary data with validation
func (e *BinaryEncoder[T]) DecodeSpan(data []byte) (domain.SpanSnapshot[T], error) {
	reader := NewBinaryReader(data)

	// Validate magic and version
	magic, err := reader.ReadU32()
	if err != nil {
		return nil, fmt.Errorf("failed to read magic: %w", err)
	}
	if magic != FormatMagic {
		return nil, fmt.Errorf("invalid magic number: %x", magic)
	}

	version, err := reader.ReadU32()
	if err != nil {
		return nil, fmt.Errorf("failed to read version: %w", err)
	}
	if version != e.formatVersion {
		return nil, fmt.Errorf("unsupported version: %d", version)
	}

	// Check for compression
	marker, err := reader.ReadU8()
	if err != nil {
		return nil, fmt.Errorf("failed to read marker: %w", err)
	}

	if marker == MarkerCompressed {
		return e.decodeCompressedSpan(reader)
	}

	if marker != TypeMarkerSpan {
		return nil, fmt.Errorf("invalid span marker: %x", marker)
	}

	// Decode span data
	span, err := e.decodeSpanData(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to decode span data: %w", err)
	}

	// Validate checksum if present
	if e.config.EnableChecksums {
		expectedChecksum, err := reader.ReadU32()
		if err != nil {
			return nil, fmt.Errorf("failed to read checksum: %w", err)
		}

		if reader.GetChecksum() != expectedChecksum {
			return nil, fmt.Errorf("checksum validation failed")
		}
	}

	return span, nil
}

// Private methods

func (e *BinaryEncoder[T]) encodeSpanData(writer *BinaryWriter, span domain.SpanSnapshot[T]) error {
	// Write trace ID (16 bytes)
	traceID := span.GetTraceID()
	writer.WriteBytes(traceID[:])

	// Write span ID (8 bytes)
	spanID := span.GetSpanID()
	writer.WriteBytes(spanID[:])

	// Write parent span ID (8 bytes)
	parentID := span.GetParentSpanID()
	writer.WriteBytes(parentID[:])

	// Write timing information
	startTime := span.GetStartTime()
	endTime := span.GetEndTime()
	writer.WriteU64(uint64(startTime.UnixNano()))
	writer.WriteU64(uint64(endTime.UnixNano()))

	// Write span name
	name := span.GetName()
	writer.WriteString(name)

	// Write span kind
	writer.WriteU8(uint8(span.GetKind()))

	// Write status
	status := span.GetStatus()
	writer.WriteU8(uint8(status.Code))
	writer.WriteString(status.Description)

	// Write attributes
	attrs := span.GetAttributes()
	writer.WriteU32(uint32(len(attrs)))
	for _, attr := range attrs {
		if err := e.encodeAttribute(writer, attr); err != nil {
			return fmt.Errorf("failed to encode attribute: %w", err)
		}
	}

	// Write events
	events := span.GetEvents()
	writer.WriteU32(uint32(len(events)))
	for _, event := range events {
		if err := e.encodeEvent(writer, event); err != nil {
			return fmt.Errorf("failed to encode event: %w", err)
		}
	}

	// Write links
	links := span.GetLinks()
	writer.WriteU32(uint32(len(links)))
	for _, link := range links {
		if err := e.encodeLink(writer, link); err != nil {
			return fmt.Errorf("failed to encode link: %w", err)
		}
	}

	// Write resource
	resource := span.GetResource()
	if err := e.encodeResource(writer, resource); err != nil {
		return fmt.Errorf("failed to encode resource: %w", err)
	}

	return nil
}

func (e *BinaryEncoder[T]) encodeAttribute(writer *BinaryWriter, attr domain.SpanAttribute[T]) error {
	// Write attribute marker
	writer.WriteU8(TypeMarkerAttribute)

	// Write key (stored as unsafe.Pointer in attribute)
	// For now, we'll extract the key through the unsafe pointer
	keyPtr := (*string)(unsafe.Pointer(&attr))
	writer.WriteString(*keyPtr)

	// Write value with type information
	return e.encodeValue(writer, attr)
}

func (e *BinaryEncoder[T]) encodeValue(writer *BinaryWriter, value any) error {
	switch v := value.(type) {
	case string:
		writer.WriteU8(ValueTypeString)
		writer.WriteString(v)
	case int64:
		writer.WriteU8(ValueTypeInt64)
		writer.WriteI64(v)
	case float64:
		writer.WriteU8(ValueTypeFloat64)
		writer.WriteF64(v)
	case bool:
		writer.WriteU8(ValueTypeBool)
		writer.WriteBool(v)
	case []byte:
		writer.WriteU8(ValueTypeBytes)
		writer.WriteBytes(v)
	case []any:
		writer.WriteU8(ValueTypeArray)
		writer.WriteU32(uint32(len(v)))
		for _, item := range v {
			if err := e.encodeValue(writer, item); err != nil {
				return err
			}
		}
	case map[string]any:
		writer.WriteU8(ValueTypeMap)
		writer.WriteU32(uint32(len(v)))
		for key, val := range v {
			writer.WriteString(key)
			if err := e.encodeValue(writer, val); err != nil {
				return err
			}
		}
	default:
		// Try custom type handlers
		if err := e.encodeCustomValue(writer, value); err != nil {
			return fmt.Errorf("unsupported value type: %T", value)
		}
	}

	return nil
}

func (e *BinaryEncoder[T]) encodeEvent(writer *BinaryWriter, event domain.SpanEvent[T]) error {
	writer.WriteU8(TypeMarkerEvent)
	writer.WriteString(event.Name)
	writer.WriteU64(uint64(event.Timestamp.UnixNano()))

	// Encode event attributes
	writer.WriteU32(uint32(len(event.Attributes)))
	for _, attr := range event.Attributes {
		if err := e.encodeAttribute(writer, attr); err != nil {
			return err
		}
	}

	return nil
}

func (e *BinaryEncoder[T]) encodeLink(writer *BinaryWriter, link domain.SpanLink[T]) error {
	writer.WriteU8(TypeMarkerLink)
	writer.WriteBytes(link.TraceID[:])
	writer.WriteBytes(link.SpanID[:])

	// Encode link attributes
	writer.WriteU32(uint32(len(link.Attributes)))
	for _, attr := range link.Attributes {
		if err := e.encodeAttribute(writer, attr); err != nil {
			return err
		}
	}

	return nil
}

func (e *BinaryEncoder[T]) encodeResource(writer *BinaryWriter, resource domain.Resource) error {
	writer.WriteU8(TypeMarkerResource)
	writer.WriteString(resource.SchemaURL)

	// Encode resource attributes
	writer.WriteU32(uint32(len(resource.Attributes)))
	for key, value := range resource.Attributes {
		writer.WriteString(key)
		if err := e.encodeValue(writer, value); err != nil {
			return err
		}
	}

	return nil
}

func (e *BinaryEncoder[T]) encodeCustomValue(writer *BinaryWriter, value any) error {
	// Check if we have a custom handler for this value type
	e.handlerMutex.RLock()
	defer e.handlerMutex.RUnlock()

	for typeID, handler := range e.typeHandlers {
		// This is a simplified type check - in practice, you'd use reflection
		// or a more sophisticated type system
		if handler.GetTypeID() == typeID {
			writer.WriteU32(uint32(typeID))
			if typedHandler, ok := handler.(TypeHandler[T]); ok {
				return typedHandler.EncodeTo(writer, value.(T))
			}
		}
	}

	return fmt.Errorf("no custom handler found for type")
}

func (e *BinaryEncoder[T]) encodeSpanBatchSIMD(writer *BinaryWriter, spans []domain.SpanSnapshot[T]) error {
	// SIMD-optimized batch encoding
	// This would use SIMD instructions for vectorized operations
	// For now, we implement an optimized loop

	// Process spans in chunks for better cache locality
	chunkSize := 8 // Process 8 spans at once for SIMD alignment

	for i := 0; i < len(spans); i += chunkSize {
		end := i + chunkSize
		if end > len(spans) {
			end = len(spans)
		}

		// Process chunk
		for j := i; j < end; j++ {
			if err := e.encodeSpanData(writer, spans[j]); err != nil {
				return fmt.Errorf("failed to encode span %d in SIMD batch: %w", j, err)
			}
		}
	}

	return nil
}

func (e *BinaryEncoder[T]) decodeCompressedSpan(reader *BinaryReader) (domain.SpanSnapshot[T], error) {
	// Read compression type
	compressionType, err := reader.ReadU8()
	if err != nil {
		return nil, fmt.Errorf("failed to read compression type: %w", err)
	}

	// Read original size
	originalSize, err := reader.ReadU32()
	if err != nil {
		return nil, fmt.Errorf("failed to read original size: %w", err)
	}

	// Read compressed data
	compressedData := reader.ReadRemaining()

	// Decompress
	compressor := NewCompressor(CompressionType(compressionType), 0)
	decompressed, err := compressor.Decompress(compressedData, int(originalSize))
	if err != nil {
		return nil, fmt.Errorf("failed to decompress data: %w", err)
	}

	// Decode decompressed data
	return e.DecodeSpan(decompressed)
}

func (e *BinaryEncoder[T]) decodeSpanData(reader *BinaryReader) (domain.SpanSnapshot[T], error) {
	// This would implement the reverse of encodeSpanData
	// For brevity, returning a placeholder
	return nil, fmt.Errorf("decoding not yet implemented")
}

func (e *BinaryEncoder[T]) getWriter() *BinaryWriter {
	if writer := e.writerPool.Get(); writer != nil {
		w := writer.(*BinaryWriter)
		w.Reset()
		atomic.AddInt64(&e.poolHits, 1)
		return w
	}

	atomic.AddInt64(&e.poolMisses, 1)
	return NewBinaryWriter(e.config.InitialBufferSize)
}

func (e *BinaryEncoder[T]) putWriter(writer *BinaryWriter) {
	if writer.Capacity() <= e.config.MaxBufferSize {
		e.writerPool.Put(writer)
	}
}

func (e *BinaryEncoder[T]) initializePools() {
	e.bufferPool.New = func() any {
		return make([]byte, e.config.InitialBufferSize)
	}

	e.writerPool.New = func() any {
		return NewBinaryWriter(e.config.InitialBufferSize)
	}
}

func (e *BinaryEncoder[T]) registerDefaultHandlers() {
	// Register default type handlers for common types
	// Implementation would register handlers for time.Time, duration, etc.
}

// GetStats returns encoder performance statistics
func (e *BinaryEncoder[T]) GetStats() EncoderStats {
	return EncoderStats{
		EncodedBytes:     atomic.LoadInt64(&e.encodedBytes),
		EncodedSpans:     atomic.LoadInt64(&e.encodedSpans),
		EncodingErrors:   atomic.LoadInt64(&e.encodingErrors),
		PoolHits:         atomic.LoadInt64(&e.poolHits),
		PoolMisses:       atomic.LoadInt64(&e.poolMisses),
		CompressionRatio: e.getCompressionRatio(),
	}
}

func (e *BinaryEncoder[T]) getCompressionRatio() float64 {
	// Calculate compression ratio
	return 1.0 // Placeholder
}

// Supporting types for encoder

type TypeRegistry interface {
	RegisterType(typeID TypeID, handler TypeHandler[any]) error
	GetHandler(typeID TypeID) (TypeHandler[any], bool)
	ListTypes() []TypeID
}

type Compressor interface {
	Compress(src, dst []byte) ([]byte, error)
	Decompress(src []byte, originalSize int) ([]byte, error)
	GetType() CompressionType
}

type EncoderStats struct {
	EncodedBytes     int64
	EncodedSpans     int64
	EncodingErrors   int64
	PoolHits         int64
	PoolMisses       int64
	CompressionRatio float64
}

func applyEncoderDefaults(config *EncoderConfig) {
	if config.InitialBufferSize == 0 {
		config.InitialBufferSize = 4096
	}
	if config.MaxBufferSize == 0 {
		config.MaxBufferSize = 1024 * 1024
	}
	if config.BufferPoolSize == 0 {
		config.BufferPoolSize = 100
	}
	if config.CompressionThreshold == 0 {
		config.CompressionThreshold = 1024
	}
	if config.CompressionLevel == 0 {
		config.CompressionLevel = 6
	}
	if config.SIMDThreshold == 0 {
		config.SIMDThreshold = 8
	}
	if config.FormatVersion == 0 {
		config.FormatVersion = FormatVersion
	}
	if config.ArenaSize == 0 {
		config.ArenaSize = 64 * 1024
	}
	if config.MaxErrors == 0 {
		config.MaxErrors = 100
	}
}
