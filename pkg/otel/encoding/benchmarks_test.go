package encoding

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/yair/tapio/pkg/otel/domain"
)

// Benchmark data generators

func generateTestSpan() domain.SpanSnapshot[string] {
	return &mockSpanSnapshot{
		traceID:   generateTraceID(),
		spanID:    generateSpanID(),
		parentID:  generateSpanID(),
		name:      "test-operation",
		kind:      domain.SpanKindServer,
		startTime: time.Now().Add(-time.Second),
		endTime:   time.Now(),
		attrs:     generateAttributes(10),
		events:    generateEvents(5),
		links:     generateLinks(3),
	}
}

func generateLargeSpan() domain.SpanSnapshot[string] {
	return &mockSpanSnapshot{
		traceID:   generateTraceID(),
		spanID:    generateSpanID(),
		parentID:  generateSpanID(),
		name:      "large-operation-with-extensive-metadata",
		kind:      domain.SpanKindServer,
		startTime: time.Now().Add(-time.Minute),
		endTime:   time.Now(),
		attrs:     generateAttributes(100),
		events:    generateEvents(50),
		links:     generateLinks(20),
	}
}

func generateSpanBatch(count int) []domain.SpanSnapshot[string] {
	spans := make([]domain.SpanSnapshot[string], count)
	for i := 0; i < count; i++ {
		spans[i] = generateTestSpan()
	}
	return spans
}

func generateRandomData(size int) []byte {
	data := make([]byte, size)
	rand.Read(data)
	return data
}

// Binary encoding benchmarks

func BenchmarkBinaryEncoder_EncodeSpan(b *testing.B) {
	config := EncoderConfig{
		InitialBufferSize: 4096,
		MaxBufferSize:     1024 * 1024,
		EnableCompression: false,
		EnableSIMD:        true,
	}
	encoder := NewBinaryEncoder[string](config)
	span := generateTestSpan()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_, err := encoder.EncodeSpan(span)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBinaryEncoder_EncodeSpanLarge(b *testing.B) {
	config := EncoderConfig{
		InitialBufferSize: 8192,
		MaxBufferSize:     1024 * 1024,
		EnableCompression: false,
		EnableSIMD:        true,
	}
	encoder := NewBinaryEncoder[string](config)
	span := generateLargeSpan()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_, err := encoder.EncodeSpan(span)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBinaryEncoder_EncodeSpanBatch(b *testing.B) {
	config := EncoderConfig{
		InitialBufferSize: 16384,
		MaxBufferSize:     2 * 1024 * 1024,
		EnableCompression: false,
		EnableSIMD:        true,
	}
	encoder := NewBinaryEncoder[string](config)
	
	batchSizes := []int{1, 10, 100, 1000}
	
	for _, size := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize%d", size), func(b *testing.B) {
			spans := generateSpanBatch(size)
			
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				_, err := encoder.EncodeSpanBatch(spans)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkBinaryEncoder_WithCompression(b *testing.B) {
	compressionTypes := []CompressionType{
		CompressionTypeGzip,
		CompressionTypeZstd,
		CompressionTypeLZ4,
		CompressionTypeSnappy,
	}
	
	for _, compType := range compressionTypes {
		b.Run(compType.String(), func(b *testing.B) {
			config := EncoderConfig{
				InitialBufferSize:    4096,
				MaxBufferSize:        1024 * 1024,
				EnableCompression:    true,
				CompressionType:      compType,
				CompressionLevel:     6,
				CompressionThreshold: 1024,
			}
			encoder := NewBinaryEncoder[string](config)
			span := generateLargeSpan()
			
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				_, err := encoder.EncodeSpan(span)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// Binary writer benchmarks

func BenchmarkBinaryWriter_WriteOperations(b *testing.B) {
	writer := NewBinaryWriter(4096)
	
	b.Run("WriteU8", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			writer.Reset()
			for j := 0; j < 1000; j++ {
				writer.WriteU8(uint8(j))
			}
		}
	})
	
	b.Run("WriteU32", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			writer.Reset()
			for j := 0; j < 1000; j++ {
				writer.WriteU32(uint32(j))
			}
		}
	})
	
	b.Run("WriteU64", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			writer.Reset()
			for j := 0; j < 1000; j++ {
				writer.WriteU64(uint64(j))
			}
		}
	})
	
	b.Run("WriteString", func(b *testing.B) {
		testString := "test-string-for-benchmarking"
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			writer.Reset()
			for j := 0; j < 1000; j++ {
				writer.WriteString(testString)
			}
		}
	})
	
	b.Run("WriteBytes", func(b *testing.B) {
		testData := generateRandomData(32)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			writer.Reset()
			for j := 0; j < 1000; j++ {
				writer.WriteBytes(testData)
			}
		}
	})
}

