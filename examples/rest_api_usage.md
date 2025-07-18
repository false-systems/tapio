# Tapio REST API Usage Examples

This document demonstrates how to use the Tapio REST API with the correlation adapter.

## Base URL

```
http://localhost:8888
```

## Health Check Endpoints

### Check API Health
```bash
curl http://localhost:8888/health
```

### Check API Readiness
```bash
curl http://localhost:8888/ready
```

## Resource Insights

### Get Insights for a Specific Resource
```bash
curl http://localhost:8888/api/v1/insights/production/api-deployment
```

Response:
```json
{
  "resource": "api-deployment",
  "namespace": "production",
  "insights": [
    {
      "id": "insight-001",
      "title": "High Memory Usage Detected",
      "description": "Memory usage trending towards OOM condition",
      "severity": "high",
      "category": "resource",
      "resource": "api-deployment",
      "namespace": "production",
      "timestamp": "2024-01-15T10:30:00Z",
      "prediction": {
        "type": "oom",
        "time_to_event": "30m",
        "probability": 0.85,
        "confidence": 0.92
      },
      "actionable_items": [
        {
          "description": "Increase memory limits",
          "command": "kubectl patch deployment api-deployment -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"api\",\"resources\":{\"limits\":{\"memory\":\"2Gi\"}}}]}}}}'",
          "impact": "Prevents OOM kills",
          "risk": "low"
        }
      ]
    }
  ],
  "count": 1,
  "timestamp": "2024-01-15T10:35:00Z"
}
```

### List All Insights
```bash
curl "http://localhost:8888/api/v1/insights?severity=high&category=resource&limit=50"
```

## Predictions

### Get Predictions for a Resource
```bash
curl http://localhost:8888/api/v1/predictions/production/api-deployment
```

Response:
```json
{
  "resource": "api-deployment",
  "namespace": "production",
  "predictions": [
    {
      "type": "oom",
      "time_to_event": "30m",
      "probability": 0.85,
      "confidence": 0.92
    }
  ],
  "count": 1,
  "timestamp": "2024-01-15T10:35:00Z"
}
```

## Actionable Items (Fixes)

### Get Available Fixes
```bash
curl http://localhost:8888/api/v1/fixes/production/api-deployment
```

Response:
```json
{
  "resource": "api-deployment",
  "namespace": "production",
  "fixes": [
    {
      "id": "fix-0",
      "description": "Increase memory limits",
      "command": "kubectl patch deployment api-deployment -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"api\",\"resources\":{\"limits\":{\"memory\":\"2Gi\"}}}]}}}}'",
      "impact": "Prevents OOM kills",
      "risk": "low",
      "auto_fixable": true
    }
  ],
  "count": 1,
  "timestamp": "2024-01-15T10:35:00Z"
}
```

### Apply a Fix (Dry Run)
```bash
curl -X POST \
  http://localhost:8888/api/v1/fixes/production/api-deployment/fix-0/apply \
  -H "Content-Type: application/json" \
  -d '{
    "dry_run": true,
    "force": false
  }'
```

## Event Processing

### Process a Single Event
```bash
curl -X POST \
  http://localhost:8888/api/v1/events \
  -H "Content-Type: application/json" \
  -d '{
    "type": "memory_warning",
    "severity": "warning",
    "source": "kubelet",
    "message": "Container api is using 85% of memory limit",
    "entity": {
      "type": "pod",
      "name": "api-deployment-7d5c4-xyz",
      "namespace": "production"
    },
    "metadata": {
      "memory_used": "1.7Gi",
      "memory_limit": "2Gi",
      "memory_percent": 85
    }
  }'
```

Response:
```json
{
  "status": "accepted",
  "event_id": "event-1705316100000000000",
  "message": "Event processed successfully"
}
```

## Correlation

### Correlate Multiple Events
```bash
curl -X POST \
  http://localhost:8888/api/v1/correlate \
  -H "Content-Type: application/json" \
  -d '{
    "events": [
      {
        "id": "event-001",
        "type": "memory_warning",
        "severity": "warning",
        "source": "kubelet",
        "message": "High memory usage",
        "timestamp": "2024-01-15T10:00:00Z"
      },
      {
        "id": "event-002",
        "type": "pod_restart",
        "severity": "error",
        "source": "kubelet",
        "message": "Pod restarted due to OOM",
        "timestamp": "2024-01-15T10:05:00Z"
      }
    ]
  }'
```

## Patterns

### List Available Patterns
```bash
curl http://localhost:8888/api/v1/patterns
```

Response:
```json
{
  "patterns": [
    {
      "id": "pattern-oom-cascade",
      "name": "OOM Cascade Pattern",
      "description": "Memory pressure causing cascading failures",
      "type": "resource",
      "enabled": true,
      "metadata": {
        "category": "memory"
      }
    }
  ],
  "count": 1
}
```

### Get Pattern Matches
```bash
curl http://localhost:8888/api/v1/patterns/pattern-oom-cascade/matches
```

## Statistics

### Get Correlation Engine Statistics
```bash
curl http://localhost:8888/api/v1/stats
```

Response:
```json
{
  "enabled": true,
  "events_processed": 165234,
  "insights_generated": 432,
  "predictions_generated": 87,
  "correlations_found": 1243,
  "last_processed_at": "2024-01-15T10:34:55Z",
  "timestamp": "2024-01-15T10:35:00Z"
}
```

## Admin Endpoints

### Get Adapter Status
```bash
curl http://localhost:8888/admin/status
```

### Enable Correlation
```bash
curl -X POST http://localhost:8888/admin/correlation/enable
```

### Disable Correlation
```bash
curl -X POST http://localhost:8888/admin/correlation/disable
```

## CLI Integration Examples

### Using with Tapio CLI

```bash
# Get insights for a deployment
tapio insights get production/api-deployment

# List all high severity insights
tapio insights list --severity=high

# Get predictions
tapio predictions get production/api-deployment

# Apply a fix (dry run)
tapio fixes apply production/api-deployment fix-0 --dry-run

# Check cluster health
tapio health cluster

# View correlation statistics
tapio stats correlation
```

## Error Handling

All endpoints return appropriate HTTP status codes:

- `200 OK` - Successful request
- `202 Accepted` - Request accepted for processing
- `400 Bad Request` - Invalid request parameters
- `401 Unauthorized` - Missing or invalid authentication
- `404 Not Found` - Resource not found
- `500 Internal Server Error` - Server error

Error responses include a JSON body:
```json
{
  "error": "Error message describing what went wrong"
}
```

## Rate Limiting

The API enforces rate limiting (default: 1000 requests per minute). When rate limit is exceeded, the API returns:
- Status Code: `429 Too Many Requests`
- Header: `X-RateLimit-Remaining: 0`
- Header: `X-RateLimit-Reset: 1705316400`

## Authentication

For admin endpoints when authentication is enabled, include the Authorization header:
```bash
curl -H "Authorization: Bearer <token>" http://localhost:8888/admin/status
```