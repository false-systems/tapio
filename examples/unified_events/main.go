package main

import (
	"fmt"
	"log"

	"github.com/yairfalse/tapio/pkg/events"
)

func main() {
	fmt.Println("🚀 Tapio Unified Message Format Demo")
	fmt.Println("=====================================")

	// Example 1: Network event
	networkEvent := events.NewBuilder().
		WithType("network.connection", events.EventCategory_CATEGORY_NETWORK).
		WithSeverity(events.EventSeverity_SEVERITY_INFO).
		WithSource("ebpf", "network-collector", "node-1").
		WithEntity(events.EntityType_ENTITY_PROCESS, "1234", "nginx").
		WithProcess(1234, "nginx").
		WithNetworkData(&events.NetworkEvent{
			Protocol:      "tcp",
			SrcIp:         "192.168.1.1",
			SrcPort:       8080,
			DstIp:         "192.168.1.2",
			DstPort:       80,
			BytesSent:     1024,
			BytesReceived: 2048,
			State:         "ESTABLISHED",
		}).
		WithAttribute("connection_id", "conn-123").
		WithLabel("environment", "production").
		Build()

	fmt.Printf("✅ Created network event: %s\n", networkEvent.Id)
	fmt.Printf("   Type: %s, Category: %s\n", networkEvent.Metadata.Type, networkEvent.Metadata.Category)
	fmt.Printf("   Size: %d bytes\n", networkEvent.Size())

	// Example 2: Memory event
	memoryEvent := events.NewBuilder().
		WithType("memory.allocation", events.EventCategory_CATEGORY_MEMORY).
		WithSeverity(events.EventSeverity_SEVERITY_WARNING).
		WithSource("ebpf", "memory-collector", "node-1").
		WithEntity(events.EntityType_ENTITY_PROCESS, "5678", "app").
		WithProcess(5678, "myapp").
		WithMemoryData(&events.MemoryEvent{
			Operation:  "alloc",
			SizeBytes:  4096,
			Address:    0x7fff12345678,
			RssBytes:   1024 * 1024,
			VmsBytes:   2048 * 1024,
			Allocator:  "malloc",
			StackTrace: []string{"main", "allocate", "malloc"},
		}).
		WithAttribute("memory_pressure", "high").
		WithLabel("app", "myapp").
		Build()

	fmt.Printf("✅ Created memory event: %s\n", memoryEvent.Id)
	fmt.Printf("   Type: %s, Category: %s\n", memoryEvent.Metadata.Type, memoryEvent.Metadata.Category)
	fmt.Printf("   High Priority: %t\n", memoryEvent.IsHighPriority())

	// Example 3: Validation
	validator := events.NewValidator()

	if err := validator.Validate(networkEvent); err != nil {
		log.Printf("❌ Network event validation failed: %v", err)
	} else {
		fmt.Println("✅ Network event validation passed")
	}

	if err := validator.Validate(memoryEvent); err != nil {
		log.Printf("❌ Memory event validation failed: %v", err)
	} else {
		fmt.Println("✅ Memory event validation passed")
	}

	// Example 4: Serialization
	data, err := networkEvent.SerializeFast()
	if err != nil {
		log.Printf("❌ Serialization failed: %v", err)
	} else {
		fmt.Printf("✅ Serialized to %d bytes\n", len(data))
	}

	// Example 5: Deserialization
	deserializedEvent, err := events.DeserializeFast(data)
	if err != nil {
		log.Printf("❌ Deserialization failed: %v", err)
	} else {
		fmt.Printf("✅ Deserialized event: %s\n", deserializedEvent.Id)
		fmt.Printf("   Original and deserialized IDs match: %t\n", networkEvent.Id == deserializedEvent.Id)
	}

	// Example 6: Batch processing
	batchBuilder := events.NewBatchBuilder()
	batchBuilder.Add(networkEvent)
	batchBuilder.Add(memoryEvent)

	batch := batchBuilder.Build()
	fmt.Printf("✅ Created batch with %d events (ID: %s)\n", len(batch.Events), batch.BatchId)

	// Example 7: Event statistics
	stats := events.GetEventStats()
	fmt.Printf("📊 Event Statistics:\n")
	fmt.Printf("   Created: %d\n", stats.Created)
	fmt.Printf("   In Flight: %d\n", stats.InFlight)
	fmt.Printf("   Total Size: %d bytes\n", stats.TotalSize)

	// Clean up
	events.ReleaseEvent(networkEvent)
	events.ReleaseEvent(memoryEvent)
	events.ReleaseEvent(deserializedEvent)

	finalStats := events.GetEventStats()
	fmt.Printf("📊 Final Statistics:\n")
	fmt.Printf("   Released: %d\n", finalStats.Released)
	fmt.Printf("   In Flight: %d\n", finalStats.InFlight)

	fmt.Println("\n🎉 Unified Message Format Demo Complete!")
}