func BenchmarkBinaryWriter_VarInt(b *testing.B) {
	writer := NewBinaryWriter(4096)
	
	values := []uint64{
		0, 127, 128, 16383, 16384, 2097151, 2097152, 268435455, 268435456,
	}
	
	for _, value := range values {
		b.Run(fmt.Sprintf("Value%d", value), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				writer.Reset()
				writer.WriteVarInt(value)
			}
		})
	}
}

func BenchmarkBinaryWriter_BufferGrowth(b *testing.B) {
	initialSizes := []int{512, 1024, 4096, 8192}
	
	for _, size := range initialSizes {
		b.Run(fmt.Sprintf("InitialSize%d", size), func(b *testing.B) {
			testData := generateRandomData(1024 * 1024) // 1MB test data
			
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				writer := NewBinaryWriter(size)
				writer.WriteBytes(testData)
			}
		})
	}
}

// Binary reader benchmarks

func BenchmarkBinaryReader_ReadOperations(b *testing.B) {
	// Generate test data
	writer := NewBinaryWriter(4096)
	for i := 0; i < 1000; i++ {
		writer.WriteU8(uint8(i))
		writer.WriteU32(uint32(i))
		writer.WriteU64(uint64(i))
		writer.WriteString(fmt.Sprintf("string-%d", i))
		writer.WriteBytes(generateRandomData(32))
	}
	testData := writer.Bytes()
	
	b.Run("ReadU8", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			reader := NewBinaryReader(testData)
			for j := 0; j < 1000; j++ {
				reader.ReadU8()
			}
		}
	})
	
	b.Run("ReadU32", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			reader := NewBinaryReader(testData)
			for j := 0; j < 1000; j++ {
				reader.ReadU32()
			}
		}
	})
	
	b.Run("ReadString", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			reader := NewBinaryReader(testData)
			for reader.Available() {
				reader.ReadString()
			}
		}
	})
}

func BenchmarkBinaryReader_BatchReading(b *testing.B) {
	// Generate batch test data
	writer := NewBinaryWriter(8192)
	valueSize := 16
	count := 1000
	
	for i := 0; i < count; i++ {
		writer.WriteBytes(generateRandomData(valueSize))
	}
	testData := writer.Bytes()
	
	b.Run("Sequential", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			reader := NewBinaryReader(testData)
			for j := 0; j < count; j++ {
				reader.ReadBytes()
			}
		}
	})
	
	b.Run("BatchSIMD", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			reader := NewBinaryReader(testData)
			reader.ReadBatch(count, valueSize+4) // +4 for length prefix
		}
	})
}

// Compression benchmarks

