package encoding

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"sync"

	"github.com/golang/snappy"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
)

// Compressor interface defines compression operations for binary encoding
type Compressor interface {
	Compress(src, dst []byte) ([]byte, error)
	Decompress(src []byte, originalSize int) ([]byte, error)
	GetType() CompressionType
	GetLevel() int
	EstimateCompressedSize(originalSize int) int
	GetStats() CompressionStats
	Reset()
}

// CompressionStats tracks compression performance
type CompressionStats struct {
	TotalCompressed   int64   // Total bytes compressed
	TotalDecompressed int64   // Total bytes decompressed
	CompressionRatio  float64 // Average compression ratio
	CompressionTime   int64   // Total compression time in nanoseconds
	DecompressionTime int64   // Total decompression time in nanoseconds
	CompressionOps    int64   // Number of compression operations
	DecompressionOps  int64   // Number of decompression operations
	Errors            int64   // Number of errors encountered
}

// GzipCompressor implements gzip compression
type GzipCompressor struct {
	level int
	pool  *sync.Pool
	stats CompressionStats
	mutex sync.RWMutex
}

// ZstdCompressor implements Zstandard compression
type ZstdCompressor struct {
	level   int
	encoder *zstd.Encoder
	decoder *zstd.Decoder
	stats   CompressionStats
	mutex   sync.RWMutex
}

// LZ4Compressor implements LZ4 compression
type LZ4Compressor struct {
	level int
	stats CompressionStats
	mutex sync.RWMutex
}

// SnappyCompressor implements Snappy compression
type SnappyCompressor struct {
	stats CompressionStats
	mutex sync.RWMutex
}

// NoOpCompressor provides a pass-through compressor
type NoOpCompressor struct {
	stats CompressionStats
}

// NewCompressor creates a new compressor based on type and level
func NewCompressor(compressionType CompressionType, level int) Compressor {
	switch compressionType {
	case CompressionTypeGzip:
		return NewGzipCompressor(level)
	case CompressionTypeZstd:
		return NewZstdCompressor(level)
	case CompressionTypeLZ4:
		return NewLZ4Compressor(level)
	case CompressionTypeSnappy:
		return NewSnappyCompressor()
	case CompressionTypeNone:
		return NewNoOpCompressor()
	default:
		return NewNoOpCompressor()
	}
}

// GzipCompressor implementation

func NewGzipCompressor(level int) *GzipCompressor {
	if level < gzip.DefaultCompression || level > gzip.BestCompression {
		level = gzip.DefaultCompression
	}

	return &GzipCompressor{
		level: level,
		pool: &sync.Pool{
			New: func() interface{} {
				var buf bytes.Buffer
				writer, _ := gzip.NewWriterLevel(&buf, level)
				return &gzipPoolItem{
					writer: writer,
					buffer: &buf,
				}
			},
		},
	}
}

type gzipPoolItem struct {
	writer *gzip.Writer
	buffer *bytes.Buffer
}

func (g *GzipCompressor) Compress(src, dst []byte) ([]byte, error) {
	startTime := getCurrentNanoTime()

	// Get writer from pool
	item := g.pool.Get().(*gzipPoolItem)
	defer g.pool.Put(item)

	// Reset for reuse
	item.buffer.Reset()
	item.writer.Reset(item.buffer)

	// Compress data
	if _, err := item.writer.Write(src); err != nil {
		g.mutex.Lock()
		g.stats.Errors++
		g.mutex.Unlock()
		return nil, fmt.Errorf("failed to write to gzip compressor: %w", err)
	}

	if err := item.writer.Close(); err != nil {
		g.mutex.Lock()
		g.stats.Errors++
		g.mutex.Unlock()
		return nil, fmt.Errorf("failed to close gzip compressor: %w", err)
	}

	// Get compressed data
	result := append(dst[:0], item.buffer.Bytes()...)

	// Update statistics
	g.mutex.Lock()
	g.stats.TotalCompressed += int64(len(src))
	g.stats.CompressionTime += getCurrentNanoTime() - startTime
	g.stats.CompressionOps++
	g.updateCompressionRatio(len(src), len(result))
	g.mutex.Unlock()

	return result, nil
}

func (g *GzipCompressor) Decompress(src []byte, originalSize int) ([]byte, error) {
	startTime := getCurrentNanoTime()

	reader, err := gzip.NewReader(bytes.NewReader(src))
	if err != nil {
		g.mutex.Lock()
		g.stats.Errors++
		g.mutex.Unlock()
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer reader.Close()

	// Pre-allocate buffer if original size is known
	var result []byte
	if originalSize > 0 {
		result = make([]byte, 0, originalSize)
	}

	// Read decompressed data
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			result = append(result, buf[:n]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			g.mutex.Lock()
			g.stats.Errors++
			g.mutex.Unlock()
			return nil, fmt.Errorf("failed to read from gzip reader: %w", err)
		}
	}

	// Update statistics
	g.mutex.Lock()
	g.stats.TotalDecompressed += int64(len(result))
	g.stats.DecompressionTime += getCurrentNanoTime() - startTime
	g.stats.DecompressionOps++
	g.mutex.Unlock()

	return result, nil
}

