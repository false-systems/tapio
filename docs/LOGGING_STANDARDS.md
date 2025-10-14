# Logging Standards for Tapio Observers

**Status**: Approved for Implementation
**Date**: 2025-10-14
**Author**: Architecture Team

---

## Problem Statement

Tapio observers currently use stdlib `log.Printf()` for debugging, which provides:
- ❌ No structured fields (everything is a string)
- ❌ No log levels (can't filter debug vs error)
- ❌ No OTEL trace correlation (logs disconnected from traces)
- ❌ High allocation overhead in hot paths (eBPF event processing)
- ❌ No queryable output (grep-only analysis)

**Goal**: Adopt structured logging with zero-allocation performance and OTEL integration.

---

## Solution: Zerolog

### Why Zerolog?

| Feature | stdlib `log` | zerolog | slog | zap |
|---------|-------------|---------|------|-----|
| **Zero allocation** | ❌ | ✅ | ❌ | ⚠️ (with sugar) |
| **Structured fields** | ❌ | ✅ | ✅ | ✅ |
| **JSON output** | ❌ | ✅ | ✅ | ✅ |
| **Log levels** | ❌ | ✅ | ✅ | ✅ |
| **OTEL trace context** | ❌ | ✅ (manual) | ✅ (manual) | ✅ (manual) |
| **Performance** | Slow | **Fastest** | Medium | Fast |
| **API simplicity** | Simple | **Simple** | Verbose | Complex |
| **Adoption (observability)** | Low | **High** | Growing | High |

**Decision**: Zerolog wins on performance (critical for eBPF hot paths) and simplicity.

---

## Architecture

### Logger Integration in BaseObserver

```
┌─────────────────────────────────────────────────────┐
│ BaseObserver                                        │
│                                                     │
│  ┌──────────────┐                                   │
│  │    logger    │ ← zerolog.Logger                  │
│  │ (with OTEL)  │                                   │
│  └──────────────┘                                   │
│         │                                           │
│         ├─▶ Logger(ctx) → WithTraceContext()       │
│         │                  (adds trace_id/span_id)  │
│         │                                           │
│         └─▶ Log events:                             │
│             - observer.Info().Msg("starting")       │
│             - observer.Error().Err(err).Msg("...")  │
└─────────────────────────────────────────────────────┘
```

### Trace Correlation Flow

```
OTEL Span Created
      ↓
trace_id + span_id in context
      ↓
observer.Logger(ctx) called
      ↓
WithTraceContext() extracts IDs
      ↓
Logger enriched with trace fields
      ↓
Log output includes:
{
  "level": "info",
  "observer": "network",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "span_id": "00f067aa0ba902b7",
  "message": "connection established"
}
```

---

## Implementation

### 1. Logger Initialization

**File**: `internal/base/logger.go`

```go
package base

import (
    "context"
    "os"
    "github.com/rs/zerolog"
    "go.opentelemetry.io/otel/trace"
)

// NewLogger creates structured logger for observer
func NewLogger(observerName string) zerolog.Logger {
    format := os.Getenv("TAPIO_LOG_FORMAT")
    var output io.Writer = os.Stdout

    // Console format for development (human-readable)
    if format == "console" {
        output = zerolog.ConsoleWriter{Out: os.Stdout}
    }

    // JSON format for production (default)
    return zerolog.New(output).
        With().
        Timestamp().
        Str("observer", observerName).
        Logger()
}

// WithTraceContext adds OTEL trace IDs to logger
func WithTraceContext(ctx context.Context, logger zerolog.Logger) zerolog.Logger {
    spanCtx := trace.SpanContextFromContext(ctx)
    if !spanCtx.IsValid() {
        return logger
    }

    return logger.With().
        Str("trace_id", spanCtx.TraceID().String()).
        Str("span_id", spanCtx.SpanID().String()).
        Logger()
}
```

### 2. BaseObserver Integration

**File**: `internal/base/observer.go`

```go
type BaseObserver struct {
    name   string
    logger zerolog.Logger  // ← Added
    // ... other fields
}

func NewBaseObserver(name string) (*BaseObserver, error) {
    return &BaseObserver{
        name:   name,
        logger: NewLogger(name),  // ← Initialize logger
        // ...
    }, nil
}

// Logger returns logger with optional trace context
func (b *BaseObserver) Logger(ctx context.Context) zerolog.Logger {
    return WithTraceContext(ctx, b.logger)
}
```

### 3. Usage in Observers

**Before (stdlib log)**:
```go
log.Printf("[%s] Event: %s from %s:%d", n.Name(), eventType, srcIP, srcPort)
```

**After (zerolog)**:
```go
n.Logger(ctx).Info().
    Str("event_type", eventType).
    Str("src_ip", srcIP).
    Uint16("src_port", srcPort).
    Msg("network event captured")
```

**Error logging**:
```go
// Before
log.Printf("[%s] Error: %v", n.Name(), err)

// After
n.Logger(ctx).Error().
    Err(err).
    Str("stage", "readeBPF").
    Msg("ring buffer read failed")
```

---

## Log Levels and When to Use Them

| Level | When to Use | Example |
|-------|-------------|---------|
| **Trace** | Function entry/exit, loop iterations | `logger.Trace().Msg("entering readeBPF stage")` |
| **Debug** | Variable values, state changes | `logger.Debug().Int("events", count).Msg("batch processed")` |
| **Info** | Normal operations, lifecycle events | `logger.Info().Msg("observer started")` |
| **Warn** | Recoverable errors, degraded performance | `logger.Warn().Msg("ring buffer full, dropping events")` |
| **Error** | Errors that need attention | `logger.Error().Err(err).Msg("failed to load eBPF")` |
| **Fatal** | Unrecoverable errors (exits process) | `logger.Fatal().Msg("kernel version incompatible")` |

**Default level**: `info` (set via `TAPIO_LOG_LEVEL=debug`)

---

## Environment Variables

### TAPIO_LOG_LEVEL
**Valid values**: `trace`, `debug`, `info`, `warn`, `error`, `fatal`, `panic`
**Default**: `info`

```bash
# Development: see all logs
export TAPIO_LOG_LEVEL=debug

# Production: errors only
export TAPIO_LOG_LEVEL=error
```

### TAPIO_LOG_FORMAT
**Valid values**: `json`, `console`
**Default**: `json`

```bash
# Development: human-readable with colors
export TAPIO_LOG_FORMAT=console

# Production: structured JSON
export TAPIO_LOG_FORMAT=json
```

---

## Structured Logging Best Practices

### 1. Use Typed Fields (Not String Concatenation)

```go
// ❌ BAD - Everything is a string
logger.Info().Msgf("connection from %s:%d to %s:%d", srcIP, srcPort, dstIP, dstPort)

// ✅ GOOD - Typed fields are queryable
logger.Info().
    Str("src_ip", srcIP).
    Uint16("src_port", srcPort).
    Str("dst_ip", dstIP).
    Uint16("dst_port", dstPort).
    Msg("connection established")
```

**Why**: JSON output allows querying: `jq '.[] | select(.src_port == 443)'`

### 2. Always Include Context in Hot Paths

```go
// ❌ BAD - Loses trace correlation
n.logger.Info().Msg("event processed")

// ✅ GOOD - Includes trace_id and span_id
n.Logger(ctx).Info().Msg("event processed")
```

### 3. Use Consistent Field Names

| Field | Type | Example |
|-------|------|---------|
| `event_type` | string | `"tcp_connect"` |
| `src_ip` | string | `"10.0.1.5"` |
| `dst_ip` | string | `"10.96.0.1"` |
| `src_port` | uint16 | `45678` |
| `dst_port` | uint16 | `443` |
| `pid` | int32 | `1234` |
| `comm` | string | `"curl"` |
| `error` | string | `"connection refused"` |
| `stage` | string | `"readeBPF"` |
| `duration_ms` | float64 | `10.5` |

**Follow OTEL attribute naming standards where applicable** (see OTEL_ATTRIBUTE_STANDARDS.md).

### 4. Log Events, Not State Dumps

```go
// ❌ BAD - Too much data, unclear intent
logger.Debug().
    Interface("event", event).  // Entire struct!
    Msg("got event")

// ✅ GOOD - Clear intent, relevant fields only
logger.Debug().
    Str("event_id", event.ID).
    Str("event_type", event.Type).
    Msg("processing event")
```

### 5. Include Error Context

```go
// ❌ BAD - No context about where/why error occurred
logger.Error().Err(err).Msg("failed")

// ✅ GOOD - Full context for debugging
logger.Error().
    Err(err).
    Str("stage", "loadeBPF").
    Str("program", "network_monitor.o").
    Msg("eBPF program load failed")
```

---

## Performance Considerations

### Zero-Allocation Logging in Hot Paths

**eBPF event processing loops process 100K+ events/second** - logging must not allocate.

```go
// ✅ Zero allocation (no interface{}, no reflection)
logger.Info().
    Str("src_ip", srcIP).         // No allocation
    Uint16("dst_port", dstPort).  // No allocation
    Msg("event captured")

// ❌ Allocates (avoid in hot paths)
logger.Info().
    Interface("event", event).  // reflection + allocation
    Msg("event captured")
```

### Conditional Debug Logging

```go
// Skip expensive operations if debug logging disabled
if logger.GetLevel() <= zerolog.DebugLevel {
    details := computeExpensiveDetails()  // Only if needed
    logger.Debug().
        Str("details", details).
        Msg("detailed state")
}
```

### Sampling High-Volume Logs

```go
// Sample 1 in 1000 events for debug logging
var eventCounter atomic.Uint64

if eventCounter.Add(1)%1000 == 0 {
    logger.Debug().
        Uint64("event_count", eventCounter.Load()).
        Msg("processing milestone")
}
```

---

## Migration Strategy

### Phase 1: Base Layer (This PR)
- ✅ Add zerolog dependency
- ✅ Create `internal/base/logger.go`
- ✅ Add logger field to BaseObserver
- ✅ Add `Logger(ctx)` method with trace context
- ✅ Update BaseObserver Start/Stop to use structured logging
- ✅ Add tests for logger initialization and trace context

### Phase 2: Network Observer (After Datner's PR)
- Replace all `log.Printf()` with structured logging
- Add trace context to all log statements
- Add structured fields for network events
- Verify zero-allocation performance in benchmarks

### Phase 3: Other Observers
- Migrate each observer incrementally
- Follow network observer patterns
- Maintain consistent field naming

### Phase 4: Remove stdlib log
- Remove all `import "log"` statements
- Update CLAUDE.md to mandate zerolog

---

## Testing Requirements

### Unit Tests

**File**: `internal/base/logger_test.go`

Required test coverage:
- ✅ Logger initialization with observer name
- ✅ JSON output format (default)
- ✅ Console output format (TAPIO_LOG_FORMAT=console)
- ✅ Trace context extraction (with/without span)
- ✅ Log level setting (SetGlobalLogLevel)
- ✅ Structured field serialization
- ✅ Error logging with stack context

### Integration Tests

Verify in observer tests:
```go
func TestObserver_LoggingWithTraceContext(t *testing.T) {
    obs, err := NewObserver("test", Config{})
    require.NoError(t, err)

    // Capture log output
    buf := &bytes.Buffer{}
    obs.logger = obs.logger.Output(buf)

    // Create span with trace context
    ctx, span := tracer.Start(context.Background(), "test-span")
    defer span.End()

    // Log with trace context
    obs.Logger(ctx).Info().Msg("test message")

    // Verify trace_id and span_id in output
    var logEntry map[string]interface{}
    json.Unmarshal(buf.Bytes(), &logEntry)
    assert.Contains(t, logEntry, "trace_id")
    assert.Contains(t, logEntry, "span_id")
}
```

### Performance Tests

Verify zero-allocation logging:
```go
func BenchmarkLogger_ZeroAlloc(b *testing.B) {
    logger := NewLogger("test")
    ctx := context.Background()

    b.ReportAllocs()
    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        logger.Info().
            Str("event", "tcp_connect").
            Str("src_ip", "10.0.1.5").
            Uint16("dst_port", 443).
            Msg("connection")
    }

    // Target: 0 allocs/op
}
```

---

## Example Output

### JSON Format (Production)

```json
{"level":"info","observer":"network","time":"2025-10-14T23:45:12+02:00","message":"observer starting"}
{"level":"info","observer":"network","time":"2025-10-14T23:45:12+02:00","message":"eBPF programs loaded"}
{"level":"info","observer":"network","trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","span_id":"00f067aa0ba902b7","event_type":"connection_established","src_ip":"10.244.1.5","src_port":45678,"dst_ip":"10.96.0.1","dst_port":443,"pid":1234,"comm":"curl","time":"2025-10-14T23:45:13+02:00","message":"network event captured"}
{"level":"warn","observer":"network","dropped":1,"time":"2025-10-14T23:45:14+02:00","message":"ring buffer full"}
{"level":"error","observer":"network","error":"connection refused","stage":"processEvent","time":"2025-10-14T23:45:15+02:00","message":"event processing failed"}
```

**Query with jq**:
```bash
# Show all errors
cat tapio.log | jq 'select(.level == "error")'

# Show connections to port 443
cat tapio.log | jq 'select(.dst_port == 443)'

# Show events with specific trace
cat tapio.log | jq 'select(.trace_id == "4bf92f3577b34da6a3ce929d0e0e4736")'
```

### Console Format (Development)

```
2025-10-14T23:45:12+02:00 INF observer starting observer=network
2025-10-14T23:45:12+02:00 INF eBPF programs loaded observer=network
2025-10-14T23:45:13+02:00 INF network event captured comm=curl dst_ip=10.96.0.1 dst_port=443 event_type=connection_established observer=network pid=1234 span_id=00f067aa0ba902b7 src_ip=10.244.1.5 src_port=45678 trace_id=4bf92f3577b34da6a3ce929d0e0e4736
2025-10-14T23:45:14+02:00 WRN ring buffer full dropped=1 observer=network
2025-10-14T23:45:15+02:00 ERR event processing failed error="connection refused" observer=network stage=processEvent
```

---

## CLAUDE.md Updates

Add to **Logging Standards** section:

```markdown
## 📝 LOGGING STANDARDS

### Structured Logging with Zerolog

**MANDATORY**: Use zerolog for all logging.

❌ **BANNED**: `import "log"`, `log.Printf()`, `fmt.Println()` for logging

✅ **REQUIRED**: Structured logging with typed fields

```go
// Access logger via BaseObserver
observer.Logger(ctx).Info().
    Str("event_type", "tcp_connect").
    Uint16("dst_port", 443).
    Msg("connection established")
```

### Logging Rules

1. **Always use context**: `observer.Logger(ctx)` (includes trace IDs)
2. **Typed fields only**: No `Msgf()` string concatenation
3. **Consistent names**: Follow OTEL naming standards
4. **Zero allocation**: No `Interface()` in hot paths
5. **Appropriate levels**: debug/info/warn/error/fatal
6. **Error context**: Include stage, operation, relevant fields

### Environment Variables

- `TAPIO_LOG_LEVEL`: trace|debug|info|warn|error (default: info)
- `TAPIO_LOG_FORMAT`: json|console (default: json)

See: `docs/LOGGING_STANDARDS.md`
```

---

## Benefits Summary

### Before (stdlib log)
```go
log.Printf("[network] Event: tcp_connect from 10.0.1.5:45678 to 10.96.0.1:443 (pid=1234)")
```
- ❌ Unstructured string (grep-only analysis)
- ❌ No log levels
- ❌ No trace correlation
- ❌ Allocates on every call

### After (zerolog)
```go
obs.Logger(ctx).Info().
    Str("event_type", "tcp_connect").
    Str("src_ip", "10.0.1.5").
    Uint16("src_port", 45678).
    Str("dst_ip", "10.96.0.1").
    Uint16("dst_port", 443).
    Int32("pid", 1234).
    Msg("network event captured")
```
- ✅ Structured JSON (queryable with jq)
- ✅ Log levels (filter in production)
- ✅ OTEL trace correlation (trace_id/span_id)
- ✅ Zero allocations (hot path safe)
- ✅ Consistent field naming (OTEL standards)

---

## Verification Checklist

- [x] zerolog dependency added to go.mod
- [x] `internal/base/logger.go` created
- [x] BaseObserver includes logger field
- [x] `Logger(ctx)` method with trace context
- [x] BaseObserver Start/Stop use structured logging
- [x] Unit tests for logger (80%+ coverage)
- [x] Integration tests with trace context
- [ ] Performance tests (zero allocation benchmark)
- [ ] Documentation in CLAUDE.md
- [ ] Example migration (network observer)

---

## References

- [Zerolog Documentation](https://github.com/rs/zerolog)
- [OTEL Attribute Standards](./OTEL_ATTRIBUTE_STANDARDS.md)
- [OTEL Trace Context Spec](https://www.w3.org/TR/trace-context/)
- [Zero Allocation Logging Best Practices](https://dave.cheney.net/2015/11/05/lets-talk-about-logging)
