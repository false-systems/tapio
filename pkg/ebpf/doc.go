/*
Package ebpf provides eBPF-based system monitoring capabilities for Tapio.

# Architecture Overview

The package is organized into several logical components:

## Core Components

1. **Collectors** - Responsible for loading and managing eBPF programs
  - Collector: Basic memory monitoring collector
  - EnhancedCollector: Extended monitoring with network, DNS, CPU, and I/O

2. **Event Types** - Structured data representing kernel events
  - Memory events: MemoryEvent, OOMEvent
  - Network events: NetworkEvent, PacketEvent, DNSEvent, ProtocolEvent
  - System events: CPUEvent, IOEvent, SystemEvent
  - Simple events: Lightweight variants for basic monitoring

3. **Parsers** - Convert raw eBPF data to structured events
  - EventParser interface: Common parsing contract
  - Specific parsers: NetworkEventParser, DNSEventParser, etc.
  - Parser registry: Manages available parsers

4. **Managers** - Handle collection lifecycle and data flow
  - RingBufferManager: Handles eBPF ring buffer operations
  - ErrorHandler: Centralized error handling with recovery
  - Note: Collector management is handled by pkg/collectors to avoid import cycles

## Design Principles

1. **Separation of Concerns**
  - Collectors focus on eBPF program management
  - Parsers handle data transformation
  - Managers coordinate operations

2. **Type Safety**
  - Strongly typed event structures
  - Consistent event categorization
  - Type-safe parser registration

3. **Error Handling**
  - Detailed error context with ParserError
  - Recovery strategies for transient failures
  - Circuit breaker pattern for system protection

4. **Performance**
  - Ring buffer for efficient kernel-userspace communication
  - Batched event processing
  - Minimal allocation in hot paths

## Usage Example

	// Create enhanced collector with all monitoring capabilities
	collector := ebpf.NewEnhancedCollector()

	// Start collection
	events, err := collector.Start()
	if err != nil {
		log.Fatal(err)
	}

	// Process events
	for event := range events {
		switch e := event.Data.(type) {
		case *NetworkEvent:
			handleNetwork(e)
		case *MemoryEvent:
			handleMemory(e)
		}
	}

## Event Categories

Events are categorized for easier processing:
- CategoryNetwork: TCP/UDP connection events
- CategoryDNS: DNS query/response events
- CategoryMemory: Memory allocation/OOM events
- CategoryCPU: CPU scheduling/throttling events
- CategoryIO: Disk I/O events
- CategoryPacket: Low-level packet events
- CategoryProtocol: Application protocol events

Each category has specific event subtypes for fine-grained monitoring.
*/
package ebpf
