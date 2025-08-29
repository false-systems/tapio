package runtime_signals

import (
	"context"
	"testing"
)

func BenchmarkCollectorStart(b *testing.B) {
	for i := 0; i < b.N; i++ {
		collector, _ := NewCollector("bench")
		ctx := context.Background()
		collector.Start(ctx)
		collector.Stop()
	}
}

func BenchmarkCreateEvent(b *testing.B) {
	collector, _ := NewCollector("bench")
	data := map[string]string{
		"pid":        "1234",
		"comm":       "test-process",
		"netns_path": "/var/run/netns/cni-550e8400-e29b-41d4-a716-446655440000",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.createEvent("netns_create", data)
	}
}

func BenchmarkParseK8sFromNetns(b *testing.B) {
	collector, _ := NewCollector("bench")
	netnsPath := "/var/run/netns/cni-550e8400-e29b-41d4-a716-446655440000"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.parseK8sFromNetns(netnsPath)
	}
}

func BenchmarkConcurrentHealthCheck(b *testing.B) {
	collector, _ := NewCollector("bench")
	ctx := context.Background()
	collector.Start(ctx)
	defer collector.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			collector.IsHealthy()
		}
	})
}
