# Tapio API Map

Complete reference for all Tapio API endpoints, organized by functionality.

## 🏗️ **API Architecture Overview**

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   CLI Commands  │────▶│   REST API       │────▶│  Correlation    │
│ check/why/fix   │     │  Port: 8888      │     │    Engine       │
└─────────────────┘     └──────────────────┘     └─────────────────┘
                                 │
                                 ▼
                        ┌──────────────────┐
                        │  eBPF/K8s/L7     │
                        │   Collectors     │
                        └──────────────────┘
```

## 📡 **Base URL**
```
http://localhost:8888/api/v1
```

---

## 🔍 **Insights & Analysis**

### **GET** `/insights/{namespace}/{resource}`
Get correlation insights for a specific resource.

**Parameters:**
- `namespace` (path): Kubernetes namespace
- `resource` (path): Resource name (pod, deployment, service)
- `severity` (query): Filter by severity (`critical`, `high`, `medium`, `low`)
- `category` (query): Filter by category (`memory`, `network`, `storage`, etc.)
- `limit` (query): Maximum results (default: 50)

**Response:**
```json
{
  "resource": "api-service",
  "namespace": "default",
  "insights": [
    {
      "id": "insight-abc123",
      "title": "OOM Kill Predicted in 7 minutes",
      "description": "Memory usage growing at 2.4MB/s, will exceed limit soon",
      "severity": "critical",
      "category": "memory",
      "timestamp": "2024-01-15T10:30:00Z",
      "prediction": {
        "type": "oom",
        "time_to_event": "420s",
        "probability": 0.92,
        "confidence": 0.85
      },
      "actionable_items": [
        {
          "id": "fix-1",
          "description": "Increase memory limit to prevent OOM",
          "command": "kubectl patch deployment api-service ...",
          "impact": "Prevents pod restart and service disruption",
          "risk": "low",
          "auto_fixable": true
        }
      ]
    }
  ],
  "count": 1,
  "timestamp": "2024-01-15T10:35:00Z"
}
```

**CLI Usage:**
```bash
tapio check api-service  # Uses this endpoint
```

---

### **GET** `/insights`
List all insights across the cluster.

**Parameters:**
- `namespace` (query): Filter by namespace
- `severity` (query): Filter by severity
- `limit` (query): Maximum results (default: 100)
- `offset` (query): Pagination offset

**Response:**
```json
{
  "insights": [...],
  "total": 45,
  "count": 20,
  "offset": 0,
  "timestamp": "2024-01-15T10:35:00Z"
}
```

---

## 🔮 **Predictions**

### **GET** `/predictions/{namespace}/{resource}`
Get predictions for a specific resource.

**Response:**
```json
{
  "resource": "api-service",
  "namespace": "default",
  "predictions": [
    {
      "id": "pred-xyz789",
      "type": "oom",
      "title": "Pod will OOM in 7 minutes",
      "time_to_event": "420s",
      "probability": 0.92,
      "confidence": 0.85,
      "severity": "critical",
      "created_at": "2024-01-15T10:30:00Z"
    },
    {
      "id": "pred-abc456",
      "type": "crash",
      "title": "Container restart likely",
      "probability": 0.78,
      "confidence": 0.80,
      "severity": "high",
      "created_at": "2024-01-15T10:25:00Z"
    }
  ],
  "count": 2
}
```

---

### **GET** `/predictions`
List all active predictions.

**Parameters:**
- `namespace` (query): Filter by namespace
- `type` (query): Filter by prediction type (`oom`, `crash`, `network_failure`)
- `severity` (query): Filter by severity
- `time_window` (query): Time window (`1h`, `6h`, `24h`)

---

## 🔧 **Fixes & Remediation**

### **GET** `/fixes/{namespace}/{resource}`
Get actionable fixes for a resource.

**Parameters:**
- `auto_fix_only` (query): Only return auto-fixable items (`true`/`false`)

**Response:**
```json
{
  "resource": "api-service",
  "namespace": "default", 
  "fixes": [
    {
      "id": "fix-mem-limit-1",
      "insight_id": "insight-abc123",
      "description": "Increase memory limit to prevent OOM",
      "command": "kubectl patch deployment api-service -p '{\"spec\":{\"template\":{\"spec\":{\"containers\":[{\"name\":\"api\",\"resources\":{\"limits\":{\"memory\":\"2Gi\"}}}]}}}}'",
      "impact": "Prevents pod restart and service disruption",
      "risk": "low",
      "auto_fixable": true
    },
    {
      "id": "fix-restart-1", 
      "insight_id": "insight-def456",
      "description": "Restart pod to clear memory leak",
      "command": "kubectl delete pod api-service-7d5c4-xyz",
      "impact": "Temporary service interruption during restart",
      "risk": "medium",
      "auto_fixable": false
    }
  ],
  "count": 2
}
```

**CLI Usage:**
```bash
tapio fix api-service  # Uses this endpoint
```

---

### **POST** `/fixes/{namespace}/{resource}/{fixId}/apply`
Apply a specific fix.

**Request Body:**
```json
{
  "confirm": true,
  "dry_run": false,
  "timeout": "300s"
}
```

**Response:**
```json
{
  "fix_id": "fix-mem-limit-1",
  "status": "applied",
  "execution_time": "2.3s",
  "result": {
    "command": "kubectl patch deployment api-service ...",
    "output": "deployment.apps/api-service patched",
    "exit_code": 0
  },
  "timestamp": "2024-01-15T10:36:00Z"
}
```

---

## 💚 **Health & Status**

### **GET** `/health/{namespace}/{resource}`
Get detailed health status for a resource.

**Response:**
```json
{
  "resource": "api-service",
  "namespace": "default",
  "status": "unhealthy",
  "score": 0.65,
  "critical_issues": 2,
  "warnings": 1,
  "last_checked": "2024-01-15T10:35:00Z",
  "components": {
    "memory": "critical",
    "cpu": "healthy", 
    "network": "degraded",
    "storage": "healthy"
  },
  "metrics": {
    "memory_usage_percent": 87.3,
    "cpu_usage_percent": 45.2,
    "restart_count": 3,
    "error_rate": 0.02
  }
}
```

**CLI Usage:**
```bash
tapio check api-service  # Uses this endpoint
```

---

### **GET** `/health/cluster`
Get overall cluster health.

**Response:**
```json
{
  "status": "degraded",
  "score": 0.82,
  "total_nodes": 5,
  "healthy_nodes": 4,
  "total_pods": 127,
  "unhealthy_pods": 3,
  "critical_issues": 2,
  "warnings": 8,
  "namespaces": {
    "default": "healthy",
    "kube-system": "healthy",
    "production": "degraded"
  },
  "top_issues": [
    "Memory pressure on production/api-service",
    "High error rate in production/payment-service"
  ]
}
```

---

## 🌊 **Network Flows (L7 Visibility)**

### **GET** `/flows`
List network flows with L7 protocol details.

**Parameters:**
- `limit` (query): Maximum results (default: 100)
- `protocol` (query): Filter by protocol (`tcp`, `udp`, `http`, `grpc`, `kafka`)
- `direction` (query): Filter by direction (`ingress`, `egress`)
- `namespace` (query): Filter by namespace
- `pod` (query): Filter by pod name
- `service` (query): Filter by service name
- `time_window` (query): Time window (`5m`, `1h`, `6h`)

**Response:**
```json
{
  "flows": [
    {
      "id": "flow-abc123",
      "source": "frontend-7d5c4-xyz",
      "source_namespace": "default",
      "destination": "api-service-8f9d2-abc",
      "destination_namespace": "default",
      "protocol": "http",
      "direction": "egress",
      "l7_data": {
        "method": "POST",
        "path": "/api/users",
        "status_code": 200,
        "latency_ms": 45,
        "user_agent": "frontend-client/1.0"
      },
      "bytes_in": 1024,
      "bytes_out": 2048,
      "timestamp": "2024-01-15T10:35:00Z",
      "anomalies": ["high_latency"],
      "tags": ["api_call", "method:post"]
    }
  ],
  "count": 1,
  "filters": {
    "protocol": "http",
    "namespace": "default"
  }
}
```

---

### **GET** `/flows/{namespace}/{pod}`
Get flows for a specific pod.

**Response:**
```json
{
  "pod": "api-service-7d5c4-xyz",
  "namespace": "default",
  "flows": [...],
  "summary": {
    "total_flows": 1247,
    "ingress_flows": 856,
    "egress_flows": 391,
    "protocols": {
      "http": 1102,
      "grpc": 89,
      "tcp": 56
    },
    "top_destinations": [
      "database-service:5432",
      "redis-service:6379"
    ],
    "error_rate": 0.02
  }
}
```

---

### **GET** `/flows/l7/{protocol}`
Get L7 protocol-specific flows.

**Protocols:** `http`, `grpc`, `kafka`

#### **HTTP Flows** - `/flows/l7/http`
```json
{
  "protocol": "http",
  "flows": [
    {
      "method": "GET",
      "path": "/api/health",
      "status_code": 200,
      "latency_ms": 2,
      "user_agent": "kube-probe/1.28",
      "requests": 1523,
      "errors": 0,
      "p95_latency_ms": 5,
      "anomalies": [],
      "tags": ["health_check"]
    },
    {
      "method": "POST", 
      "path": "/api/users",
      "status_code": 201,
      "latency_ms": 87,
      "requests": 234,
      "errors": 5,
      "p95_latency_ms": 150,
      "anomalies": ["high_latency"],
      "tags": ["api_call", "crud"]
    }
  ]
}
```

#### **gRPC Flows** - `/flows/l7/grpc`
```json
{
  "protocol": "grpc",
  "flows": [
    {
      "service": "correlation.EventCollector",
      "method": "StreamEvents",
      "status": "OK",
      "stream_type": "bidi_stream",
      "streams": 42,
      "messages": 165000,
      "errors": 0,
      "p95_latency_ms": 500,
      "anomalies": [],
      "tags": ["streaming", "events"]
    },
    {
      "service": "user.UserService",
      "method": "GetUser",
      "status": "NOT_FOUND", 
      "stream_type": "unary",
      "requests": 156,
      "errors": 23,
      "p95_latency_ms": 25,
      "anomalies": ["high_error_rate"],
      "tags": ["crud", "error"]
    }
  ]
}
```

#### **Kafka Flows** - `/flows/l7/kafka`
```json
{
  "protocol": "kafka",
  "flows": [
    {
      "operation": "produce",
      "topic": "events",
      "partition": 0,
      "producer": "collector-abc",
      "messages": 85000,
      "throughput_mb_s": 10.2,
      "latency_ms": 5,
      "errors": 0,
      "anomalies": [],
      "tags": ["high_throughput"]
    },
    {
      "operation": "fetch",
      "topic": "notifications", 
      "consumer": "notification-service",
      "consumer_group": "notifications",
      "messages": 12000,
      "lag": 120,
      "latency_ms": 15,
      "anomalies": ["consumer_lag"],
      "tags": ["consumer", "lag"]
    }
  ]
}
```

---

## 🔄 **Real-time Updates (WebSocket)**

### **WS** `/ws/insights`
WebSocket for real-time insight updates.

**Connection:**
```javascript
const ws = new WebSocket('ws://localhost:8888/api/v1/ws/insights');

