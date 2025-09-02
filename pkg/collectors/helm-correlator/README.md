# Helm Correlator

## Overview

The helm-correlator is a hybrid eBPF + Kubernetes collector that correlates Helm operation failures with their root causes. It tracks Helm processes, Kubernetes state changes, and correlates them to identify specific failure patterns.

## Architecture

### Data Collection

```yaml
Layer 1: eBPF (Process Tracking)
  - Tracks helm/kubectl execution
  - Captures command arguments
  - Monitors file access (values.yaml, templates)
  - Times each operation phase
  - Tracks API calls to Kubernetes

Layer 2: Kubernetes (State Tracking)
  - Watches Helm release secrets
  - Monitors K8s events
  - Tracks pod/job status
  - Detects resource conflicts

Layer 3: Correlation Engine
  - Matches eBPF operations to K8s changes
  - Applies pattern detection
  - Builds failure timeline
  - Generates root cause with resolution
```

## Failure Patterns

The correlation engine detects the following patterns:

1. **Hook Failures**: Image pull issues, job timeouts, script errors
2. **Stuck Releases**: PENDING-UPGRADE, PENDING-ROLLBACK states
3. **Template Errors**: Syntax errors, missing values
4. **Resource Conflicts**: Already exists, immutable field changes
5. **Partial Deployments**: Some resources succeed, others fail
6. **Wait Timeouts**: Pods not becoming ready
7. **CRD Mismatches**: Required CRDs missing or wrong version

## Event Format

```json
{
  "type": "operational",
  "severity": "error",
  "source": "helm-correlator",
  "pattern": "Hook Failed - Image Pull",
  "confidence": 0.95,
  "release": {
    "name": "backend-worker1",
    "namespace": "production",
    "operation": "upgrade",
    "from_version": 2,
    "to_version": 3,
    "status": "failed"
  },
  "root_cause": {
    "summary": "Pre-upgrade hook failed: Cannot pull image",
    "details": "Container 'migrate' failed to pull image from ECR",
    "evidence": [
      "Hook job 'backend-worker1-migrate-v3' created at 10:01:06",
      "Pod in ImagePullBackOff state",
      "ECR rate limit: 429 Too Many Requests",
      "Container has restarted 3 times"
    ],
    "impact": "Helm upgrade blocked - hook job cannot start",
    "resolution": "Wait for rate limit reset or use different registry"
  },
  "correlation": {
    "operation_id": "helm-12345-1234567890",
    "confidence": 0.95,
    "event_chain": [
      "Helm upgrade started",
      "Release status: pending-upgrade",
      "Job backend-worker1-migrate-v3 failed",
      "Pod backend-worker1-migrate-v3-xyz failed"
    ]
  }
}
```

## Configuration

```yaml
helm-correlator:
  enabled: true
  buffer_size: 1000
  
  # Feature flags
  enable_ebpf: true           # Track helm/kubectl processes
  enable_k8s_watching: true   # Watch secrets/events/pods
  
  # Correlation settings
  correlation_window: 5m      # Time window for correlation
  stuck_release_timeout: 10m  # When to consider release stuck
  hook_timeout: 5m           # Max time for hooks
  
  # Kubernetes settings
  namespaces: []             # Empty = all namespaces
  
  # eBPF settings (Linux only)
  track_kubectl: true        # Also track kubectl commands
  track_files: true          # Track file access
  track_api: true           # Track API calls
```

## Usage

Import the collector in your main.go:

```go
import _ "github.com/yairfalse/tapio/pkg/collectors/helm-correlator"
```

## How It Works

### 1. Operation Tracking (eBPF)
```
User runs: helm upgrade api ./chart
    ↓
eBPF captures:
  - Process start (PID 12345)
  - Command arguments (--timeout 10m)
  - Files read (values-prod.yaml)
  - API calls to K8s
  - Process exit (code 1)
```

### 2. State Observation (K8s)
```
Kubernetes activity:
  - Secret created (sh.helm.release.v1.api.v3)
  - Status: PENDING-UPGRADE
  - Job created (api-migrate-v3)
  - Pod failed (ImagePullBackOff)
  - Event: "Failed to pull image"
```

### 3. Correlation & Diagnosis
```
Correlation Engine:
  - Matches eBPF operation to K8s changes
  - Identifies pattern: "Hook Image Pull Failure"
  - Builds evidence chain
  - Generates resolution steps
```

## Requirements

- **Linux kernel 4.14+** for eBPF support
- **CAP_SYS_ADMIN** capability for eBPF programs
- **Kubernetes RBAC** permissions to read secrets, pods, events, jobs
- Falls back to K8s-only mode on non-Linux platforms

## Examples

### Example 1: NetworkPolicy Blocking Hook

```yaml
Pattern: Resource Conflict
Release: frontend (v5 → v6)
Problem: Pre-install hook blocked by NetworkPolicy

Evidence:
  - Hook job created in namespace 'production'
  - Pod cannot reach database on port 5432
  - NetworkPolicy 'deny-all' blocks egress

Resolution:
  1. Temporarily allow hook traffic:
     kubectl label pod <hook-pod> allow-db=true
  2. Or update NetworkPolicy to allow hooks
  3. Or run with --no-hooks if safe
```

### Example 2: Stuck Release from Crashed Operation

```yaml
Pattern: Stuck Release
Release: api-server
Status: PENDING-UPGRADE (45 minutes)

Problem: Previous upgrade killed, release locked

Evidence:
  - Previous helm process exited with SIGKILL
  - Release stuck since 10:15 AM
  - No active operations running

Resolution:
  helm rollback api-server
  # Or if rollback fails:
  kubectl delete secret sh.helm.release.v1.api-server.v17
```

