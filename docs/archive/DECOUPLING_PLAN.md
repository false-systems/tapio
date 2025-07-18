# Tapio Decoupling Plan

## 🎯 Target Architecture
```
tapio-ecosystem/
├── cmd/
│   ├── tapio-cli/           # Lightweight CLI binary
│   ├── tapio-engine/        # Correlation engine service
│   ├── tapio-gui/           # Desktop app (Wails + Vue)
│   └── plugins/
│       ├── tapio-prometheus/
│       ├── tapio-otel/
│       ├── tapio-hubble/
│       └── tapio-grafana/
├── pkg/
│   ├── engine/              # Shared engine interfaces
│   ├── client/              # Engine client library
│   ├── plugins/             # Plugin framework
│   └── api/                 # gRPC/REST definitions
└── deployments/
    ├── cli/                 # CLI installation
    ├── engine/              # Engine deployment (K8s/Docker)
    └── plugins/             # Plugin distribution
```

## 📊 Current Coupling Issues

### 1. **CLI Component Coupling**
- **Current**: `cmd/tapio/` → `internal/cli/` → `pkg/*` (everything)
- **Issue**: CLI directly imports server-specific packages
- **Solution**: Create `cmd/tapio-cli/` with own `pkg/client/` library

### 2. **Monolithic pkg/ Directory**
- **Current**: Single `pkg/` with 30+ packages used by all components
- **Issue**: No clear boundaries between component responsibilities
- **Solution**: Split into component-specific packages

### 3. **Internal Package Shared Usage**
- **Current**: `internal/` used by CLI, server, and other components
- **Issue**: Violates Go's internal package design principles
- **Solution**: Move to proper public APIs

### 4. **Configuration Coupling**
- **Current**: Shared `pkg/config/` with monolithic configuration
- **Issue**: All components know about all configuration
- **Solution**: Component-specific configs with schema validation

### 5. **Data Structure Coupling**
- **Current**: Direct struct sharing across components
- **Issue**: Changes affect all components
- **Solution**: Interface-based communication with DTOs

## 🔧 Decoupling Strategy

### Phase 1: Create New Structure
1. Create target directory structure
2. Setup component-specific go.mod files
3. Define clear API boundaries

### Phase 2: Extract CLI Component
1. Move CLI to `cmd/tapio-cli/`
2. Create `pkg/client/` library
3. Remove dependencies on internal packages

### Phase 3: Restructure Engine
1. Move server to `cmd/tapio-engine/`
2. Create `pkg/engine/` interfaces
3. Extract correlation engine as library

### Phase 4: Plugin Framework
1. Create `pkg/plugins/` framework
2. Extract existing plugins to `cmd/plugins/`
3. Create plugin SDK

### Phase 5: Decouple GUI
1. Move GUI to `cmd/tapio-gui/`
2. Create API-only communication
3. Remove direct backend dependencies

### Phase 6: Create Shared Libraries
1. `pkg/api/` - gRPC/REST definitions
2. `pkg/client/` - Engine client
3. `pkg/plugins/` - Plugin framework

### Phase 7: Implement Clean Interfaces
1. Dependency injection pattern
2. Interface-based communication
3. Clean architecture principles

### Phase 8: Component Deployment
1. Create component-specific deployments
2. Independent scaling
3. Separate release cycles

## 📋 Migration Steps

### Step 1: Backup and Prepare
```bash
# Create backup branch
git checkout -b backup-before-decoupling
git push origin backup-before-decoupling

# Create new structure branch
git checkout -b feature/modular-architecture
```

### Step 2: Create New Directory Structure
```bash
mkdir -p cmd/tapio-cli
mkdir -p cmd/tapio-engine
mkdir -p cmd/tapio-gui
mkdir -p cmd/plugins/{tapio-prometheus,tapio-otel,tapio-hubble,tapio-grafana}
mkdir -p pkg/{engine,client,plugins,api}
mkdir -p deployments/{cli,engine,plugins}
```

### Step 3: Component-Specific go.mod Files
- `cmd/tapio-cli/go.mod`
- `cmd/tapio-engine/go.mod`
- `cmd/tapio-gui/go.mod`
- `cmd/plugins/*/go.mod`

### Step 4: Extract Components Systematically
1. Start with CLI (least dependencies)
2. Move to Engine (core functionality)
3. Extract Plugins (most isolated)
4. Finish with GUI (frontend only)

## 🎯 Success Criteria

### Technical Metrics
- [ ] Each component has independent go.mod
- [ ] No direct imports between cmd/ directories
- [ ] All communication via well-defined APIs
- [ ] Plugin system supports independent development
- [ ] Each component can be deployed separately

### Architectural Principles
- [ ] Single Responsibility: Each component has clear purpose
- [ ] Dependency Inversion: Components depend on abstractions
- [ ] Interface Segregation: Small, focused interfaces
- [ ] Open/Closed: Extensible via plugins without modification

## 🚨 Risks and Mitigation

### Risk 1: Breaking Changes
- **Mitigation**: Gradual migration with compatibility layers
- **Rollback**: Backup branch available

### Risk 2: Performance Impact
- **Mitigation**: Benchmark before/after
- **Monitoring**: Performance tests in CI

### Risk 3: Configuration Complexity
- **Mitigation**: Schema validation and migration tools
- **Documentation**: Clear migration guide

### Risk 4: Integration Issues
- **Mitigation**: E2E tests for each component
- **Testing**: Comprehensive integration test suite

## 📈 Implementation Timeline

| Phase | Duration | Dependencies |
|-------|----------|-------------|
| Phase 1 | 1-2 days | None |
| Phase 2 | 3-4 days | Phase 1 |
| Phase 3 | 5-7 days | Phase 2 |
| Phase 4 | 4-5 days | Phase 3 |
| Phase 5 | 2-3 days | Phase 4 |
| Phase 6 | 3-4 days | All phases |
| Phase 7 | 5-6 days | Phase 6 |
| Phase 8 | 2-3 days | Phase 7 |

**Total Estimated Time**: 25-35 days

## 🔄 Next Steps

1. **Review and Approval**: Stakeholder review of this plan
2. **Phase 1 Execution**: Create new directory structure
3. **Component Extraction**: Start with CLI component
4. **Iterative Development**: Phase by phase execution
5. **Testing and Validation**: Comprehensive testing at each phase
6. **Documentation**: Update all documentation for new structure

---

*This plan ensures systematic decoupling while maintaining system functionality and enabling independent component development.*