func (g *GzipCompressor) GetType() CompressionType {
	return CompressionTypeGzip
}

func (g *GzipCompressor) GetLevel() int {
	return g.level
}

func (g *GzipCompressor) EstimateCompressedSize(originalSize int) int {
	// Rough estimate for gzip compression
	return originalSize/2 + 1024 // Assume 50% compression + overhead
}

func (g *GzipCompressor) GetStats() CompressionStats {
	g.mutex.RLock()
	defer g.mutex.RUnlock()
	return g.stats
}

func (g *GzipCompressor) Reset() {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.stats = CompressionStats{}
}

func (g *GzipCompressor) updateCompressionRatio(original, compressed int) {
	if g.stats.CompressionOps == 0 {
		g.stats.CompressionRatio = float64(compressed) / float64(original)
	} else {
		// Running average
		ratio := float64(compressed) / float64(original)
		g.stats.CompressionRatio = (g.stats.CompressionRatio*float64(g.stats.CompressionOps-1) + ratio) / float64(g.stats.CompressionOps)
	}
}

// ZstdCompressor implementation

func NewZstdCompressor(level int) *ZstdCompressor {
	encoder, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)))
	decoder, _ := zstd.NewReader(nil)

	return &ZstdCompressor{
		level:   level,
		encoder: encoder,
		decoder: decoder,
	}
}

func (z *ZstdCompressor) Compress(src, dst []byte) ([]byte, error) {
	startTime := getCurrentNanoTime()

	result := z.encoder.EncodeAll(src, dst[:0])

	z.mutex.Lock()
	z.stats.TotalCompressed += int64(len(src))
	z.stats.CompressionTime += getCurrentNanoTime() - startTime
	z.stats.CompressionOps++
	z.updateCompressionRatio(len(src), len(result))
	z.mutex.Unlock()

	return result, nil
}

func (z *ZstdCompressor) Decompress(src []byte, originalSize int) ([]byte, error) {
	startTime := getCurrentNanoTime()

	var dst []byte
	if originalSize > 0 {
		dst = make([]byte, 0, originalSize)
	}

	result, err := z.decoder.DecodeAll(src, dst)
	if err != nil {
		z.mutex.Lock()
		z.stats.Errors++
		z.mutex.Unlock()
		return nil, fmt.Errorf("failed to decompress with zstd: %w", err)
	}

	z.mutex.Lock()
	z.stats.TotalDecompressed += int64(len(result))
	z.stats.DecompressionTime += getCurrentNanoTime() - startTime
	z.stats.DecompressionOps++
	z.mutex.Unlock()

	return result, nil
}

func (z *ZstdCompressor) GetType() CompressionType {
	return CompressionTypeZstd
}

func (z *ZstdCompressor) GetLevel() int {
	return z.level
}

func (z *ZstdCompressor) EstimateCompressedSize(originalSize int) int {
	// Zstd typically achieves better compression than gzip
	return originalSize/3 + 512 // Assume 66% compression + overhead
}

func (z *ZstdCompressor) GetStats() CompressionStats {
	z.mutex.RLock()
	defer z.mutex.RUnlock()
	return z.stats
}

func (z *ZstdCompressor) Reset() {
	z.mutex.Lock()
	defer z.mutex.Unlock()
	z.stats = CompressionStats{}
}

func (z *ZstdCompressor) updateCompressionRatio(original, compressed int) {
	if z.stats.CompressionOps == 0 {
		z.stats.CompressionRatio = float64(compressed) / float64(original)
	} else {
		ratio := float64(compressed) / float64(original)
		z.stats.CompressionRatio = (z.stats.CompressionRatio*float64(z.stats.CompressionOps-1) + ratio) / float64(z.stats.CompressionOps)
	}
}

// LZ4Compressor implementation

func NewLZ4Compressor(level int) *LZ4Compressor {
	return &LZ4Compressor{
		level: level,
	}
}

func (l *LZ4Compressor) Compress(src, dst []byte) ([]byte, error) {
	startTime := getCurrentNanoTime()

	// Ensure dst has enough capacity
	maxSize := lz4.CompressBlockBound(len(src))
	if cap(dst) < maxSize {
		dst = make([]byte, 0, maxSize)
	}

	compressedSize, err := lz4.CompressBlock(src, dst[:maxSize], nil)
	if err != nil {
		l.mutex.Lock()
		l.stats.Errors++
		l.mutex.Unlock()
		return nil, fmt.Errorf("failed to compress with LZ4: %w", err)
	}

	result := dst[:compressedSize]

	l.mutex.Lock()
	l.stats.TotalCompressed += int64(len(src))
	l.stats.CompressionTime += getCurrentNanoTime() - startTime
	l.stats.CompressionOps++
	l.updateCompressionRatio(len(src), len(result))
	l.mutex.Unlock()

	return result, nil
}

