// Package observers provides a minimal interface for failure observation in Tapio.
//
// This package defines the core Observer interface that all observers must implement.
// Observers are responsible for detecting failures, anomalies, and critical events
// from various sources (eBPF, Kubernetes API, systemd, CNI, etc.) focusing on
// what's wrong rather than collecting everything.
//
// Key principles:
//   - Observers focus on failures and anomalies
//   - No collection of "normal" events
//   - Minimal overhead through selective observation
//   - Events are emitted only when something is wrong
//   - All intelligence happens in the pipeline layer
//
// Architecture:
//
//	┌─────────────┐  ┌─────────────┐  ┌─────────────┐
//	│    eBPF     │  │     K8s     │  │   Systemd   │
//	│  Observer   │  │  Observer   │  │  Observer   │
//	└──────┬──────┘  └──────┬──────┘  └──────┬──────┘
//	       │                 │                 │
//	       └────────────┬────┴─────────────────┘
//	                    ▼
//	            ┌──────────────┐
//	            │   Failure    │
//	            │   Event      │
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
//	            │Failure +     │
//	            │K8s Context   │
//	            └──────────────┘
//
// Example usage:
//
//	observer := ebpf.NewObserver(config)
//	err := observer.Start(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer observer.Stop()
//
//	for event := range observer.Events() {
//	    // Process failure event
//	    processFailureEvent(event)
//	}
package observers
