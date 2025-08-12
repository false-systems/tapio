# Tapio CO-RE Migration Implementation Plan

## Executive Summary

The Tapio project has partial CO-RE (Compile Once - Run Everywhere) support but requires critical improvements for production deployment across kernel versions 5.4 to 6.1+. This document provides a detailed technical implementation plan for achieving full CO-RE compatibility.

## Current State Analysis

### ✅ What's Already Good

1. **CO-RE Attributes Present**: All struct definitions in `vmlinux_minimal.h` correctly use `__attribute__((preserve_access_index))`
2. **BPF Helper Usage**: Proper use of `bpf_core_read()` and `BPF_CORE_READ()` macros
3. **Multi-Architecture Build**: Using `bpf2go` with `-target amd64,arm64` flags
4. **Type Safety**: Well-defined event structures with proper packing

### ❌ Critical Issues Found

#### 1. Architecture-Specific Hardcoded Offsets (HIGH PRIORITY)

**Location**: Multiple kprobe handlers using `pt_regs`
- `/pkg/collectors/kernel/bpf_src/kernel_monitor.c:433`
- `/pkg/collectors/kernel/network/bpf_src/network_monitor.c:198,304,366`
- `/pkg/collectors/kernel/security/bpf_src/security_monitor.c:214,318`

**Problem**: Hardcoded offset `112` for x86_64 RDI register
```c
// Current problematic code:
bpf_probe_read_kernel(&arg1, sizeof(arg1), (void *)((char *)ctx + 112)); // x86_64 RDI
```

**Impact**: Programs will fail or read incorrect data on ARM64 systems

#### 2. Missing BTF Availability Detection (MEDIUM PRIORITY)

**Problem**: No runtime detection of BTF availability before loading BPF programs
- Could fail on older kernels without BTF support
- No graceful fallback mechanism

#### 3. No Kernel Feature Detection (MEDIUM PRIORITY)

**Problem**: No detection of kernel capabilities:
- BPF ring buffer support (kernel 5.8+)
- BPF helpers availability
- Map types support

## Detailed Implementation Plan

### Phase 1: Fix Architecture-Specific Code (Week 1)

#### Task 1.1: Create CO-RE Helper for Function Arguments

**File**: `/home/yair/projects/tapio/pkg/collectors/bpf_common/helpers.h`

```c
// SPDX-License-Identifier: GPL-2.0
#ifndef __BPF_HELPERS_H__
#define __BPF_HELPERS_H__

#include "vmlinux_minimal.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>

// CO-RE helper to read function arguments from pt_regs
static __always_inline unsigned long 
get_func_arg(struct pt_regs *ctx, int arg_num)
{
    unsigned long arg = 0;
    
#ifdef __TARGET_ARCH_x86
    switch(arg_num) {
        case 0:
            bpf_core_read(&arg, sizeof(arg), &ctx->di);
            break;
        case 1:
            bpf_core_read(&arg, sizeof(arg), &ctx->si);
            break;
        case 2:
            bpf_core_read(&arg, sizeof(arg), &ctx->dx);
            break;
        case 3:
            bpf_core_read(&arg, sizeof(arg), &ctx->cx);
            break;
        case 4:
            bpf_core_read(&arg, sizeof(arg), &ctx->r8);
            break;
        case 5:
            bpf_core_read(&arg, sizeof(arg), &ctx->r9);
            break;
    }
#elif defined(__TARGET_ARCH_arm64)
    if (arg_num < 8) {
        bpf_core_read(&arg, sizeof(arg), &ctx->regs[arg_num]);
    }
#endif
    
    return arg;
}

// Alternative: Use BPF_KPROBE macro for newer kernels
#define BPF_KPROBE_READ_ARG(x, arg_num) \
    ({ \
        unsigned long _arg = get_func_arg(ctx, arg_num); \
        bpf_probe_read(&x, sizeof(x), (void *)_arg); \
    })

#endif /* __BPF_HELPERS_H__ */
```

#### Task 1.2: Update All Kprobe Handlers

Replace all hardcoded offset usages with the CO-RE helper:

```c
// Before:
unsigned long arg1 = 0;
bpf_probe_read_kernel(&arg1, sizeof(arg1), (void *)((char *)ctx + 112));

// After:
unsigned long arg1 = get_func_arg(ctx, 0);  // Get first argument
```

### Phase 2: Implement BTF Detection (Week 1-2)

#### Task 2.1: Create BTF Detection Module

**File**: `/home/yair/projects/tapio/pkg/collectors/ebpf/btf_detector.go`

