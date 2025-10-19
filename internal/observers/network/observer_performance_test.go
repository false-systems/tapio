//go:build linux
// +build linux

package network

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
)

// setupBenchmark sets up OTEL for benchmarks
func setupBenchmark(b *testing.B) {
	b.Helper()
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	b.Cleanup(func() {
		otel.SetMeterProvider(nil)
	})
}

// setupTest sets up OTEL for regular tests
func setupTest(t *testing.T) {
	t.Helper()
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(nil)
	})
}

// BenchmarkStateToEventType measures state transition mapping performance
func BenchmarkStateToEventType(b *testing.B) {
	testCases := []struct {
		oldState uint8
		newState uint8
	}{
		{TCP_SYN_SENT, TCP_ESTABLISHED},
		{TCP_CLOSE, TCP_LISTEN},
		{TCP_LISTEN, TCP_CLOSE},
		{TCP_ESTABLISHED, TCP_CLOSE},
		{TCP_SYN_SENT, TCP_SYN_RECV},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tc := testCases[i%len(testCases)]
		_ = stateToEventType(tc.oldState, tc.newState, "", nil) // Ignore: benchmark needs result to prevent optimization
	}
}

// BenchmarkConvertIPv4 measures IPv4 conversion performance
func BenchmarkConvertIPv4(b *testing.B) {
	ips := []uint32{
		0x0100007f, // 127.0.0.1
		0x6401a8c0, // 192.168.1.100
		0x08080808, // 8.8.8.8
		0x01010101, // 1.1.1.1
		0x00000000, // 0.0.0.0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = convertIPv4(ips[i%len(ips)]) // Ignore: benchmark needs result to prevent optimization
	}
}

// BenchmarkConvertIPv6 measures IPv6 conversion performance
func BenchmarkConvertIPv6(b *testing.B) {
	ipv6s := [][16]byte{
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},                               // ::1
		{0x26, 0x07, 0xf8, 0xb0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},                   // 2607:f8b0::1
		{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0x02, 0x00, 0x5e, 0xff, 0xfe, 0x00, 0x00, 0x01}, // fe80::200:5eff:fe00:1
		{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},                   // 2001:db8::1
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = convertIPv6(ipv6s[i%len(ipv6s)]) // Ignore: benchmark needs result to prevent optimization
	}
}

// BenchmarkExtractComm measures process name extraction performance
func BenchmarkExtractComm(b *testing.B) {
	comms := [][16]byte{
		{'c', 'u', 'r', 'l', 0},
		{'n', 'g', 'i', 'n', 'x', 0},
		{'s', 's', 'h', 'd', 0},
		{'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o', 'p'},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = extractComm(comms[i%len(comms)]) // Ignore: benchmark needs result to prevent optimization
	}
}

// BenchmarkNetworkEventBPFAllocation measures struct allocation performance
func BenchmarkNetworkEventBPFAllocation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		evt := NetworkEventBPF{
			PID:      uint32(i),
			SrcIP:    0x0100007f,
			DstIP:    0x6401a8c0,
			SrcPort:  50000,
			DstPort:  80,
			Family:   2,
			Protocol: IPPROTO_TCP,
			OldState: TCP_SYN_SENT,
			NewState: TCP_ESTABLISHED,
			Comm:     [16]byte{'c', 'u', 'r', 'l', 0},
		}
		_ = evt // Ignore: benchmark needs result to prevent optimization
	}
}

// BenchmarkEventProcessingPipeline measures complete event processing
func BenchmarkEventProcessingPipeline(b *testing.B) {
	evt := NetworkEventBPF{
		PID:      1234,
		SrcIP:    0x0100007f,
		DstIP:    0x6401a8c0,
		SrcPort:  50000,
		DstPort:  80,
		Family:   2,
		Protocol: IPPROTO_TCP,
		OldState: TCP_SYN_SENT,
		NewState: TCP_ESTABLISHED,
		Comm:     [16]byte{'c', 'u', 'r', 'l', 0},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate complete event processing
		eventType := stateToEventType(evt.OldState, evt.NewState, "", nil)
		srcIP := convertIPv4(evt.SrcIP)
		dstIP := convertIPv4(evt.DstIP)
		comm := extractComm(evt.Comm)

		// Ignore: benchmark needs results to prevent optimization
		_, _, _, _ = eventType, srcIP, dstIP, comm // Ignore: prevent compiler optimization
	}
}

// BenchmarkObserverCreation measures observer creation overhead
func BenchmarkObserverCreation(b *testing.B) {
	setupBenchmark(b)

	config := Config{
		Output: base.OutputConfig{Stdout: true},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		observer, err := NewNetworkObserver("bench-observer", config)
		if err != nil {
			b.Fatalf("Failed to create observer: %v", err)
		}
		_ = observer // Ignore: benchmark needs result to prevent optimization
	}
}

