# Container Observer Design

## Problem
Missing container runtime failure tracking for ALL container types (init, main, sidecar, ephemeral):
- CPU throttling (invisible to cAdvisor - 50% of "slow container" complaints)
- OOMKills (memory limit exceeded)
- Container crashes (non-zero exit codes)
- Image pull failures (ErrImagePull, ImagePullBackOff)

## Solution
K8s Informer watching Pods → Detect container failures → Emit domain events

## Data Sources
1. K8s API - Container status via Pod.Status.ContainerStatuses[]
2. cgroups v2 - CPU throttling (Phase 2)
3. /proc/<pid>/status - Context switches (Phase 2)
4. eBPF - Scheduler latency (Phase 3)

## Events
- `container_oom_killed` - Exit code 137
- `container_crashed` - Non-zero exit code
- `container_image_pull_failed` - ErrImagePull/ImagePullBackOff
- `container_throttled` - CPU throttling (Phase 2)

## Implementation Phases

### Phase 1: Basic Container Tracking (Current)
**TDD Cycles**:
1. Detect OOMKill (exit code 137, reason "OOMKilled")
2. Detect crashes (exit code > 0)
3. Detect image pull failures (reason "ErrImagePull"/"ImagePullBackOff")
4. Track init containers (separate from main containers)
5. Emit domain events

**Tests**: 15 unit tests
**Coverage**: > 80%

### Phase 2: CPU Throttling (Future)
Parse cgroups v2, implement Gregg's differential diagnosis

### Phase 3: eBPF Integration (Future)
Scheduler latency tracking

## Architecture Compliance
- Level 1: `internal/observers/container/`
- Depends only on `pkg/domain/`
- No map[string]interface{}
- No TODOs or stubs
