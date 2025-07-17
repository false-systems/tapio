# Tapio Modular Build Plan

## Current Problems
1. Single monolithic go.mod with 42 direct dependencies
2. Components can't build independently
3. Circular dependencies everywhere
4. No clear component boundaries

## New Structure

### Core Components (Each with its own go.mod)

1. **pkg/domain** - Core types only, ZERO dependencies
2. **pkg/collectors** - Data collection interfaces
3. **pkg/correlation** - Correlation engine
4. **pkg/ebpf** - eBPF functionality (Linux only)
5. **pkg/capabilities** - Capability management
6. **cmd/tapio** - CLI tool
7. **cmd/install** - Installer

## Implementation Steps

1. Create domain module with pure types
2. Create minimal go.mod for each component
3. Fix imports to use proper module paths
4. Test each module builds independently