```go
package ebpf

import (
    "errors"
    "fmt"
    "os"
    "github.com/cilium/ebpf/btf"
)

type BTFSupport struct {
    Available bool
    KernelBTF *btf.Spec
    Version   string
}

func DetectBTFSupport() (*BTFSupport, error) {
    support := &BTFSupport{}
    
    // Try to load kernel BTF
    spec, err := btf.LoadKernelSpec()
    if err != nil {
        // Check for BTF in common locations
        paths := []string{
            "/sys/kernel/btf/vmlinux",
            "/boot/vmlinux-%s",
            "/lib/modules/%s/vmlinux",
        }
        
        for _, path := range paths {
            if _, err := os.Stat(path); err == nil {
                spec, err = btf.LoadSpec(path)
                if err == nil {
                    support.Available = true
                    support.KernelBTF = spec
                    break
                }
            }
        }
    } else {
        support.Available = true
        support.KernelBTF = spec
    }
    
    return support, nil
}

// GetFallbackStrategy returns loading strategy based on BTF availability
func (b *BTFSupport) GetFallbackStrategy() LoadStrategy {
    if b.Available {
        return LoadWithCORE
    }
    return LoadLegacy
}
```

### Phase 3: Kernel Feature Detection (Week 2)

#### Task 3.1: Create Feature Detector

**File**: `/home/yair/projects/tapio/pkg/collectors/ebpf/feature_detector.go`

```go
package ebpf

import (
    "github.com/cilium/ebpf"
    "github.com/cilium/ebpf/features"
)

type KernelFeatures struct {
    RingBuffer    bool
    BTF           bool
    CORESupport   bool
    MinKernelVer  string
    MapTypes      map[ebpf.MapType]bool
    ProgTypes     map[ebpf.ProgramType]bool
}

func DetectKernelFeatures() (*KernelFeatures, error) {
    kf := &KernelFeatures{
        MapTypes:  make(map[ebpf.MapType]bool),
        ProgTypes: make(map[ebpf.ProgramType]bool),
    }
    
    // Check ring buffer support (5.8+)
    if err := features.HaveMapType(ebpf.RingBuf); err == nil {
        kf.RingBuffer = true
    }
    
    // Check BTF support
    btfSupport, _ := DetectBTFSupport()
    kf.BTF = btfSupport.Available
    
    // Check program types
    progTypes := []ebpf.ProgramType{
        ebpf.Kprobe,
        ebpf.TracePoint,
        ebpf.XDP,
    }
    
    for _, pt := range progTypes {
        if err := features.HaveProgType(pt); err == nil {
            kf.ProgTypes[pt] = true
        }
    }
    
    return kf, nil
}
```

### Phase 4: Update BPF Loaders (Week 2-3)

#### Task 4.1: Create Smart BPF Loader

**File**: `/home/yair/projects/tapio/pkg/collectors/ebpf/loader.go`

```go
package ebpf

import (
    "fmt"
    "github.com/cilium/ebpf"
    "github.com/cilium/ebpf/link"
    "github.com/cilium/ebpf/rlimit"
)

type BPFLoader struct {
    features *KernelFeatures
    btf      *BTFSupport
}

func NewBPFLoader() (*BPFLoader, error) {
    // Remove memory limit for BPF
    if err := rlimit.RemoveMemlock(); err != nil {
        return nil, fmt.Errorf("failed to remove memlock: %w", err)
    }
    
    features, err := DetectKernelFeatures()
    if err != nil {
        return nil, fmt.Errorf("failed to detect kernel features: %w", err)
    }
    
    btf, err := DetectBTFSupport()
    if err != nil {
        return nil, fmt.Errorf("failed to detect BTF support: %w", err)
    }
    
    return &BPFLoader{
        features: features,
        btf:      btf,
    }, nil
}

func (l *BPFLoader) LoadProgram(spec *ebpf.ProgramSpec) (*ebpf.Program, error) {
    opts := &ebpf.ProgramOptions{}
    
    // Use kernel BTF if available
    if l.btf.Available && l.btf.KernelBTF != nil {
        opts.KernelTypes = l.btf.KernelBTF
    }
    
    // Adjust program based on features
    if !l.features.RingBuffer {
        // Fallback to perf buffer if ring buffer not available
        spec = l.adaptForPerfBuffer(spec)
    }
    
    return ebpf.NewProgramWithOptions(spec, opts)
}
```

### Phase 5: Testing Matrix (Week 3-4)

#### Test Environments Required

