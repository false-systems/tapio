# Tapio - Kubernetes Intelligence Platform

**Named after Tapio, Finnish forest god** - because debugging Kubernetes clusters shouldn't require divine intervention.

---

## 🎯 Mission Statement

**Make Kubernetes and eBPF accessible to ANYONE.**

We are building the tool that transforms complex kernel-level debugging into simple, human-readable insights. A junior developer should be able to install Tapio and immediately become a Kubernetes expert.

---

## 🏗️ Core Architecture Principles

### Dual-Input Intelligence Philosophy

- **Kubernetes API**: What the cluster *thinks* is happening
- **eBPF Kernel Data**: What's *actually* happening at the kernel level
- **Correlation Engine**: Finds dangerous mismatches and predicts failures

### Zero-Dashboard Approach

- **NO dashboards** - information comes to YOU
- **NO configuration** - works out of the box like kubectl
- **NO extra data** - only what matters for debugging
- **NO technical jargon** - human language only

### Accessibility First

- Junior dev installs → 5 minutes later fixes K8s problems
- No eBPF knowledge needed → tool handles all complexity
- No K8s expertise required → plain English explanations

---

## 📁 Project Structure

```
tapio/
├── cmd/tapio/                    # CLI entry point
├── pkg/
│   ├── k8s/                     # Kubernetes client & API integration
│   ├── ebpf/                    # eBPF collectors & kernel monitoring
│   ├── simple/                  # Basic health checker (current)
│   ├── health/                  # Health analysis engine
│   ├── correlation/             # Multi-source intelligence engine
│   ├── types/                   # Shared data structures
│   ├── metrics/                 # Prometheus integration
│   └── output/                  # Human-readable formatters
├── internal/
│   ├── cli/                     # Cobra command implementations
│   └── output/                  # Internal output formatting
├── deploy/
│   └── helm/tapio/             # Helm charts for cluster deployment
├── scripts/                     # Development and agent management
├── .github/workflows/           # CI/CD pipelines
├── Makefile                     # Build and quality gates
└── Taskfile.yml               # Task automation
```

---

## 🚀 Current Implementation Status

### ✅ COMPLETED

- **Basic CLI framework** with Cobra + Viper
- **Simple health checker** (`tapio check`)
- **Kubernetes API integration**
- **CI/CD pipeline** with quality gates
- **Agent workflow system** for organized development
- **Comprehensive Makefile** with linting and testing

### 🔄 IN PROGRESS

- **eBPF system collector** (high-performance kernel monitoring)
- **Advanced correlation engine** (multi-source intelligence)

### 📋 PLANNED

- **Complete system monitoring** (systemd + journald integration)
- **Auto-fix capabilities** (`tapio fix`)
- **OTEL integration** for enterprise observability

---

## 🎮 Command Interface

### Dead Simple Commands

```bash
tapio check           # "Is stuff broken?"
tapio fix            # "Fix it now" (future)
tapio why            # "Explain like I'm 5" (future)
tapio watch          # "Tell me when things break" (future)
```

### Current Capabilities

```bash
tapio check                    # Current namespace health
tapio check my-app             # Specific deployment
tapio check pod/my-pod-xyz     # Specific pod
tapio check --all              # Entire cluster
tapio check --output json      # Machine-readable output
```

### Expected Output Style

```
ANALYSIS: my-app has issues

pod/api-service-xyz: High restart count
  Container restarted 8 times in last hour
  Pattern: Consistent OOMKilled events
  
  Likely cause: Memory limit (256Mi) too low
  
  Next steps:
  [1] kubectl logs api-service-xyz --previous
  [2] kubectl top pod api-service-xyz
  
  Suggested fix:
  kubectl patch deployment my-app -p '{"spec":{"template":{"spec":{"containers":[{"name":"api","resources":{"limits":{"memory":"512Mi"}}}]}}}}'
```

---

## 🔧 Development Standards

### Quality Gates (MANDATORY)

- **Formatting**: `make fmt` - gofmt + goimports
- **Linting**: `make lint` - golangci-lint with 25+ rules
- **Testing**: `make test` - minimum 70% coverage
- **Security**: `make security` - gosec vulnerability scanning
- **Build**: `make ci` - complete pipeline validation

