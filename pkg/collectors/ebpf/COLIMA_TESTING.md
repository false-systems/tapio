# Colima Testing Guide for eBPF Collector

This guide documents how to properly test eBPF functionality using Colima on macOS.

## Overview

Since eBPF is Linux-specific, macOS developers need a Linux VM to test eBPF functionality. Colima provides a lightweight Linux VM that mounts the host filesystem, making it ideal for testing.

## Prerequisites

1. **Install Colima**:
   ```bash
   brew install colima
   ```

2. **Start Colima**:
   ```bash
   colima start
   ```

3. **Verify Colima is running**:
   ```bash
   colima status
   # Should show: "colima is running using macOS Virtualization.Framework"
   ```

## Key Understanding

### Filesystem Mounting
- Colima automatically mounts your macOS filesystem at the same paths
- Your project at `/Users/yair/projects/tapio` is accessible in Colima at `/Users/yair/projects/tapio`
- No file copying is needed - the filesystem is shared

### Go Environment
- Colima VM doesn't have Go installed by default
- You need to build test binaries on macOS for Linux, then run them in Colima
- Use cross-compilation: `GOOS=linux GOARCH=amd64 go test -c`

## Testing Workflow

### Method 1: Cross-Compiled Test Binary (Recommended)

1. **Navigate to test directory**:
   ```bash
   cd pkg/collectors/ebpf/internal
   ```

2. **Build Linux test binary**:
   ```bash
   GOOS=linux GOARCH=amd64 go test -c -o ebpf-tests-linux
   ```

3. **Run tests in Colima**:
   ```bash
   colima ssh -- sudo /Users/yair/projects/tapio/pkg/collectors/ebpf/internal/ebpf-tests-linux -test.v -test.run TestMapManager
   ```

### Method 2: Install Go in Colima (Alternative)

If you need to run `go test` directly in Colima:

1. **Install Go in Colima**:
   ```bash
   colima ssh -- 'curl -L https://go.dev/dl/go1.21.0.linux-amd64.tar.gz | sudo tar -C /usr/local -xzf -'
   colima ssh -- 'echo "export PATH=$PATH:/usr/local/go/bin" >> ~/.bashrc'
   ```

2. **Run tests directly**:
   ```bash
   colima ssh -- 'cd /Users/yair/projects/tapio/pkg/collectors/ebpf/internal && /usr/local/go/bin/go test -v -run TestMapManager'
   ```

## Specific Test Categories

### MapManager Tests
Tests BPF map lifecycle and operations:
```bash
colima ssh -- sudo /Users/yair/projects/tapio/pkg/collectors/ebpf/internal/ebpf-tests-linux -test.v -test.run TestMapManager
```

### PerfEventManager Tests
Tests perf event processing:
```bash
colima ssh -- sudo /Users/yair/projects/tapio/pkg/collectors/ebpf/internal/ebpf-tests-linux -test.v -test.run TestPerfEventManager
```

### Integration Tests
Tests full collector functionality:
```bash
colima ssh -- sudo /Users/yair/projects/tapio/pkg/collectors/ebpf/internal/ebpf-tests-linux -test.v -test.run TestCollector_Integration
```

### Benchmark Tests
Performance validation:
```bash
colima ssh -- sudo /Users/yair/projects/tapio/pkg/collectors/ebpf/internal/ebpf-tests-linux -test.bench=. -test.run=^$ -test.v
```

## Common Issues and Solutions

### Issue: "No such file or directory"
**Problem**: Trying to run test binary that doesn't exist or using wrong path.

**Solution**: 
1. Verify the binary was built: `ls -la ebpf-tests-linux`
2. Use full absolute paths in Colima commands
3. Ensure you're in the correct directory when building

### Issue: "Operation not permitted" or Binary Format Errors
**Problem**: Trying to run macOS binary in Linux or file corruption.

**Solution**:
1. Always use `GOOS=linux GOARCH=amd64` when building
2. Rebuild the test binary if corrupted
3. Use `sudo` for eBPF operations that require root privileges

### Issue: "go: command not found" in Colima
**Problem**: Go is not installed in the Colima VM.

**Solution**: Use cross-compilation (Method 1) or install Go in Colima (Method 2)

## Best Practices

1. **Always use sudo**: eBPF operations require root privileges
2. **Cross-compile on macOS**: Faster than installing Go in Colima
3. **Use full paths**: Avoid directory navigation issues
4. **Clean builds**: Remove old test binaries before rebuilding
5. **Verify Colima status**: Ensure Colima is running before testing

## Example Test Script

Create a `test-ebpf-colima.sh` script:

```bash
#!/bin/bash
set -e

echo "Building eBPF tests for Linux..."
cd pkg/collectors/ebpf/internal
GOOS=linux GOARCH=amd64 go test -c -o ebpf-tests-linux

echo "Running MapManager tests..."
colima ssh -- sudo /Users/yair/projects/tapio/pkg/collectors/ebpf/internal/ebpf-tests-linux -test.v -test.run TestMapManager

echo "Running PerfEventManager tests..."
colima ssh -- sudo /Users/yair/projects/tapio/pkg/collectors/ebpf/internal/ebpf-tests-linux -test.v -test.run TestPerfEventManager

echo "Running Integration tests..."
colima ssh -- sudo /Users/yair/projects/tapio/pkg/collectors/ebpf/internal/ebpf-tests-linux -test.v -test.run TestCollector_Integration

echo "Cleaning up..."
rm -f ebpf-tests-linux

echo "All tests completed!"
```

## Troubleshooting

### Debug Colima SSH Issues
```bash
# Check Colima VM details
colima status

# List files in Colima
colima ssh -- ls -la /Users/yair/projects/tapio/pkg/collectors/ebpf/internal/

# Check if binary is executable
colima ssh -- file /Users/yair/projects/tapio/pkg/collectors/ebpf/internal/ebpf-tests-linux
```

### Verify eBPF Support
```bash
# Check if eBPF is supported in Colima
colima ssh -- 'ls /sys/fs/bpf 2>/dev/null && echo "eBPF supported" || echo "eBPF not supported"'
```

This approach provides a reliable way to test Linux-specific eBPF functionality on macOS development machines.