func (l *LZ4Compressor) Decompress(src []byte, originalSize int) ([]byte, error) {
	startTime := getCurrentNanoTime()

	dst := make([]byte, originalSize)

	decompressedSize, err := lz4.UncompressBlock(src, dst)
	if err != nil {
		l.mutex.Lock()
		l.stats.Errors++
		l.mutex.Unlock()
		return nil, fmt.Errorf("failed to decompress with LZ4: %w", err)
	}

	result := dst[:decompressedSize]

	l.mutex.Lock()
	l.stats.TotalDecompressed += int64(len(result))
	l.stats.DecompressionTime += getCurrentNanoTime() - startTime
	l.stats.DecompressionOps++
	l.mutex.Unlock()

	return result, nil
}

func (l *LZ4Compressor) GetType() CompressionType {
	return CompressionTypeLZ4
}

func (l *LZ4Compressor) GetLevel() int {
	return l.level
}

func (l *LZ4Compressor) EstimateCompressedSize(originalSize int) int {
	// LZ4 prioritizes speed over compression ratio
	return originalSize*2/3 + 256 // Assume 33% compression + overhead
}

func (l *LZ4Compressor) GetStats() CompressionStats {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	return l.stats
}

func (l *LZ4Compressor) Reset() {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.stats = CompressionStats{}
}

func (l *LZ4Compressor) updateCompressionRatio(original, compressed int) {
	if l.stats.CompressionOps == 0 {
		l.stats.CompressionRatio = float64(compressed) / float64(original)
	} else {
		ratio := float64(compressed) / float64(original)
		l.stats.CompressionRatio = (l.stats.CompressionRatio*float64(l.stats.CompressionOps-1) + ratio) / float64(l.stats.CompressionOps)
	}
}

// SnappyCompressor implementation

func NewSnappyCompressor() *SnappyCompressor {
	return &SnappyCompressor{}
}

func (s *SnappyCompressor) Compress(src, dst []byte) ([]byte, error) {
	startTime := getCurrentNanoTime()

	result := snappy.Encode(dst[:0], src)

	s.mutex.Lock()
	s.stats.TotalCompressed += int64(len(src))
	s.stats.CompressionTime += getCurrentNanoTime() - startTime
	s.stats.CompressionOps++
	s.updateCompressionRatio(len(src), len(result))
	s.mutex.Unlock()

	return result, nil
}

func (s *SnappyCompressor) Decompress(src []byte, originalSize int) ([]byte, error) {
	startTime := getCurrentNanoTime()

	var dst []byte
	if originalSize > 0 {
		dst = make([]byte, 0, originalSize)
	}

	result, err := snappy.Decode(dst, src)
	if err != nil {
		s.mutex.Lock()
		s.stats.Errors++
		s.mutex.Unlock()
		return nil, fmt.Errorf("failed to decompress with snappy: %w", err)
	}

	s.mutex.Lock()
	s.stats.TotalDecompressed += int64(len(result))
	s.stats.DecompressionTime += getCurrentNanoTime() - startTime
	s.stats.DecompressionOps++
	s.mutex.Unlock()

	return result, nil
}

func (s *SnappyCompressor) GetType() CompressionType {
	return CompressionTypeSnappy
}

func (s *SnappyCompressor) GetLevel() int {
	return 0 // Snappy doesn't have compression levels
}

func (s *SnappyCompressor) EstimateCompressedSize(originalSize int) int {
	// Snappy prioritizes speed, similar compression to LZ4
	return originalSize*2/3 + 128 // Assume 33% compression + overhead
}

func (s *SnappyCompressor) GetStats() CompressionStats {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.stats
}

func (s *SnappyCompressor) Reset() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.stats = CompressionStats{}
}

func (s *SnappyCompressor) updateCompressionRatio(original, compressed int) {
	if s.stats.CompressionOps == 0 {
		s.stats.CompressionRatio = float64(compressed) / float64(original)
	} else {
		ratio := float64(compressed) / float64(original)
		s.stats.CompressionRatio = (s.stats.CompressionRatio*float64(s.stats.CompressionOps-1) + ratio) / float64(s.stats.CompressionOps)
	}
}

// NoOpCompressor implementation

func NewNoOpCompressor() *NoOpCompressor {
	return &NoOpCompressor{}
}