| Kernel Version | Distribution | Architecture | BTF Support |
|---------------|--------------|--------------|-------------|
| 5.4.x | Ubuntu 20.04 | x86_64 | No |
| 5.4.x | Ubuntu 20.04 | arm64 | No |
| 5.10.x | Debian 11 | x86_64 | Yes |
| 5.15.x | Ubuntu 22.04 | x86_64 | Yes |
| 5.15.x | Ubuntu 22.04 | arm64 | Yes |
| 6.1.x | Debian 12 | x86_64 | Yes |
| 6.2.x | Ubuntu 23.04 | x86_64 | Yes |

#### Test Script

**File**: `/home/yair/projects/tapio/scripts/test_core_compatibility.sh`

```bash
#!/bin/bash
set -e

KERNEL_VER=$(uname -r)
ARCH=$(uname -m)

echo "Testing Tapio BPF programs on kernel $KERNEL_VER ($ARCH)"

# Test BTF availability
if [ -f /sys/kernel/btf/vmlinux ]; then
    echo "✓ BTF available"
else
    echo "✗ BTF not available - testing fallback"
fi

# Test each collector
for collector in dns cni kernel systemd etcd; do
    echo "Testing $collector collector..."
    
    # Run collector test
    go test -v ./pkg/collectors/$collector/... -tags=ebpf_test
    
    if [ $? -eq 0 ]; then
        echo "✓ $collector: PASSED"
    else
        echo "✗ $collector: FAILED"
        exit 1
    fi
done

echo "All tests passed!"
```

## Implementation Priority

### Week 1: Critical Fixes
1. ✅ Fix hardcoded pt_regs offsets (Task 1.1, 1.2)
2. ✅ Create CO-RE helper functions

### Week 2: Detection & Fallbacks
1. ✅ Implement BTF detection (Task 2.1)
2. ✅ Implement kernel feature detection (Task 3.1)
3. ✅ Create smart BPF loader (Task 4.1)

### Week 3: Integration & Testing
1. ✅ Update all collectors to use new loader
2. ✅ Add fallback strategies for older kernels
3. ✅ Implement comprehensive error handling

### Week 4: Validation
1. ✅ Test on all target kernel versions
2. ✅ Performance benchmarking
3. ✅ Documentation updates

## Success Criteria

1. **Binary Compatibility**: Single binary works on kernels 5.4 to 6.1+
2. **Architecture Support**: Verified on x86_64 and arm64
3. **Graceful Degradation**: Falls back safely on older kernels
4. **Performance**: No regression in data collection efficiency
5. **Error Handling**: Clear error messages for unsupported configurations

## Risk Mitigation

| Risk | Mitigation Strategy |
|------|-------------------|
| Older kernels without BTF | Implement legacy loading with runtime offsets |
| Missing BPF features | Feature detection with alternative implementations |
| Architecture differences | CO-RE helpers with arch-specific paths |
| Performance regression | Benchmark before/after, optimize hot paths |

## Files to Modify

### High Priority (Breaking Issues)
- `/pkg/collectors/kernel/bpf_src/kernel_monitor.c`
- `/pkg/collectors/kernel/network/bpf_src/network_monitor.c`
- `/pkg/collectors/kernel/security/bpf_src/security_monitor.c`

### Medium Priority (Enhancement)
- `/pkg/collectors/dns/bpf_src/dns_monitor.c`
- `/pkg/collectors/cni/bpf_src/cni_monitor.c`
- `/pkg/collectors/systemd/bpf/systemd_monitor.c`

### New Files to Create
- `/pkg/collectors/bpf_common/helpers.h`
- `/pkg/collectors/ebpf/btf_detector.go`
- `/pkg/collectors/ebpf/feature_detector.go`
- `/pkg/collectors/ebpf/loader.go`

## Validation Checklist

- [ ] All pt_regs offset hardcoding removed
- [ ] CO-RE helpers work on x86_64 and arm64
- [ ] BTF detection works correctly
- [ ] Fallback strategies implemented
- [ ] Tests pass on kernel 5.4 without BTF
- [ ] Tests pass on kernel 6.1 with BTF
- [ ] No performance regression
- [ ] Error messages are clear and actionable

## Next Steps

1. Review and approve this plan
2. Create feature branch: `feature/core-migration`
3. Implement Phase 1 (critical fixes) immediately
4. Set up test environments for validation
5. Deploy to staging for real-world testing

## References

- [Linux Kernel BTF Documentation](https://www.kernel.org/doc/html/latest/bpf/btf.html)
- [BPF CO-RE Reference Guide](https://nakryiko.com/posts/bpf-portability-and-co-re/)
- [libbpf Documentation](https://libbpf.readthedocs.io/)
- [Cilium eBPF Go Library](https://github.com/cilium/ebpf)