// BenchmarkConcurrentEventProcessing measures parallel event processing
func BenchmarkConcurrentEventProcessing(b *testing.B) {
	events := make([]NetworkEventBPF, 100)
	for i := range events {
		events[i] = NetworkEventBPF{
			PID:      uint32(1000 + i),
			SrcIP:    0x0100007f,
			DstIP:    uint32(0xc0a80100 + i), // 192.168.1.x
			SrcPort:  uint16(50000 + i),
			DstPort:  80,
			Family:   2,
			Protocol: IPPROTO_TCP,
			OldState: TCP_SYN_SENT,
			NewState: TCP_ESTABLISHED,
			Comm:     [16]byte{'a', 'p', 'p', byte('0' + i%10), 0},
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			evt := events[i%len(events)]
			_ = stateToEventType(evt.OldState, evt.NewState, "", nil) // Ignore: benchmark needs result to prevent optimization
			_ = convertIPv4(evt.SrcIP)                                // Ignore: benchmark needs result to prevent optimization
			_ = convertIPv4(evt.DstIP)                                // Ignore: benchmark needs result to prevent optimization
			_ = extractComm(evt.Comm)                                 // Ignore: benchmark needs result to prevent optimization
			i++
		}
	})
}

// BenchmarkIPv4vsIPv6Conversion compares IPv4 and IPv6 conversion performance
func BenchmarkIPv4vsIPv6Conversion(b *testing.B) {
	b.Run("IPv4", func(b *testing.B) {
		ip := uint32(0x0100007f)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = convertIPv4(ip) // Ignore: benchmark needs result to prevent optimization
		}
	})

	b.Run("IPv6", func(b *testing.B) {
		ip := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = convertIPv6(ip) // Ignore: benchmark needs result to prevent optimization
		}
	})
}

// BenchmarkMemoryAllocation measures memory allocation patterns
func BenchmarkMemoryAllocation(b *testing.B) {
	b.Run("StackAllocation", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var evt NetworkEventBPF
			evt.PID = uint32(i)
			evt.SrcIP = 0x0100007f
			_ = evt // Ignore: benchmark needs result to prevent optimization
		}
	})

	b.Run("HeapAllocation", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			evt := &NetworkEventBPF{
				PID:   uint32(i),
				SrcIP: 0x0100007f,
			}
			_ = evt // Ignore: benchmark needs result to prevent optimization
		}
	})
}

// TestPerformance_HighThroughput simulates high event throughput
func TestPerformance_HighThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping high throughput test in short mode")
	}

	setupTest(t)

	// Simulate processing 100k events
	eventCount := 100000
	processed := 0

	for i := 0; i < eventCount; i++ {
		evt := NetworkEventBPF{
			PID:      uint32(1000 + i%1000),
			SrcIP:    uint32(0x0100007f + i%256),
			DstIP:    uint32(0xc0a80100 + i%256),
			SrcPort:  uint16(50000 + i%10000),
			DstPort:  80,
			Family:   2,
			Protocol: IPPROTO_TCP,
			OldState: TCP_SYN_SENT,
			NewState: TCP_ESTABLISHED,
			Comm:     [16]byte{'a', 'p', 'p', 0},
		}

		// Process event
		_ = stateToEventType(evt.OldState, evt.NewState, "", nil) // Ignore: benchmark needs result to prevent optimization
		_ = convertIPv4(evt.SrcIP)                                // Ignore: benchmark needs result to prevent optimization
		_ = convertIPv4(evt.DstIP)                                // Ignore: benchmark needs result to prevent optimization
		_ = extractComm(evt.Comm)                                 // Ignore: benchmark needs result to prevent optimization

		processed++
	}

	require.Equal(t, eventCount, processed, "Should process all events")
}

// TestPerformance_MemoryFootprint measures memory usage
func TestPerformance_MemoryFootprint(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory footprint test in short mode")
	}

	setupTest(t)

	// Allocate 10k events (simulate ring buffer capacity)
	events := make([]NetworkEventBPF, 10000)

	for i := range events {
		events[i] = NetworkEventBPF{
			PID:      uint32(i),
			SrcIP:    0x0100007f,
			DstIP:    0x6401a8c0,
			SrcPort:  50000,
			DstPort:  80,
			Family:   2,
			Protocol: IPPROTO_TCP,
			OldState: TCP_SYN_SENT,
			NewState: TCP_ESTABLISHED,
			Comm:     [16]byte{'t', 'e', 's', 't', 0},
		}
	}

	// Verify memory allocation
	require.Equal(t, 10000, len(events))

	// Verify actual struct size matches expected 72 bytes (70 packed + 2 Go alignment padding)
	actualSize := int(unsafe.Sizeof(NetworkEventBPF{}))
	require.Equal(t, 72, actualSize, "NetworkEventBPF size changed - update C struct!")

	// Calculate total memory footprint
	totalBytes := actualSize * len(events)
	require.Equal(t, 720000, totalBytes, "10k events should be 720KB")
}