func (n *NoOpCompressor) Compress(src, dst []byte) ([]byte, error) {
	result := append(dst[:0], src...)
	n.stats.TotalCompressed += int64(len(src))
	n.stats.CompressionOps++
	n.stats.CompressionRatio = 1.0 // No compression
	return result, nil
}

func (n *NoOpCompressor) Decompress(src []byte, originalSize int) ([]byte, error) {
	result := make([]byte, len(src))
	copy(result, src)
	n.stats.TotalDecompressed += int64(len(result))
	n.stats.DecompressionOps++
	return result, nil
}

func (n *NoOpCompressor) GetType() CompressionType {
	return CompressionTypeNone
}

func (n *NoOpCompressor) GetLevel() int {
	return 0
}

func (n *NoOpCompressor) EstimateCompressedSize(originalSize int) int {
	return originalSize // No compression
}

func (n *NoOpCompressor) GetStats() CompressionStats {
	return n.stats
}

func (n *NoOpCompressor) Reset() {
	n.stats = CompressionStats{}
}

// Utility functions

func getCurrentNanoTime() int64 {
	// In a real implementation, this would use a high-resolution timer
	// For now, we'll use a placeholder
	return 0
}

// CompressorPool manages a pool of compressors for concurrent use
type CompressorPool struct {
	compressorType CompressionType
	level          int
	pool           sync.Pool
}

// NewCompressorPool creates a new pool of compressors
func NewCompressorPool(compressionType CompressionType, level int) *CompressorPool {
	return &CompressorPool{
		compressorType: compressionType,
		level:          level,
		pool: sync.Pool{
			New: func() interface{} {
				return NewCompressor(compressionType, level)
			},
		},
	}
}

// Get retrieves a compressor from the pool
func (cp *CompressorPool) Get() Compressor {
	return cp.pool.Get().(Compressor)
}

// Put returns a compressor to the pool
func (cp *CompressorPool) Put(compressor Compressor) {
	compressor.Reset()
	cp.pool.Put(compressor)
}

// GetPoolStats returns statistics for all compressors in the pool
func (cp *CompressorPool) GetPoolStats() CompressionStats {
	// This would aggregate stats from all compressors
	// For now, return empty stats
	return CompressionStats{}
}

// AdaptiveCompressor automatically selects the best compression algorithm
type AdaptiveCompressor struct {
	compressors []Compressor
	mutex       sync.RWMutex
	stats       map[CompressionType]CompressionStats
}

// NewAdaptiveCompressor creates a compressor that adapts based on data characteristics
func NewAdaptiveCompressor() *AdaptiveCompressor {
	return &AdaptiveCompressor{
		compressors: []Compressor{
			NewGzipCompressor(6),
			NewZstdCompressor(3),
			NewLZ4Compressor(1),
			NewSnappyCompressor(),
		},
		stats: make(map[CompressionType]CompressionStats),
	}
}

// Compress automatically selects the best compressor for the data
func (ac *AdaptiveCompressor) Compress(src, dst []byte) ([]byte, error) {
	// Simple heuristic: use snappy for small data, zstd for large data
	if len(src) < 1024 {
		return ac.compressors[3].Compress(src, dst) // Snappy
	}
	return ac.compressors[1].Compress(src, dst) // Zstd
}

// Decompress decompresses data (requires compression type metadata)
func (ac *AdaptiveCompressor) Decompress(src []byte, originalSize int) ([]byte, error) {
	// In practice, this would read the compression type from metadata
	// For now, try each compressor until one succeeds
	for _, compressor := range ac.compressors {
		result, err := compressor.Decompress(src, originalSize)
		if err == nil {
			return result, nil
		}
	}
	return nil, fmt.Errorf("failed to decompress with any available compressor")
}

func (ac *AdaptiveCompressor) GetType() CompressionType {
	return CompressionTypeNone // Adaptive type
}

func (ac *AdaptiveCompressor) GetLevel() int {
	return 0 // Adaptive level
}

func (ac *AdaptiveCompressor) EstimateCompressedSize(originalSize int) int {
	// Conservative estimate
	return originalSize / 2
}

func (ac *AdaptiveCompressor) GetStats() CompressionStats {
	// Aggregate stats from all compressors
	var totalStats CompressionStats
	for _, compressor := range ac.compressors {
		stats := compressor.GetStats()
		totalStats.TotalCompressed += stats.TotalCompressed
		totalStats.TotalDecompressed += stats.TotalDecompressed
		totalStats.CompressionOps += stats.CompressionOps
		totalStats.DecompressionOps += stats.DecompressionOps
		totalStats.Errors += stats.Errors
	}

	if totalStats.CompressionOps > 0 {
		totalStats.CompressionRatio /= float64(len(ac.compressors))
	}

	return totalStats
}

func (ac *AdaptiveCompressor) Reset() {
	for _, compressor := range ac.compressors {
		compressor.Reset()
	}
}
