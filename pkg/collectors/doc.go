// Package collectors provides a minimal interface for data collection in Tapio.
//
// This package defines the core Collector interface that all collectors must implement.
// Collectors are responsible for gathering raw data from various sources (eBPF, Kubernetes API,
// systemd, CNI, etc.) without performing any business logic or data enrichment.
//
// Key principles:
//   - Collectors only collect raw data
//   - No business logic or data transformation
//   - No Kubernetes context enrichment
//   - Events are emitted as RawEvent with just bytes and minimal metadata
//   - All intelligence happens in the pipeline layer
//
// Architecture:
//
//	┌─────────────┐  ┌─────────────┐  ┌─────────────┐
//	│    eBPF     │  │     K8s     │  │   Systemd   │
//	│  Collector  │  │  Collector  │  │  Collector  │
//	└──────┬──────┘  └──────┬──────┘  └──────┬──────┘
//	       │                 │                 │
//	       └────────────┬────┴─────────────────┘
//	                    ▼
//	            ┌──────────────┐
//	            │   RawEvent   │
//	            │  (bytes)     │
//	            └──────────────┘
//	                    │
//	                    ▼
//	            ┌──────────────┐
//	            │   Pipeline   │
//	            │ (enrichment) │
//	            └──────────────┘
//	                    │
//	                    ▼
//	            ┌──────────────┐
//	            │RawEvent +    │
//	            │K8s Context   │
//	            └──────────────┘
//
// Example usage:
//
//	collector := ebpf.NewCollector(config)
//	err := collector.Start(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer collector.Stop()
//
//	for event := range collector.Events() {
//	    // Process raw event
//	    processRawEvent(event)
//	}
package collectors