func BenchmarkCompression_Algorithms(b *testing.B) {
	dataSizes := []int{1024, 4096, 16384, 65536, 262144} // 1KB to 256KB
	compressionTypes := []CompressionType{
		CompressionTypeGzip,
		CompressionTypeZstd,
		CompressionTypeLZ4,
		CompressionTypeSnappy,
	}
	
	for _, size := range dataSizes {
		testData := generateRandomData(size)
		
		for _, compType := range compressionTypes {
			b.Run(fmt.Sprintf("%s_Size%d", compType.String(), size), func(b *testing.B) {
				compressor := NewCompressor(compType, 6)
				dst := make([]byte, 0, size*2)
				
				b.ResetTimer()
				b.ReportAllocs()
				
				for i := 0; i < b.N; i++ {
					_, err := compressor.Compress(testData, dst)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkCompression_Roundtrip(b *testing.B) {
	testData := generateRandomData(16384) // 16KB
	compressionTypes := []CompressionType{
		CompressionTypeGzip,
		CompressionTypeZstd,
		CompressionTypeLZ4,
		CompressionTypeSnappy,
	}
	
	for _, compType := range compressionTypes {
		b.Run(compType.String(), func(b *testing.B) {
			compressor := NewCompressor(compType, 6)
			dst := make([]byte, 0, len(testData)*2)
			
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				compressed, err := compressor.Compress(testData, dst)
				if err != nil {
					b.Fatal(err)
				}
				
				_, err = compressor.Decompress(compressed, len(testData))
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// Memory efficiency benchmarks

func BenchmarkMemoryEfficiency_ZeroAllocation(b *testing.B) {
	config := EncoderConfig{
		InitialBufferSize: 8192,
		MaxBufferSize:     1024 * 1024,
		EnableZeroCopy:    true,
		EnableCompression: false,
	}
	encoder := NewBinaryEncoder[string](config)
	span := generateTestSpan()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_, err := encoder.EncodeSpan(span)
		if err != nil {
			b.Fatal(err)
		}
	}
	
	// Verify zero allocations in hot path
	stats := encoder.GetStats()
	if stats.PoolMisses > stats.PoolHits {
		b.Errorf("Too many pool misses: %d misses vs %d hits", stats.PoolMisses, stats.PoolHits)
	}
}

func BenchmarkMemoryEfficiency_BufferReuse(b *testing.B) {
	writer := NewBinaryWriter(4096)
	testData := generateRandomData(1024)
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		writer.Reset()
		writer.WriteBytes(testData)
	}
}

// Concurrent performance benchmarks

func BenchmarkConcurrentEncoding(b *testing.B) {
	config := EncoderConfig{
		InitialBufferSize: 4096,
		MaxBufferSize:     1024 * 1024,
		EnableCompression: false,
	}
	encoder := NewBinaryEncoder[string](config)
	span := generateTestSpan()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := encoder.EncodeSpan(span)
			if err != nil {
				b.Error(err)
			}
		}
	})
}

func BenchmarkConcurrentCompression(b *testing.B) {
	testData := generateRandomData(8192)
	pool := NewCompressorPool(CompressionTypeZstd, 3)
	
	b.ResetTimer()
	b.ReportAllocs()
	
	b.RunParallel(func(pb *testing.PB) {
		dst := make([]byte, 0, len(testData)*2)
		
		for pb.Next() {
			compressor := pool.Get()
			_, err := compressor.Compress(testData, dst)
			if err != nil {
				b.Error(err)
			}
			pool.Put(compressor)
		}
	})
}

// Real-world scenario benchmarks

func BenchmarkRealWorldScenario_HighThroughput(b *testing.B) {
	config := EncoderConfig{
		InitialBufferSize:    8192,
		MaxBufferSize:        2 * 1024 * 1024,
		EnableCompression:    true,
		CompressionType:      CompressionTypeZstd,
		CompressionThreshold: 4096,
		EnableSIMD:          true,
	}
	encoder := NewBinaryEncoder[string](config)
	
	// Simulate high-throughput scenario: 1000 spans/batch, 100 batches
	spansPerBatch := 1000
	batchCount := 100
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		for j := 0; j < batchCount; j++ {
			spans := generateSpanBatch(spansPerBatch)
			_, err := encoder.EncodeSpanBatch(spans)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
	
	// Report throughput
	stats := encoder.GetStats()
	throughput := float64(stats.EncodedSpans) / b.Elapsed().Seconds()
	b.ReportMetric(throughput, "spans/sec")
}

func BenchmarkRealWorldScenario_LowLatency(b *testing.B) {
	config := EncoderConfig{
		InitialBufferSize: 4096,
		MaxBufferSize:     256 * 1024,
		EnableCompression: false, // Disable compression for low latency
		EnableSIMD:       true,
		EnableZeroCopy:   true,
	}
	encoder := NewBinaryEncoder[string](config)
	span := generateTestSpan()
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		start := time.Now()
		_, err := encoder.EncodeSpan(span)
		if err != nil {
			b.Fatal(err)
		}
		latency := time.Since(start)
		
		// Verify low latency requirement (< 1ms)
		if latency > time.Millisecond {
			b.Errorf("Latency too high: %v", latency)
		}
	}
}

// Helper functions and mock implementations

func (c CompressionType) String() string {
	switch c {
	case CompressionTypeGzip:
		return "Gzip"
	case CompressionTypeZstd:
		return "Zstd"
	case CompressionTypeLZ4:
		return "LZ4"
	case CompressionTypeSnappy:
		return "Snappy"
	default:
		return "None"
	}
}

// Mock implementations for testing

type mockSpanSnapshot struct {
	traceID   domain.TraceID
	spanID    domain.SpanID
	parentID  domain.SpanID
	name      string
	kind      domain.SpanKind
	startTime time.Time
	endTime   time.Time
	attrs     []domain.SpanAttribute[string]
	events    []domain.SpanEvent[string]
	links     []domain.SpanLink[string]
}

func (m *mockSpanSnapshot) GetTraceID() domain.TraceID { return m.traceID }
func (m *mockSpanSnapshot) GetSpanID() domain.SpanID   { return m.spanID }
func (m *mockSpanSnapshot) GetParentSpanID() domain.SpanID { return m.parentID }
func (m *mockSpanSnapshot) GetName() string { return m.name }
func (m *mockSpanSnapshot) GetKind() domain.SpanKind { return m.kind }
func (m *mockSpanSnapshot) GetStatus() domain.SpanStatus {
	return domain.SpanStatus{Code: domain.StatusCodeOK, Description: "OK"}
}
func (m *mockSpanSnapshot) GetStartTime() time.Time { return m.startTime }
func (m *mockSpanSnapshot) GetEndTime() time.Time { return m.endTime }
func (m *mockSpanSnapshot) GetDuration() time.Duration { return m.endTime.Sub(m.startTime) }
func (m *mockSpanSnapshot) GetAttributes() []domain.SpanAttribute[string] { return m.attrs }
func (m *mockSpanSnapshot) GetEvents() []domain.SpanEvent[string] { return m.events }
func (m *mockSpanSnapshot) GetLinks() []domain.SpanLink[string] { return m.links }
func (m *mockSpanSnapshot) GetResource() domain.Resource {
	return domain.Resource{Attributes: map[string]any{"service.name": "test-service"}}
}
func (m *mockSpanSnapshot) GetInstrumentationScope() domain.InstrumentationScope {
	return domain.InstrumentationScope{Name: "test-scope", Version: "1.0.0"}
}
func (m *mockSpanSnapshot) MarshalBinary() ([]byte, error) { return nil, nil }
func (m *mockSpanSnapshot) WriteBinaryTo(buf []byte) (int, error) { return 0, nil }
func (m *mockSpanSnapshot) GetArena() *domain.ArenaRef { return nil }
func (m *mockSpanSnapshot) Release() {}

func generateTraceID() domain.TraceID {
	var id domain.TraceID
	rand.Read(id[:])
	return id
}

func generateSpanID() domain.SpanID {
	var id domain.SpanID
	rand.Read(id[:])
	return id
}

func generateAttributes(count int) []domain.SpanAttribute[string] {
	attrs := make([]domain.SpanAttribute[string], count)
	for i := 0; i < count; i++ {
		attrs[i] = domain.SpanAttribute[string]{
			// In practice, these would be properly initialized
		}
	}
	return attrs
}

func generateEvents(count int) []domain.SpanEvent[string] {
	events := make([]domain.SpanEvent[string], count)
	for i := 0; i < count; i++ {
		events[i] = domain.SpanEvent[string]{
			Name:      fmt.Sprintf("event-%d", i),
			Timestamp: time.Now(),
		}
	}
	return events
}

func generateLinks(count int) []domain.SpanLink[string] {
	links := make([]domain.SpanLink[string], count)
	for i := 0; i < count; i++ {
		links[i] = domain.SpanLink[string]{
			TraceID: generateTraceID(),
			SpanID:  generateSpanID(),
		}
	}
	return links
}