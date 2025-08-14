# Tapio CRI Collector Dashboard

This directory contains Grafana dashboard configuration for monitoring the Tapio CRI Collector with comprehensive OTEL instrumentation.

## Dashboard Overview

The dashboard provides real-time visibility into:

- **Container Events Processing**: Rate and types of container lifecycle events
- **OOM Kill Detection**: Critical memory exhaustion events from both CRI and eBPF
- **Performance Metrics**: Processing latency, buffer usage, and resource consumption
- **eBPF Metrics**: Kernel-level event collection and processing (Linux only)
- **Error Tracking**: CRI API errors and eBPF failures
- **Resource Utilization**: Memory usage, buffer capacity, and active containers

## Files

- `cri-collector.json`: Raw Grafana dashboard JSON
- `dashboard-configmap.yaml`: Kubernetes ConfigMap for automated dashboard deployment
- `README.md`: This documentation file

## Dashboard Panels

### 1. CRI Events Processed Rate
- **Type**: Stat panel
- **Metrics**: `rate(cri_events_processed_total[5m])`
- **Purpose**: Monitor event processing throughput by type
- **Thresholds**: Green < 10 events/sec, Yellow < 100 events/sec, Red > 100 events/sec

### 2. OOM Kills Detected
- **Type**: Stat panel with alerting
- **Metrics**: 
  - `sum(rate(cri_oom_kills_total[5m]))` (rate)
  - `sum(cri_oom_kills_total)` (total)
- **Purpose**: Critical memory management monitoring
- **Alert**: Triggers when OOM kill rate > 0.5/sec for 1 minute

### 3. Container Processing Latency
- **Type**: Time series
- **Metrics**: Histogram quantiles (p50, p95, p99) of `cri_processing_latency_bucket`
- **Purpose**: Monitor CRI API call performance
- **Thresholds**: Green < 100ms, Yellow < 500ms, Red > 500ms

### 4. Buffer and Resource Usage
- **Type**: Time series with alerting
- **Metrics**: 
  - `cri_buffer_usage_percent` 
  - `process_resident_memory_bytes`
- **Purpose**: Monitor memory usage and internal buffer capacity
- **Alert**: Triggers when buffer usage > 85% for 2 minutes

### 5. Active Containers & Events
- **Type**: Time series
- **Metrics**: 
  - `cri_containers_active`
  - `rate(cri_events_dropped_total[5m]) * 60`
- **Purpose**: Track container count and dropped events

### 6. CRI and eBPF Errors
- **Type**: Time series
- **Metrics**: Error rates from CRI operations and eBPF loading/attachment
- **Purpose**: Monitor system reliability and troubleshoot issues

### 7. eBPF Events & Kernel OOM Kills
- **Type**: Time series (Linux only)
- **Metrics**: 
  - `rate(cri_ebpf_events_total[5m])`
  - `rate(cri_ebpf_events_dropped_total[5m])`
  - `rate(cri_ebpf_kernel_oom_kills_total[5m])`
- **Purpose**: Monitor kernel-level event collection

### 8. Container Events by Type
- **Type**: Pie chart
- **Metrics**: `sum by (event_type) (increase(cri_events_processed_total[15m]))`
- **Purpose**: Visualize event type distribution

### 9. State Check Performance
- **Type**: Time series
- **Metrics**: 
  - `rate(cri_checks_performed_total[5m])`
  - `histogram_quantile(0.95, rate(cri_batch_size_bucket[5m]))`
- **Purpose**: Monitor polling efficiency and batch processing

## Deployment

### Option 1: Manual Import

1. Copy the contents of `cri-collector.json`
2. In Grafana UI: **+** → **Import** → **Import via panel json**
3. Paste the JSON and configure data source

### Option 2: Kubernetes ConfigMap (Recommended)

```bash
# Deploy the dashboard ConfigMap
kubectl apply -f dashboard-configmap.yaml

# Verify deployment
kubectl get configmap tapio-cri-collector-dashboard -n monitoring
```

**Prerequisites:**
- Grafana with dashboard sidecar enabled
- ConfigMaps labeled with `grafana_dashboard: "1"` are automatically loaded
- Prometheus data source configured in Grafana

### Option 3: Grafana Operator

If using the Grafana Operator, create a `GrafanaDashboard` resource:

```yaml
apiVersion: integreatly.org/v1alpha1
kind: GrafanaDashboard
metadata:
  name: tapio-cri-collector
  labels:
    app: grafana
spec:
  datasources:
    - inputName: "DS_PROMETHEUS"
      datasourceName: "prometheus"
  json: |
    # Content from cri-collector.json
```

## Data Sources

The dashboard expects the following data sources:

### Primary Data Source: Prometheus
- **Name**: `prometheus` (default) or `${DS_PROMETHEUS}` (templated)
- **Required metrics**: All `cri_*` prefixed metrics from the CRI collector
- **Scrape interval**: Recommended 15-30s for production

### Optional Data Sources: 
- **Grafana**: For annotations and alerts overlay
- **Process metrics**: Standard Go process metrics for resource monitoring

## Template Variables

The dashboard includes template variables for filtering:

### Collector Filter
- **Name**: `$collector`
- **Query**: `label_values(cri_events_processed_total, collector)`
- **Purpose**: Filter by collector instance
- **Multi-select**: Yes
- **Include All**: Yes

### Namespace Filter
- **Name**: `$namespace`
- **Query**: `label_values(cri_events_processed_total{collector=~"$collector"}, namespace)`
- **Purpose**: Filter by Kubernetes namespace
- **Multi-select**: Yes
- **Include All**: Yes

## Alerting Rules

The dashboard includes built-in alerts:

