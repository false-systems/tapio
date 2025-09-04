# CO-RE Implementation Standards for Tapio eBPF Programs

## Overview

This document defines the standards and patterns for implementing CO-RE (Compile Once - Run Everywhere) in Tapio's eBPF collectors. All eBPF programs must follow these patterns to ensure compatibility across different kernel versions and architectures.

## Core Principles

1. **Zero Overhead**: CO-RE compatibility must not impact runtime performance
2. **Kernel-First**: Use CO-RE relocations whenever possible, with safe fallbacks
3. **Architecture Neutral**: Support both x86_64 and ARM64 architectures
4. **Version Resilient**: Handle kernel versions from 5.4 to 6.1+
5. **Fail-Safe**: Gracefully degrade on unsupported kernels

## Implementation Requirements

### 1. Required Headers

All eBPF programs must include:

```c
#include "../../bpf_common/vmlinux_minimal.h"
#include "../../bpf_common/helpers.h"
#include "../../bpf_common/core_compat.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>
```

### 2. Architecture Detection

Use the predefined architecture macros:

```c
#ifdef ARCH_X86_64
    // x86_64 specific code
#elif defined(ARCH_ARM64)
    // ARM64 specific code
#endif
```

### 3. CO-RE Patterns

#### Field Access
Always use CO-RE macros with field existence checks:

```c
// ✅ CORRECT - Safe CO-RE field access
if (bpf_core_field_exists(task->pid)) {
    pid_t pid;
    BPF_CORE_READ_INTO(&pid, task, pid);
}

// ❌ WRONG - Direct field access
pid_t pid = task->pid;
```

#### Structure Reading
Use BPF_CORE_READ for structure access:

```c
// ✅ CORRECT - CO-RE structure reading
struct task_struct *parent;
if (BPF_CORE_READ_INTO(&parent, task, real_parent) == 0) {
    __u32 ppid;
    BPF_CORE_READ_INTO(&ppid, parent, tgid);
}

// ❌ WRONG - Direct pointer dereference
struct task_struct *parent = task->real_parent;
```

#### Cgroup ID Extraction
Use the safe cgroup extraction helper:

```c
// ✅ CORRECT - Safe cgroup extraction
__u64 cgroup_id = safe_cgroup_id_extraction(task);

// ❌ WRONG - Manual cgroup extraction
__u64 cgroup_id = get_cgroup_id_unsafe(task);
```

### 4. Runtime Compatibility

#### Feature Detection
Initialize compatibility features at program start:

```c
SEC("tracepoint/syscalls/sys_enter_openat")
int trace_openat(void *ctx) {
    INIT_CORE_COMPAT();
    REQUIRE_CORE_FEATURES(CORE_FEAT_BTF | CORE_FEAT_TASK_CGROUPS);
    
    // Program logic here...
}
```

#### Graceful Fallbacks
Provide fallback mechanisms for older kernels:

```c
if (g_core_features.has_btf) {
    // Use CO-RE path
    BPF_CORE_READ_INTO(&value, src, field);
} else {
    // Fallback path
    bpf_probe_read_kernel(&value, sizeof(value), &src->field);
}
```

### 5. Error Handling

Use proper error handling patterns:

```c
int ret = BPF_CORE_READ_INTO(&value, src, field);
if (ret != 0) {
    bpf_debug_printk("Failed to read field: %d", ret);
    return 0;  // Graceful exit
}
```

### 6. Build System Integration

#### Generate Commands
All `generate.go` files must use these flags:

```go
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 -cc clang programname ../bpf_src/program.c -- -I../../bpf_common -I../bpf_src -g -O2 -Wall -Wextra
```

#### Compilation Flags
- `-g`: Generate debug information for BTF
- `-O2`: Enable optimizations for performance
- `-Wall -Wextra`: Enable all warnings
- `-target amd64,arm64`: Support both architectures

## Architecture-Specific Patterns

### x86_64 Register Access

```c
#ifdef ARCH_X86_64
static __always_inline unsigned long get_arg0(struct pt_regs *ctx) {
    return BPF_CORE_READ(ctx, di);
}
#endif
```

### ARM64 Register Access