ws.onmessage = (event) => {
  const insight = JSON.parse(event.data);
  console.log('New insight:', insight.title);
};
```

**Message Format:**
```json
{
  "type": "insight",
  "action": "created",  // created, updated, resolved
  "data": {
    "id": "insight-abc123",
    "title": "OOM Kill Predicted in 7 minutes",
    "severity": "critical",
    "resource": "api-service",
    "namespace": "default"
  },
  "timestamp": "2024-01-15T10:35:00Z"
}
```

---

### **WS** `/ws/flows`
WebSocket for real-time flow updates.

---

## 🔍 **Correlation & Analysis**

### **POST** `/correlate`
Correlate specific events to find relationships.

**Request:**
```json
{
  "events": [
    {"type": "memory", "pod": "api-service", "timestamp": "2024-01-15T10:30:00Z"},
    {"type": "error", "pod": "api-service", "timestamp": "2024-01-15T10:31:00Z"}
  ],
  "time_window": "5m",
  "correlation_types": ["temporal", "semantic", "causal"]
}
```

**Response:**
```json
{
  "correlation_id": "corr-xyz789",
  "relationships": [
    {
      "type": "causal",
      "confidence": 0.89,
      "description": "Memory pressure caused application errors",
      "events": ["memory-event-1", "error-event-2"],
      "timeline": [...]
    }
  ]
}
```

---

### **GET** `/patterns`
List available correlation patterns.

**Response:**
```json
{
  "patterns": [
    {
      "id": "memory_leak_oom_cascade",
      "name": "Memory Leak OOM Cascade",
      "description": "Detects memory leaks leading to OOM kills",
      "confidence_threshold": 0.85,
      "enabled": true
    },
    {
      "id": "network_failure_cascade", 
      "name": "Network Failure Cascade",
      "description": "Detects network failures propagating through services",
      "confidence_threshold": 0.80,
      "enabled": true
    }
  ]
}
```

---

### **GET** `/patterns/{patternId}/matches`
Get recent matches for a specific pattern.

---

## 🚨 **System Endpoints**

### **GET** `/health`
Basic health check.

**Response:**
```json
{
  "status": "healthy",
  "time": "2024-01-15T10:35:00Z"
}
```

---

### **GET** `/ready`
Readiness check.

**Response:**
```json
{
  "status": "ready", 
  "time": "2024-01-15T10:35:00Z",
  "components": {
    "correlation_engine": "ready",
    "event_collectors": "ready",
    "database": "ready"
  }
}
```

---

## 🔒 **Admin Endpoints**

*Require authentication when `auth_enabled: true`*

### **GET** `/admin/metrics`
Internal metrics and performance data.

### **POST** `/admin/correlation/retrain`
Retrain correlation models.

### **DELETE** `/admin/cache`
Clear all caches.

---

## 📊 **Response Codes**

| Code | Meaning | Description |
|------|---------|-------------|
| 200 | OK | Request successful |
| 201 | Created | Resource created (fixes applied) |
| 400 | Bad Request | Invalid parameters |
| 401 | Unauthorized | Authentication required |
| 404 | Not Found | Resource not found |
| 429 | Too Many Requests | Rate limit exceeded |
| 500 | Internal Server Error | Server error |
| 503 | Service Unavailable | Correlation engine unavailable |

---

## 🔧 **Configuration**

```yaml
# API Server Configuration
api:
  port: "8888"
  enable_cors: true
  rate_limit_per_min: 1000
  auth_enabled: false
  metrics_enabled: true
  cache_timeout: "30s"

# L7 Protocol Parsing
l7:
  enable_http: true
  enable_grpc: true  
  enable_kafka: true
  max_http_body_size: 65536
  parse_http_body: false  # Privacy
  flow_timeout: "5m"
```

---

## 🎯 **CLI Integration**

| CLI Command | API Endpoint | Description |
|-------------|--------------|-------------|
| `tapio check <resource>` | `GET /insights/{ns}/{resource}` | Get insights and predictions |
| `tapio why <resource>` | `GET /insights/{ns}/{resource}` | Get detailed analysis |
| `tapio fix <resource>` | `GET /fixes/{ns}/{resource}` | Get actionable fixes |
| `tapio fix <resource> --apply` | `POST /fixes/{ns}/{resource}/{id}/apply` | Apply specific fix |

This API provides complete programmatic access to all Tapio capabilities, from basic health checking to deep L7 protocol analysis!