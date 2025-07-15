# Circular Dependency Resolution

## Problem

There was a circular import dependency in the cmd/install package:
```
installer -> platform -> installer
```

## Root Cause

1. The `platform` package had a `factory.go` file that imported types from the `installer` package
2. The `installer` package files (binary.go, docker.go, kubernetes.go) imported `platform.Info` from the `platform` package
3. Additionally, the `validation` package implemented `installer.Validator` interface, creating another cycle

## Solution

### 1. Created Common Types Package

Created `/cmd/install/common/types.go` to hold shared types:
- `PlatformInfo` - moved from `platform` package
- `Validator` interface - moved from `installer` package

### 2. Created Separate Factory Package

Moved the factory logic from `platform/factory.go` to a new `/cmd/install/factory/factory.go` package that:
- Imports from both `common` and `installer` packages
- Creates installers based on strategy
- Breaks the circular dependency

### 3. Updated Import Paths

- Platform package: Uses type alias `type Info = common.PlatformInfo` for backward compatibility
- Installer package: Updated to use `common.PlatformInfo` instead of `platform.Info`
- Validation package: Updated to implement `common.Validator` instead of `installer.Validator`
- Main.go: Updated to use the new `factory` package

## New Dependency Graph

```
main.go
├── factory (creates installers)
│   ├── common (shared types)
│   └── installer (installer implementations)
│       ├── common (for PlatformInfo)
│       └── validation (for validation logic)
│           └── common (for Validator interface)
└── platform (platform detection)
    └── common (for PlatformInfo)
```

No circular dependencies!

## Benefits

1. Clean separation of concerns
2. No circular imports
3. Shared types in a common location
4. Factory pattern properly isolated
5. Validation can implement interfaces without creating cycles