### High OOM Kill Rate
- **Condition**: `rate(cri_oom_kills_total[5m]) > 0.5`
- **Duration**: 1 minute
- **Severity**: Critical
- **Purpose**: Detect memory pressure issues

### CRI Buffer Usage Critical
- **Condition**: `cri_buffer_usage_percent > 85`
- **Duration**: 2 minutes
- **Severity**: Warning
- **Purpose**: Prevent event loss due to buffer overflow

## Customization

### Adding Custom Panels

1. Edit the dashboard in Grafana UI
2. Export the updated JSON
3. Update `cri-collector.json` and `dashboard-configmap.yaml`
4. Redeploy via kubectl

### Custom Alerts

Add additional alert conditions by:

1. Creating new panels with alert rules
2. Using Grafana's unified alerting
3. Integrating with external alert managers

### Environment-Specific Tuning

Adjust thresholds based on your environment:

```json
{
  "thresholds": {
    "steps": [
      {"color": "green", "value": null},
      {"color": "yellow", "value": 50},  // Adjust based on load
      {"color": "red", "value": 200}     // Adjust based on capacity
    ]
  }
}
```

## Troubleshooting

### No Data Visible

1. **Check data source configuration**:
   ```bash
   # Verify Prometheus is scraping CRI collector metrics
   curl -s "http://prometheus:9090/api/v1/query?query=cri_events_processed_total" | jq
   ```

2. **Verify CRI collector is running**:
   ```bash
   kubectl logs -l app=tapio-cri-collector -n tapio-system
   ```

3. **Check metric labels match queries**:
   ```bash
   # List all CRI metrics
   curl -s "http://prometheus:9090/api/v1/label/__name__/values" | jq '.data[] | select(startswith("cri_"))'
   ```

### Panels Show "N/A"

- **Template variables**: Ensure `$collector` and `$namespace` have valid selections
- **Time range**: Verify the selected time range has data
- **Metric availability**: Check if eBPF metrics exist (Linux only)

### Performance Issues

- **Reduce time range**: Use shorter intervals for real-time monitoring
- **Optimize queries**: Add rate intervals appropriate for your scrape frequency
- **Limit template variable selections**: Avoid selecting too many collectors/namespaces

## Metric Reference

### Core CRI Metrics

| Metric | Type | Description | Labels |
|--------|------|-------------|---------|
| `cri_events_processed_total` | Counter | Total container events processed | `collector`, `event_type` |
| `cri_events_dropped_total` | Counter | Events dropped due to buffer full | `collector`, `reason` |
| `cri_oom_kills_total` | Counter | OOM kills detected | `collector`, `source`, `container` |
| `cri_processing_latency` | Histogram | Processing latency in milliseconds | `operation` |
| `cri_buffer_usage_percent` | Gauge | Ring buffer usage percentage | `collector`, `socket` |
| `cri_containers_active` | UpDownCounter | Currently monitored containers | `collector` |
| `cri_errors_total` | Counter | CRI client errors | `operation`, `error` |
| `cri_batch_size` | Histogram | Event batch sizes | None |
| `cri_checks_performed_total` | Counter | Container state checks | None |

### eBPF-Specific Metrics (Linux only)

| Metric | Type | Description | Labels |
|--------|------|-------------|---------|
| `cri_ebpf_loads_total` | Counter | eBPF program load attempts | `collector_name` |
| `cri_ebpf_load_errors_total` | Counter | eBPF program load errors | `collector_name`, `error_type` |
| `cri_ebpf_attachments_total` | Counter | eBPF program attach attempts | `collector_name`, `program`, `type` |
| `cri_ebpf_attach_errors_total` | Counter | eBPF program attach errors | `collector_name`, `program`, `error_type` |
| `cri_ebpf_events_total` | Counter | Total eBPF events received | None |
| `cri_ebpf_events_dropped_total` | Counter | eBPF events dropped | `error_type` |
| `cri_ebpf_kernel_oom_kills_total` | Counter | Kernel OOM kills detected via eBPF | None |
| `cri_ebpf_map_update_errors_total` | Counter | eBPF map update errors | `map`, `operation` |

## Integration Examples

### Prometheus Recording Rules

```yaml
groups:
  - name: tapio_cri_collector
    rules:
      - record: tapio:cri_event_rate_5m
        expr: rate(cri_events_processed_total[5m])
      
      - record: tapio:cri_oom_rate_5m
        expr: rate(cri_oom_kills_total[5m])
        
      - record: tapio:cri_latency_p99_5m
        expr: histogram_quantile(0.99, rate(cri_processing_latency_bucket[5m]))
```

### Alert Manager Integration

```yaml
groups:
  - name: tapio_cri_alerts
    rules:
      - alert: CRIHighOOMKillRate
        expr: rate(cri_oom_kills_total[5m]) > 0.5
        for: 1m
        labels:
          severity: critical
          component: cri-collector
        annotations:
          summary: "High OOM kill rate detected"
          description: "CRI collector detected {{ $value }} OOM kills per second"
          
      - alert: CRIBufferUsageHigh
        expr: cri_buffer_usage_percent > 85
        for: 2m
        labels:
          severity: warning
          component: cri-collector
        annotations:
          summary: "CRI collector buffer usage high"
          description: "Buffer usage is {{ $value }}% on {{ $labels.collector }}"
```

## Support

For issues with the dashboard:

1. **Check the Tapio documentation**: [GitHub Repository](https://github.com/yairfalse/tapio)
2. **Verify OTEL configuration**: Ensure proper instrumentation setup
3. **Monitor collector logs**: Check for configuration or runtime issues
4. **Test metric availability**: Use Prometheus query interface directly

The dashboard is designed to provide comprehensive observability for production CRI collector deployments with minimal configuration required.