### Testing Strategy

1. **Unit Tests**: Fast, isolated component testing
2. **Integration Tests**: Multi-component interaction
3. **System Tests**: Full application testing
4. **E2E Tests**: Real cluster scenarios with Kind

### Code Quality Requirements

- **Error Handling**: Every function properly handles errors
- **Documentation**: GoDoc for all public functions
- **Performance**: Built-in monitoring and benchmarking
- **Concurrency**: Thread-safe patterns, proper context usage

---

## 🌊 Branch Management & Agent Workflow

### Agent Task System

```bash
# Start new work
make agent-start
# Agent ID: agent-1
# Component: correlation
# Action: enhancement
# → Creates feature/agent-1/correlation-enhancement

# Check work status
make agent-status

# Prepare for PR
make pr-ready
```

### Branch Strategy

- `main` - Production-ready code
- `develop` - Integration branch
- `feature/agent-[id]/[component]-[action]` - Agent work branches

### Commit Standards

- Format: `feat(component): description` or `fix(component): description`
- Small PRs: < 200 lines per PR
- Quality gates: All checks must pass before merge

---

## ⚡ Performance Requirements

### High-Performance Architecture

- **Event Throughput**: 165,000 events/sec per node → 5,000 relevant/sec (97% filtering)
- **Processing Latency**: <500µs per event
- **Memory Usage**: <100MB per node for eBPF buffers
- **CPU Overhead**: <1% system impact
- **Response Time**: `tapio check` in <2 seconds

### Optimization Strategies

- **Smart Caching**: K8s API data cached for 30 seconds
- **Batch Processing**: eBPF events in 1-second windows
- **Object Pooling**: Zero-GC pressure design
- **Lock-Free Data Structures**: Maximum throughput
- **Load Shedding**: Progressive event dropping under load

---

## 🏭 Deployment Architecture

### Single Deployment with Modular Components

```yaml
# DaemonSet on each node
tapio-agent:
  - eBPF collectors
  - systemd monitoring
  - journald analysis
  - correlation engine
  
# Cluster-level service
tapio-server:
  - Central intelligence
  - Prometheus metrics
  - API endpoints
```

### Installation Methods

```bash
# CLI installation
curl -sSL https://install.tapio.sh | sh

# Helm deployment
helm install tapio deploy/helm/tapio

# kubectl deployment
kubectl apply -f deploy/kubernetes/
```

---

## 🔍 Key Implementation Guidelines

### eBPF Development

- Use `cilium/ebpf` framework for all eBPF programs
- Build tags: `//go:build ebpf` for eBPF-specific code
- Privilege checking: Graceful fallback if eBPF unavailable
- Performance focus: Kernel filtering for 97% event reduction

### Kubernetes Integration

- Use official `k8s.io/client-go` library
- Support multiple kubeconfig sources
- Graceful degradation if cluster access limited
- Context-aware operations (current namespace)

### Output Formatting

- **Human-first**: Clear, actionable explanations
- **Context-aware**: Show relevant information only
- **Color-coded**: Green (healthy), Yellow (warning), Red (critical)
- **No emojis**: Professional terminal output

### Configuration Management

- **Zero-config default**: Works like kubectl out of the box
- **Hierarchical**: CLI flags → env vars → config file → defaults
- **Environment variables**: `TAPIO_*` prefix
- **Kubernetes-native**: YAML configuration

---

## 🧪 Testing Philosophy

### Test Everything

- **Unit tests**: Every function with edge cases
- **Integration tests**: Component interactions
- **E2E tests**: Real cluster scenarios
- **Performance tests**: Benchmarks and race detection

### Quality Standards

- **Coverage**: Minimum 70% test coverage
- **Race detection**: All tests run with `-race`
- **Table-driven tests**: Consistent patterns
- **Mock interfaces**: Clean test isolation

---

## 🎯 Implementation Priorities

### Phase 1: Foundation (DONE)

- ✅ CLI framework
- ✅ Basic health checking
- ✅ CI/CD pipeline
- ✅ Quality gates

