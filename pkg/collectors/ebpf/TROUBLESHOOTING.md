# eBPF Memory Collector Troubleshooting Guide

This guide helps resolve common issues when building, testing, or running the eBPF memory collector.

## Common Build Issues

### 1. `go generate` fails with "command not found: bpf2go"

**Solution:**
```bash
# Install bpf2go
go install github.com/cilium/ebpf/cmd/bpf2go@latest

# Add to PATH
export PATH=$PATH:$(go env GOPATH)/bin

# Verify installation
which bpf2go
```

### 2. `fatal error: 'linux/types.h' file not found`

**Solution:**
```bash
# Install kernel headers
# Ubuntu/Debian:
sudo apt-get install linux-headers-$(uname -r)

# Fedora/RHEL:
sudo dnf install kernel-devel kernel-headers
```

### 3. `error: unable to find clang`

**Solution:**
```bash
# Ubuntu/Debian:
sudo apt-get install clang llvm

# Fedora/RHEL:
sudo dnf install clang llvm

# Verify
clang --version
```

## Runtime Issues

### 1. `operation not permitted` when loading eBPF

**Problem:** Insufficient privileges to load eBPF programs.

**Solutions:**

```bash
# Option 1: Run as root
sudo go test -tags ebpf ./...

# Option 2: Add capabilities to Go binary
sudo setcap cap_sys_admin,cap_bpf+eip $(which go)

# Option 3: Run in privileged container
docker run --privileged -v $(pwd):/workspace ...
```

### 2. `eBPF is not available on this system`

**Problem:** eBPF filesystem not mounted or kernel too old.

**Solutions:**

```bash
# Check kernel version (must be 4.18+)
uname -r

# Mount BPF filesystem
sudo mount -t bpf bpf /sys/fs/bpf

# Check if mounted
mount | grep bpf

# Make permanent
echo "bpf /sys/fs/bpf bpf defaults 0 0" | sudo tee -a /etc/fstab
```

### 3. `failed to create ring buffer: not supported`

**Problem:** Kernel doesn't support BPF ring buffer (requires 5.8+).

**Solutions:**

```bash
# Check kernel version
uname -r

# If < 5.8, you need to:
# 1. Upgrade kernel, OR
# 2. Modify code to use perf buffer instead:
```

```go
// In memory.go, replace ring buffer with perf buffer:
// Instead of: ringbuf.NewReader(mc.objs.Events)
// Use: perf.NewReader(mc.objs.Events, 4096)
```

### 4. `BPF program is too large`

**Problem:** eBPF program exceeds verifier limits.

**Solutions:**

1. Reduce program complexity
2. Split into multiple programs
3. Use newer kernel (limits increased over time)
4. Check verifier output:
```bash
sudo cat /sys/kernel/debug/tracing/trace_pipe
```

## Test Failures

### 1. `undefined: memorytrackerObjects`

**Problem:** Generated types don't match expected names.

**Solution:**
```bash
# Check generated file names
ls *_bpfel.go *_bpfeb.go

# Update memory.go to use correct type name
# The pattern is: [name]Objects where [name] is from go:generate
```

### 2. Import cycle or conflicts

**Problem:** Naming conflicts between eBPF types and protobuf types.

**Solution:**
```go
// Add import alias
import (
    protoevents "github.com/yairfalse/tapio/pkg/events"
    // ... other imports
)

// Use aliased types
func convertToUnifiedEvent(event *MemoryEvent, tracker *ProcessMemoryTracker) (*protoevents.UnifiedEvent, error) {
    // Use protoevents.MemoryEvent for protobuf type
    memoryEventData := &protoevents.MemoryEvent{
        // ...
    }
}
```

### 3. Performance benchmarks timeout

**Problem:** Benchmarks take too long or deadlock.

**Solutions:**

```bash
# Increase timeout
go test -tags ebpf -bench=. -timeout=30m

# Run specific benchmark
go test -tags ebpf -bench=BenchmarkMemoryEventProcessing

# Skip slow benchmarks
go test -tags ebpf -bench=. -short
```

## Debugging Techniques

### 1. Enable Verbose eBPF Verifier Output

```bash
# Before running tests
echo 2 | sudo tee /proc/sys/kernel/bpf_verifier_log_level

# Check verifier logs
sudo dmesg | grep -i bpf
```

### 2. Use bpftool for Inspection

```bash
# List loaded programs
sudo bpftool prog list

# Show program details
sudo bpftool prog show id [PROG_ID]

# Dump program instructions
sudo bpftool prog dump xlated id [PROG_ID]

# Show maps
sudo bpftool map list
```

### 3. Enable Debug Logging

```go
// In memory.go constructor
collector := &MemoryCollector{
    config: config,
    // Enable debug
    debug: true,
}

// Add debug prints
if mc.debug {
    fmt.Printf("Loading eBPF program...\n")
}
```

### 4. Memory Profiling

```bash
# Run with memory profile
sudo go test -tags ebpf -memprofile=mem.prof -bench=.

# Analyze
go tool pprof -http=:8080 mem.prof
```

## Platform-Specific Issues

### Ubuntu 20.04 / 22.04
```bash
# May need to disable secure boot for eBPF
# Or sign your eBPF programs

# Check secure boot status
mokutil --sb-state
```

### RHEL/CentOS 8+
```bash
# May need to enable eBPF in kernel
sudo grubby --update-kernel=ALL --args="bpf_jit_enable=1"
sudo reboot
```

### WSL2
```bash
# Ensure you're using WSL2 (not WSL1)
wsl --list --verbose

# May need custom kernel with eBPF enabled
# See: https://github.com/microsoft/WSL2-Linux-Kernel
```

### Container Environments

```dockerfile
# Dockerfile for testing
FROM ubuntu:22.04

RUN apt-get update && apt-get install -y \
    clang \
    llvm \
    linux-headers-generic \
    build-essential \
    golang-go

# Run with:
# docker run --privileged -v $(pwd):/app ...
```

## Getting Help

If you're still stuck:

1. **Check kernel logs:**
   ```bash
   sudo dmesg | tail -100
   sudo journalctl -u kernel -n 100
   ```

2. **Gather system info:**
   ```bash
   uname -a
   cat /proc/version
   ls /sys/kernel/btf/
   ```

3. **Create detailed issue with:**
   - Exact error message
   - Kernel version
   - Distribution
   - Steps to reproduce
   - Output of debugging commands

4. **Check existing issues:**
   - https://github.com/cilium/ebpf/issues
   - https://github.com/yairfalse/tapio/issues

## Quick Fixes Reference

| Error | Quick Fix |
|-------|-----------|
| `operation not permitted` | Run as root: `sudo` |
| `command not found: bpf2go` | `go install github.com/cilium/ebpf/cmd/bpf2go@latest` |
| `linux/types.h not found` | `sudo apt-get install linux-headers-$(uname -r)` |
| `ring buffer not supported` | Upgrade to kernel 5.8+ |
| `undefined: memorytrackerObjects` | Check generated file names |
| `BPF filesystem not mounted` | `sudo mount -t bpf bpf /sys/fs/bpf` |