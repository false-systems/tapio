# Monorepo Considerations for Tapio Architecture

## Current State Analysis

Looking at Tapio's structure, we have characteristics of a monorepo:
- Multiple go.mod files in subdirectories
- Components at different architectural levels
- Interdependent modules

## Key Insights from the Article

### 1. Module Tagging Strategy
For Tapio's architecture with subdirectory modules:
- Root level tags: `v1.2.3`
- Subdirectory tags: `pkg/collectors/ebpf/v1.2.3`
- Each module needs independent versioning

### 2. Dependency Management Challenges

The article warns about circular dependencies. Our architecture MUST avoid this:

```
Level 0: pkg/domain/          # No dependencies ✓
Level 1: pkg/collectors/      # Only domain ✓
Level 2: pkg/intelligence/    # Only domain + L1 ✓
Level 3: pkg/integrations/    # Only domain + L1 + L2 ✓
Level 4: pkg/interfaces/      # All lower levels ✓
```

### 3. Go Workspaces (go.work)

We already have a `go.work` file! This is good for development but we need to be careful:
- Workspaces help during development
- Production releases still need proper versioning
- Changes must propagate from lower to higher levels

### 4. Replace Directives vs Vendor

The article discusses using `replace` directives. For Tapio:
- ❌ AVOID replace directives in go.mod files (makes releases complex)
- ✅ USE go.work for local development
- ✅ USE proper versioning for releases

## Recommended Approach for Tapio

### 1. Module Structure
```
tapio/
├── go.work                           # Development workspace
├── pkg/domain/
│   └── go.mod                       # github.com/falseyair/tapio/pkg/domain
├── pkg/collectors/
│   ├── ebpf/
│   │   └── go.mod                   # github.com/falseyair/tapio/pkg/collectors/ebpf
│   ├── k8s/
│   │   └── go.mod                   # github.com/falseyair/tapio/pkg/collectors/k8s
│   └── [other collectors...]
├── pkg/intelligence/
│   ├── correlation/
│   │   └── go.mod                   # github.com/falseyair/tapio/pkg/intelligence/correlation
│   └── [other intelligence...]
├── pkg/integrations/                 # NEW
│   ├── otel/
│   │   └── go.mod                   # github.com/falseyair/tapio/pkg/integrations/otel
│   └── [other integrations...]
└── pkg/interfaces/                   # NEW
    ├── cli/
    │   └── go.mod                   # github.com/falseyair/tapio/pkg/interfaces/cli
    └── [other interfaces...]
```

### 2. Versioning Strategy

When releasing:
1. Start with lowest level changes (domain)
2. Tag: `pkg/domain/v1.0.0`
3. Move up levels sequentially
4. Each component gets its own semver

Example release sequence:
```bash
# Domain change
git tag pkg/domain/v1.2.0
git push origin pkg/domain/v1.2.0

# Collector using new domain
git tag pkg/collectors/ebpf/v1.1.0
git push origin pkg/collectors/ebpf/v1.1.0

# Intelligence using new collector
git tag pkg/intelligence/correlation/v1.0.5
git push origin pkg/intelligence/correlation/v1.0.5
```

### 3. Development Workflow

During development:
```bash
# Use go.work for local development
go work use ./pkg/domain
go work use ./pkg/collectors/ebpf
go work use ./pkg/intelligence/correlation
# etc...
```

For CI/CD:
```bash
# Don't use workspace in production builds
GOWORK=off go build ./...
```

### 4. Dependency Management

Each module's go.mod should specify exact versions:
```go
// pkg/collectors/ebpf/go.mod
module github.com/falseyair/tapio/pkg/collectors/ebpf

go 1.21

require (
    github.com/falseyair/tapio/pkg/domain v1.2.0
    // other dependencies...
)
```

## Benefits of This Approach

1. **Clean Dependencies**: No circular dependencies possible
2. **Independent Releases**: Each component can be versioned independently
3. **Clear Hierarchy**: Architecture is enforced by module structure
4. **Development Flexibility**: go.work enables rapid local development
5. **Production Stability**: Proper versioning ensures reproducible builds

## Risks to Avoid

1. ❌ Using replace directives in go.mod files
2. ❌ Vendoring with replace (as article warns)
3. ❌ Mixing workspace and production builds
4. ❌ Cross-level imports
5. ❌ Forgetting to tag subdirectory modules correctly

## Conclusion

The monorepo approach works well for Tapio's architecture:
- Enforces architectural boundaries
- Allows independent component development
- Maintains clean dependency hierarchy
- Supports both rapid development and stable releases