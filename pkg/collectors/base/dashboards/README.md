# Tapio Collector Dashboards

## Overview

The base collector package provides a unified Grafana dashboard system that works for ALL Tapio collectors. This eliminates dashboard duplication while allowing collector-specific extensions.

## Architecture

### 1. Base Dashboard (`tapio-collector-base.json`)
- **Single dashboard for all collectors** using Grafana variables
- **Automatic collector discovery** via `$collector_name` variable
- **Standard panels** for all collectors:
  - Health status gauge (Healthy/Degraded/Unhealthy)
  - Event processing rate
  - Error rate percentage
  - Drop counters
  - Processing latency percentiles (P50/P90/P95/P99)
  - Event size distribution
  - Lifetime counters

### 2. Dashboard Extension System (`dashboard.go`)
Collectors can optionally create extended dashboards with:
- Protocol-specific visualizations
- Custom metrics
- Specialized panels
- Additional variables

## Usage

### For Operators

1. **Import the base dashboard** once:
```bash
# Import to Grafana
curl -X POST http://grafana:3000/api/dashboards/db \
  -H "Content-Type: application/json" \
  -d @pkg/collectors/base/dashboards/tapio-collector-base.json
```

2. **Select any collector** from the dropdown:
- Network collector
- Memory-leak-hunter
- Syscall-errors
- Resource-starvation
- Any future collector automatically appears!

3. **Navigate to extended dashboards** via dashboard links (if available)

### For Developers

#### Using Base Dashboard Only
Your collector automatically works with the base dashboard if it:
1. Embeds `BaseCollector`
2. Uses standard metric names: `{collector_name}_events_processed_total`, etc.
3. Calls `RecordEvent()`, `RecordError()`, `RecordDrop()`

That's it! No dashboard JSON needed.

#### Adding Extended Dashboard
```go
// In your collector package
func GetDashboardExtension() *base.DashboardExtension {
    return &base.DashboardExtension{
        RowTitle: "Network Protocol Analysis",
        Panels: []base.Panel{
            {
                Title: "Protocol Distribution",
                Type:  "piechart",
                GridPos: base.GridPosition{H: 8, W: 12, X: 0, Y: 1},
                Targets: []base.Target{
                    {
                        Expr: "sum by (protocol) (rate(network_events_by_protocol[5m]))",
                        LegendFormat: "{{protocol}}",
                    },
                },
            },
        },
        Variables: []base.Variable{
            {
                Name:  "protocol",
                Label: "Protocol Filter",
                Type:  "query",
                Query: "label_values(network_events_by_protocol, protocol)",
            },
        },
        Tags: []string{"network", "l7"},
    }
}
```

Generate the extended dashboard:
```go
extension := GetDashboardExtension()
dashboardJSON, _ := base.GenerateDashboardConfig("network", extension)
// Save to network/dashboards/network-extended.json
```

## Benefits

### Consistency
- ✅ Same layout for all collectors
- ✅ Predictable metric locations
- ✅ Unified alerting rules

### Maintainability
- ✅ Update base dashboard → all collectors updated
- ✅ Fix a bug once → fixed everywhere
- ✅ Add new base metric → available to all

### Scalability
- ✅ New collectors work immediately
- ✅ No dashboard proliferation
- ✅ Optional extensions for specialized needs

## Dashboard Variables

| Variable | Description | Example Values |
|----------|-------------|----------------|
| `$datasource` | Prometheus instance | `prometheus-prod` |
| `$collector_name` | Collector to monitor | `network`, `memory_leak_hunter` |

## Metric Naming Convention

All collectors MUST follow this naming pattern:
```
{collector_name}_{metric_name}_{unit}
```

Examples:
- `network_events_processed_total`
- `memory_leak_hunter_errors_total`
- `syscall_errors_processing_duration_seconds`

## Extended Dashboard Guidelines

When creating extended dashboards:

1. **Link back to base**: Always include a link to the base dashboard
2. **Use consistent colors**: Follow Grafana's color schemes
3. **Add descriptions**: Help operators understand metrics
4. **Group related panels**: Use rows to organize
5. **Tag appropriately**: Use `tapio-extended` tag for discovery

## Migration Path

For existing collectors with custom dashboards:

1. **Phase 1**: Ensure metrics follow naming convention
2. **Phase 2**: Test with base dashboard
3. **Phase 3**: Extract unique panels to extension
4. **Phase 4**: Deprecate old dashboard

## Testing

Verify your collector works with the dashboard:
```bash
# Start your collector
./tapio --collector=your-collector

# Check metrics endpoint
curl http://localhost:9090/metrics | grep your_collector_

# Open Grafana dashboard
# Select your collector from dropdown
# Verify all panels show data
```

## Future Enhancements

- [ ] Dashboard provisioning via ConfigMap
- [ ] Automated screenshot testing
- [ ] Alert rule templates
- [ ] SLO/SLI dashboard variants
- [ ] Mobile-responsive layouts