### Phase 2: Intelligence (IN PROGRESS)

- 🔄 eBPF system collector
- 🔄 Correlation engine
- 📋 Multi-source intelligence

### Phase 3: Auto-Healing (PLANNED)

- 📋 `tapio fix` command
- 📋 Automated remediation
- 📋 Predictive capabilities

### Phase 4: Enterprise (PLANNED)

- 📋 OTEL integration
- 📋 Advanced monitoring
- 📋 Federation support

---

## 💡 Success Criteria

### For Every Task

- [ ] Code follows all quality standards
- [ ] Tests pass with >70% coverage
- [ ] `make pr-ready` succeeds
- [ ] Human-readable output
- [ ] Zero-config operation

### For the Project

- A junior developer can install Tapio and immediately solve K8s problems
- The tool explains complex issues in simple, actionable terms
- Performance impact is minimal (<1% CPU, <100MB memory)
- Works reliably across different K8s distributions

---

## 🚨 Important Notes

### What We DON'T Do

- ❌ Build dashboards (information comes to users)
- ❌ Require configuration (zero-config philosophy)
- ❌ Use technical jargon (human language only)
- ❌ Create data overload (show only what matters)

### What We DO Focus On

- ✅ Accessibility for everyone
- ✅ Actionable insights
- ✅ Production reliability
- ✅ Performance efficiency

---

## 🔄 Continuous Improvement

### Code Quality

- Pre-commit hooks enforce standards
- CI pipeline catches issues early
- Regular dependency updates
- Security scanning on every commit

### Performance Monitoring

- Built-in metrics collection
- Benchmark tracking
- Memory profiling
- Performance regression prevention

---

## 🔬 Engineering Philosophy: No Patches, Only Solutions

### Root Cause Analysis (MANDATORY)

When you encounter ANY problem:

1. **STOP** - Don't immediately patch or workaround
2. **UNDERSTAND** - Systematically analyze the root cause
3. **DESIGN** - Create a proper solution that addresses the core issue
4. **IMPLEMENT** - Build well-designed, maintainable code
5. **VALIDATE** - Ensure the solution prevents the problem class, not just the symptom

### What We DON'T Accept

- ❌ **Quick patches** that hide underlying issues
- ❌ **Band-aid fixes** that create technical debt
- ❌ **Workarounds** that bypass proper design
- ❌ **Shortcuts** that compromise code quality
- ❌ **"It works for now"** mentality

### What We DEMAND

- ✅ **Systematic analysis** of every problem
- ✅ **Well-designed solutions** that address root causes
- ✅ **Properly architected code** that's maintainable
- ✅ **Comprehensive testing** that prevents regressions
- ✅ **Documentation** that explains the WHY behind decisions

### Problem-Solving Workflow

```bash
# When you hit a problem:
1. Document the problem clearly
2. Analyze dependencies and root causes
3. Design a proper solution
4. Implement with tests
5. Validate the solution prevents the problem class
6. Document the architectural decision
```

---

## 🔄 Development Workflows

### 1. Feature Development Workflow

```bash
# Start new feature
make agent-start
# Agent ID: agent-2
# Component: correlation  
# Action: multi-source-intelligence

# Development cycle
while [[ "$feature_complete" != "true" ]]; do
  # Write failing test first
  go test ./pkg/correlation/... -v
  
  # Implement solution
  # Focus on: clean design, proper error handling, performance
  
  # Validate solution
  make pr-ready
  
  # Commit incremental progress
  git add .
  git commit -m "feat(correlation): add event timeline analysis"
done

# Final validation
make ci
git push origin feature/agent-2/correlation-multi-source-intelligence
```

### 2. Bug Investigation Workflow

```bash
# Bug reported
1. REPRODUCE the bug consistently
2. ANALYZE the root cause (not just symptoms)
3. DESIGN proper fix (not patch)
4. IMPLEMENT with comprehensive tests
5. VALIDATE fix prevents entire problem class
6. DOCUMENT the architectural decision

# Example investigation:
echo "BUG: eBPF events getting dropped"
echo "SYMPTOM: Missing events in correlation engine"
echo "ROOT CAUSE: Ring buffer overflow under load"
echo "PROPER FIX: Implement backpressure and load shedding"
echo "NOT ACCEPTABLE: Increase buffer size (band-aid)"
```

