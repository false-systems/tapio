// Package performance provides high-performance data structures and utilities
// for the Tapio observability platform. It is designed to handle 165k+ events
// per second with minimal overhead and zero dependencies.
//
// Key Components:
//
// - RingBuffer: Lock-free multi-producer multi-consumer ring buffer
// - EventBuffer: Type-safe wrapper for UnifiedEvent processing
// - ObjectPool: Per-CPU object pools to reduce GC pressure
// - Metrics: Performance metrics tracking
//
// All components are designed with:
// - Zero-copy operations where possible
// - Cache-line padding to prevent false sharing
// - Power-of-2 sizing for optimal performance
// - Minimal allocations in hot paths
//
// This package belongs to the domain layer (Level 0) and has zero dependencies
// on other Tapio packages, making it available to all layers.
package performance