```c
#ifdef ARCH_ARM64
static __always_inline unsigned long get_arg0(struct pt_regs *ctx) {
    return BPF_CORE_READ(ctx, regs[0]);
}
#endif
```

## Kernel Version Compatibility

### Minimum Requirements
- **Kernel 5.4+**: Full CO-RE support with BTF
- **Kernel 4.14+**: Limited support with fallbacks
- **Below 4.14**: Graceful failure with informative messages

### Feature Matrix

| Kernel Version | BTF Support | Ring Buffer | CGroup v2 | Status |
|----------------|-------------|-------------|-----------|---------|
| 6.1+           | ✅          | ✅          | ✅        | Full Support |
| 5.8+           | ✅          | ✅          | ✅        | Full Support |
| 5.4+           | ✅          | ❌          | ✅        | Partial Support |
| 4.14+          | ❌          | ❌          | ✅        | Fallback Mode |
| < 4.14         | ❌          | ❌          | ❌        | Not Supported |

## Common Anti-Patterns

### ❌ Avoid These Patterns

1. **Hard-coded offsets**:
```c
// WRONG
int pid = *(int*)((char*)task + 1234);
```

2. **Direct structure access without CO-RE**:
```c
// WRONG
if (task->cgroups != NULL) { ... }
```

3. **Architecture-specific assumptions**:
```c
// WRONG - assumes x86_64
unsigned long arg = ctx->di;
```

4. **Missing error handling**:
```c
// WRONG
BPF_CORE_READ_INTO(&pid, task, pid);
// No error check
```

5. **No kernel compatibility checks**:
```c
// WRONG - assumes modern kernel
struct kernfs_node *kn = cgroup->kn;
```

## Testing Requirements

### 1. Multi-Architecture Testing
All programs must be tested on:
- x86_64 (Intel/AMD)
- ARM64 (Apple Silicon, ARM servers)

### 2. Multi-Kernel Testing
Test on kernel versions:
- 6.1+ (latest stable)
- 5.15+ (LTS)
- 5.4+ (older LTS)

### 3. Feature Detection Testing
Verify graceful degradation when:
- BTF is not available
- Ring buffers are not supported
- Specific kernel structures are missing

## Code Review Checklist

- [ ] Uses `vmlinux_minimal.h` instead of system headers
- [ ] Includes proper CO-RE headers
- [ ] Uses `BPF_CORE_READ*` macros for field access
- [ ] Checks field existence with `bpf_core_field_exists`
- [ ] Has proper error handling for all CO-RE operations
- [ ] Supports both x86_64 and ARM64 architectures
- [ ] Uses `INIT_CORE_COMPAT()` for feature detection
- [ ] Has graceful fallbacks for unsupported features
- [ ] Generate command includes proper compilation flags
- [ ] No hard-coded offsets or architecture assumptions

## Migration Guidelines

### From Legacy eBPF to CO-RE

1. **Replace direct field access**:
```c
// Before
pid_t pid = task->pid;

// After
pid_t pid;
if (bpf_core_field_exists(task->pid)) {
    BPF_CORE_READ_INTO(&pid, task, pid);
}
```

2. **Replace bpf_probe_read with CO-RE**:
```c
// Before
bpf_probe_read(&value, sizeof(value), &src->field);

// After
BPF_CORE_READ_INTO(&value, src, field);
```

3. **Add feature detection**:
```c
// Add at function start
INIT_CORE_COMPAT();
REQUIRE_CORE_FEATURES(CORE_FEAT_BTF);
```

## Performance Impact

CO-RE implementation adds minimal overhead:

1. **Compile-time**: CO-RE relocations are resolved at load time
2. **Runtime**: No additional CPU cycles for field access
3. **Memory**: Minimal increase in program size for feature detection

## Summary

Following these CO-RE standards ensures:

✅ **Compatibility**: Works across kernel versions 5.4+  
✅ **Portability**: Supports x86_64 and ARM64 architectures  
✅ **Reliability**: Graceful degradation on unsupported systems  
✅ **Performance**: Zero runtime overhead for CO-RE operations  
✅ **Maintainability**: Consistent patterns across all collectors  

All new eBPF programs must implement these patterns, and existing programs should be migrated to follow these standards.