### 3. Performance Optimization Workflow

```bash
# Performance issue detected
1. MEASURE current performance with benchmarks
2. PROFILE to identify actual bottlenecks
3. ANALYZE system-wide impact
4. DESIGN optimization strategy
5. IMPLEMENT with before/after metrics
6. VALIDATE performance improvement
7. DOCUMENT optimization decisions

# Example:
go test -bench=. -benchmem ./pkg/correlation/
go tool pprof cpu.prof
# Identify actual bottleneck (not assumed)
# Design proper solution (not micro-optimization)
```

### 4. Code Review Workflow

```bash
# Before requesting review:
make pr-ready              # All quality gates pass
make test-coverage         # >70% coverage verified
make security             # Security scan clean

# PR checklist:
- [ ] Root cause properly addressed (no patches)
- [ ] Well-designed solution with clean architecture
- [ ] Comprehensive tests prevent regression
- [ ] Performance impact measured and acceptable
- [ ] Documentation explains architectural decisions
- [ ] Human-readable output maintained
- [ ] Zero-config philosophy preserved
```

### 5. Architecture Decision Workflow

```bash
# Major design decisions
1. RESEARCH existing patterns and solutions
2. ANALYZE trade-offs and alternatives
3. DESIGN with future extensibility in mind
4. PROTOTYPE to validate approach
5. IMPLEMENT with comprehensive testing
6. DOCUMENT decision rationale

# Example architectural decision:
echo "DECISION: Use single deployment with modular components"
echo "RATIONALE: Operational simplicity + performance benefits"
echo "ALTERNATIVES CONSIDERED: Microservices (rejected: complexity)"
echo "TRADE-OFFS: Larger binary vs simpler deployment"
```

### 6. Debugging Workflow for Complex Issues

```bash
# Complex system issues
1. COLLECT comprehensive data (logs, metrics, traces)
2. BUILD hypothesis about root cause
3. TEST hypothesis systematically
4. ISOLATE the actual problem
5. DESIGN comprehensive solution
6. IMPLEMENT with prevention measures
7. DOCUMENT troubleshooting process

# Tools for systematic debugging:
make test-e2e             # Real cluster scenarios
make profile              # Performance profiling
make trace                # Execution tracing
kubectl logs -f           # Runtime behavior
```

---

## 🏗️ Architectural Decision Documentation

### Required Documentation for Major Changes

```markdown
# ADR: [Decision Title]

## Status
[Proposed/Accepted/Superseded]

## Context
[What forces are at play? Technical, business, team?]

## Decision
[What we decided to do]

## Rationale
[Why this decision over alternatives]

## Consequences
[Positive and negative impacts]

## Implementation Notes
[How to implement this decision]
```

### Decision Review Process

- All architectural decisions must be discussed before implementation
- Consider impact on accessibility, performance, and maintainability
- Document trade-offs clearly
- Plan migration strategy for breaking changes

---

## 🤝 Agent Collaboration

### Communication Style

- Be direct and actionable in commit messages
- Ask questions when requirements are unclear
- Share knowledge through code comments
- Document architectural decisions and rationale

### Quality Partnership

- Use the branch management system religiously
- Follow the testing pyramid consistently
- Maintain performance standards always
- Keep human accessibility as top priority
- **NEVER accept patches - only proper solutions**

### Escalation Process

- **Technical blockers**: Document analysis and ask for architectural guidance
- **Design decisions**: Present alternatives with trade-off analysis
- **Performance issues**: Provide profiling data and systematic analysis
- **Complex bugs**: Share investigation process and root cause analysis

---

**Remember**: We're building production-grade software that will be used by thousands of developers. Every line of code should be:

- **Well-designed** (not patched together)
- **Properly tested** (preventing entire problem classes)
- **Human-accessible** (making K8s understandable)
- **Performance-conscious** (respecting system resources)
- **Maintainable** (for long-term evolution)
