# Tapio Infrastructure Cleanup Action Plan

## Immediate Actions Required

### 1. Remove Binary Files from Repository
**Issue**: 14 large binary files (>1MB) found in repository
**Action**: 
```bash
# Add to .gitignore
echo "# Binaries" >> .gitignore
echo "bin/" >> .gitignore
echo "build/" >> .gitignore
echo "*.exe" >> .gitignore
echo "tapio" >> .gitignore
echo "tapio-*" >> .gitignore
echo "server-example" >> .gitignore
echo "pattern-tester" >> .gitignore

# Remove from git
git rm --cached tapio tapio-collector server-example
git rm --cached minimal-tapio/tapio-server minimal-tapio/tapio
git rm --cached pkg/intelligence/correlation/pattern-tester
```

### 2. Consolidate Go Modules
**Issue**: 20 separate go.mod files
**Action**: 
- Keep modular structure for major components
- Consider consolidating test-builds modules
- Remove duplicate test-domain modules

### 3. Address Hardcoded Values
**Issue**: 33 hardcoded ports, 9 hardcoded IPs
**Action**:
- Move all ports to configuration
- Use environment variables or config files
- Create constants file for default values

### 4. Secure Configuration Files
**Issue**: 21 files containing "secret" or "password"
**Action**:
- Verify no actual secrets are committed
- Use Secret management (Kubernetes Secrets, Vault)
- Implement secret scanning in CI/CD

## Docker Consolidation

### Create Base Dockerfiles
```dockerfile
# build/docker/base-alpine.Dockerfile
FROM golang:1.21-alpine AS base-alpine
RUN apk add --no-cache git ca-certificates tzdata
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

# build/docker/base-debian.Dockerfile  
FROM golang:1.21-bullseye AS base-debian
RUN apt-get update && apt-get install -y \
    git ca-certificates tzdata && \
    rm -rf /var/lib/apt/lists/*
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
```

### Update Component Dockerfiles
```dockerfile
# deployments/cli/Dockerfile
ARG BASE_IMAGE=base-alpine
FROM build/docker/${BASE_IMAGE} AS builder
# ... rest of build
```

## Health Check Standardization

### Create Health Check Package
```go
// pkg/health/health.go
package health

type Response struct {
    Status    string            `json:"status"`
    Timestamp time.Time         `json:"timestamp"`
    Service   string            `json:"service"`
    Version   string            `json:"version"`
    Checks    map[string]Check  `json:"checks,omitempty"`
}

type Check struct {
    Status   string        `json:"status"`
    Message  string        `json:"message,omitempty"`
    Latency  string        `json:"latency,omitempty"`
    Metadata interface{}   `json:"metadata,omitempty"`
}
```

### Implement Consistent Handlers
```go
// All services use:
health.Handler(serviceName, version, checks...)
```

## Kubernetes Organization

### Directory Structure
```
deploy/
├── base/              # Base configurations
│   ├── namespace.yaml
│   └── rbac.yaml
├── overlays/          # Environment-specific
│   ├── dev/
│   ├── staging/
│   └── prod/
└── helm/
    └── tapio/         # Helm chart
```

### Helm Improvements
1. Add schema validation:
```yaml
# deploy/helm/tapio/values.schema.json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["replicaCount", "image"],
  "properties": {
    "replicaCount": {
      "type": "integer",
      "minimum": 1
    }
  }
}
```

2. Add NOTES.txt:
```
# deploy/helm/tapio/templates/NOTES.txt
Tapio has been installed!

To access the API:
  kubectl port-forward svc/{{ include "tapio.fullname" . }} 8080:8080

Health check:
  curl http://localhost:8080/health
```

## Configuration Cleanup

### Consolidate Configurations
1. Create shared configuration structure
2. Use ConfigMap for non-sensitive data
3. Use Secrets for sensitive data
4. Implement hot-reload capability

### Environment-Specific Configs
```yaml
# config/base/config.yaml
server:
  port: 8080
  health_port: 8081

# config/overlays/prod/config.yaml  
server:
  tls:
    enabled: true
    cert: /tls/cert.pem
    key: /tls/key.pem
```

## Security Hardening

### 1. Separate Health Check Port
```go
// Internal health checks on different port
go http.ListenAndServe(":8081", healthMux)
// Main API with auth
go http.ListenAndServe(":8080", apiMux)
```

### 2. Remove Debug Endpoints
- No pprof in production builds
- Use build tags for debug features
- Implement feature flags

### 3. Secret Management
- Integrate with Kubernetes Secrets
- Consider HashiCorp Vault
- Implement secret rotation

## Monitoring Improvements

### 1. Standardize Metrics
- Use OTLP for all metrics
- Implement consistent labeling
- Add service mesh integration

### 2. Define SLIs/SLOs
```yaml
# slo/api.yaml
apiVersion: sloth.slok.dev/v1
kind: PrometheusServiceLevel
spec:
  service: "tapio-api"
  labels:
    team: "platform"
  slos:
    - name: "availability"
      objective: 99.9
```

## Build Optimization

### 1. Multi-Stage Builds
- Separate build and runtime stages
- Use distroless base images
- Minimize layer count

### 2. Build Cache
```yaml
# .github/workflows/build.yaml
- uses: docker/build-push-action@v4
  with:
    cache-from: type=gha
    cache-to: type=gha,mode=max
```

## Testing Infrastructure

### Move Test Apps
```bash
mkdir -p test/k8s/apps
mv test/ebpf/test-apps/* test/k8s/apps/
```

### Create Test Namespace
```yaml
# test/k8s/namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: tapio-test
  labels:
    purpose: testing
```

## Documentation Updates

### 1. Infrastructure README
Create `deploy/README.md` with:
- Deployment overview
- Configuration guide
- Security considerations
- Troubleshooting

### 2. Development Guide
Update docs with:
- Local development setup
- Testing procedures
- Release process
- Infrastructure changes

## Timeline

### Week 1
- [ ] Remove binary files
- [ ] Standardize health checks
- [ ] Consolidate Dockerfiles

### Week 2
- [ ] Implement configuration management
- [ ] Security hardening
- [ ] Update documentation

### Week 3
- [ ] Kubernetes reorganization
- [ ] Helm chart improvements
- [ ] Monitoring setup

### Week 4
- [ ] Testing and validation
- [ ] Migration guide
- [ ] Team training

## Success Metrics

1. **Reduced Complexity**
   - 50% fewer Dockerfiles
   - Standardized health checks
   - Consolidated configurations

2. **Improved Security**
   - No hardcoded secrets
   - Separate health check ports
   - Automated secret scanning

3. **Better Observability**
   - Consistent metrics
   - Standardized logging
   - Clear SLIs/SLOs

## Risks and Mitigations

1. **Service Disruption**
   - Mitigation: Phased rollout
   - Test in staging first
   - Maintain rollback plan

2. **Configuration Drift**
   - Mitigation: GitOps approach
   - Automated validation
   - Clear documentation

3. **Team Adoption**
   - Mitigation: Training sessions
   - Clear migration guides
   - Gradual implementation