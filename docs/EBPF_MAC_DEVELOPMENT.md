# eBPF Development on macOS

A comprehensive guide for developing and testing eBPF collectors on macOS for Linux deployment.

## 🎯 Overview

eBPF (Extended Berkeley Packet Filter) is Linux-specific kernel technology. While you cannot run eBPF programs on macOS, you can develop, compile, and test them for Linux targets using various approaches.

## 🛠️ Development Strategies

### 1. Docker-Based Development (Recommended)

The most straightforward approach for eBPF development on Mac.

#### Setup

```bash
# Create a development container with eBPF tools
docker run -it --rm \
  --privileged \
  -v $(pwd):/workspace \
  -w /workspace \
  ubuntu:22.04 bash

# Inside container, install dependencies
apt-get update && apt-get install -y \
  clang \
  llvm \
  libbpf-dev \
  linux-headers-generic \
  build-essential \
  golang-go
```

#### Dockerfile for eBPF Development

```dockerfile
FROM ubuntu:22.04

RUN apt-get update && apt-get install -y \
    clang-14 \
    llvm-14 \
    libbpf-dev \
    linux-headers-5.15.0-91-generic \
    build-essential \
    pkg-config \
    golang-1.21 \
    git \
    vim

# Install bpftool
RUN git clone --recurse-submodules https://github.com/libbpf/bpftool.git && \
    cd bpftool/src && \
    make && \
    make install

WORKDIR /workspace
```

### 2. Lima VM (Linux on Mac)

Lima provides Linux VMs with automatic file sharing and port forwarding.

```bash
# Install Lima
brew install lima

# Create Ubuntu VM optimized for eBPF development
limactl start --name=ebpf-dev --cpus=4 --memory=8 \
  --vm-type=vz --mount-type=virtiofs \
  --mount=$(pwd):/workspace \
  template://ubuntu-lts

# Enter the VM
limactl shell ebpf-dev

# Install eBPF tools in VM
sudo apt-get update && sudo apt-get install -y \
  clang llvm libbpf-dev linux-headers-$(uname -r)
```

### 3. Remote Linux Development

Use VS Code Remote-SSH or similar tools to develop on a Linux machine.

```bash
# SSH config (~/.ssh/config)
Host ebpf-dev
    HostName your-linux-box.local
    User developer
    ForwardAgent yes
    
# Connect with VS Code
code --remote ssh-remote+ebpf-dev /path/to/project
```

## 📦 Compilation Process

### Step 1: Compile eBPF C Code to BPF Bytecode

```bash
# Use Docker for compilation
docker run --rm -v $(pwd):/src -w /src \
  ghcr.io/cilium/ebpf-builder:latest \
  clang -O2 -g -target bpf \
    -D__TARGET_ARCH_x86 \
    -I/usr/include/x86_64-linux-gnu \
    -c bpf_src/memory_monitor.c \
    -o bpf_src/memory_monitor.o
```

### Step 2: Generate Go Bindings

```bash
# Install bpf2go
go install github.com/cilium/ebpf/cmd/bpf2go@latest

# Generate Go code from compiled BPF
bpf2go -cc clang-14 -cflags "-O2 -g -D__TARGET_ARCH_x86" \
  memoryMonitor bpf_src/memory_monitor.c -- \
  -I/usr/include/x86_64-linux-gnu
```

### Step 3: Build Go Binary (Cross-Compile)

```bash
# On Mac, cross-compile for Linux
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go build -tags ebpf \
  -o bin/collector-linux ./cmd/collector
```

## 🔧 Makefile for eBPF Development

Create a `Makefile` for consistent builds:

```makefile
# Variables
CLANG := clang-14
CFLAGS := -O2 -g -Wall -Werror
BPF_CFLAGS := $(CFLAGS) -target bpf -D__TARGET_ARCH_x86
BPF_INCLUDES := -I/usr/include/x86_64-linux-gnu

# BPF sources
BPF_SRCS := $(wildcard bpf_src/*.c)
BPF_OBJS := $(BPF_SRCS:.c=.o)

# Targets
.PHONY: all ebpf go clean docker-build

all: ebpf go

# Compile eBPF programs
ebpf: $(BPF_OBJS)

bpf_src/%.o: bpf_src/%.c
	$(CLANG) $(BPF_CFLAGS) $(BPF_INCLUDES) -c $< -o $@

# Generate Go bindings
generate:
	go generate ./...

# Build Go binary
go:
	GOOS=linux GOARCH=amd64 go build -tags ebpf -o bin/collector .

# Docker-based build
docker-build:
	docker build -f Dockerfile.ebpf -t ebpf-builder .
	docker run --rm -v $(PWD):/workspace ebpf-builder make all

clean:
	rm -f bpf_src/*.o
	rm -f bin/*
```

## 🐛 Testing Strategies

### 1. Unit Tests (Mac-Compatible)

```go
//go:build !linux
// +build !linux

package memory_leak_hunter

import "testing"

func TestConfigValidation(t *testing.T) {
    // Test configuration logic that doesn't need eBPF
    config := &Config{
        MinAllocationSize: 1024,
        SamplingRate: 10,
    }
    
    if err := config.Validate(); err != nil {
        t.Errorf("Valid config failed: %v", err)
    }
}
```

### 2. Integration Tests (Docker)

```bash
#!/bin/bash
# test_ebpf.sh

# Build test container
docker build -t ebpf-test -f Dockerfile.test .

# Run tests with privileges needed for eBPF
docker run --rm --privileged \
  -v /sys/kernel/debug:/sys/kernel/debug:ro \
  ebpf-test go test -tags ebpf ./...
```

### 3. VM-Based Testing

```yaml
# .github/workflows/ebpf-test.yml
name: eBPF Tests
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Install Dependencies
        run: |
          sudo apt-get update
          sudo apt-get install -y clang llvm libbpf-dev
      
      - name: Compile eBPF
        run: make ebpf
      
      - name: Run Tests
        run: sudo go test -tags ebpf ./...
```

## 🏗️ Project Structure

Recommended structure for eBPF collectors:

```
pkg/collectors/your-collector/
├── bpf_src/
│   ├── monitor.c          # eBPF C source
│   └── monitor.h          # Headers
├── collector.go           # Platform-agnostic logic
├── collector_ebpf.go      # Linux-specific eBPF logic
├── collector_mock.go      # Mac/Windows mock
├── config.go             # Configuration
├── types.go              # Shared types
├── Makefile              # Build automation
└── README.md            # Documentation
```

## 🔍 Debugging Tips

### 1. BPF Verifier Errors

When developing on Mac and deploying to Linux:

```bash
# Use verbose verifier output in Docker
docker run --rm --privileged -v $(pwd):/src \
  ubuntu:22.04 bash -c "
    cd /src && \
    bpftool prog load memory_monitor.o /sys/fs/bpf/test \
    2>&1 | grep -A20 'Verifier analysis'
  "
```

### 2. Cross-Platform Build Tags

```go
//go:build linux && ebpf
// +build linux,ebpf

package collector

// Linux-specific eBPF implementation
```

```go
//go:build !linux || !ebpf
// +build !linux !ebpf

package collector

// Mock implementation for development
```

### 3. Testing Locally with Vagrant

```ruby
# Vagrantfile
Vagrant.configure("2") do |config|
  config.vm.box = "ubuntu/jammy64"
  
  config.vm.provider "virtualbox" do |vb|
    vb.memory = "4096"
    vb.cpus = 2
  end
  
  config.vm.provision "shell", inline: <<-SHELL
    apt-get update
    apt-get install -y clang llvm libbpf-dev linux-headers-$(uname -r)
  SHELL
  
  config.vm.synced_folder ".", "/vagrant"
end
```

## 📚 Resources

- [cilium/ebpf](https://github.com/cilium/ebpf) - Go library for eBPF
- [libbpf](https://github.com/libbpf/libbpf) - C library for eBPF
- [BPF CO-RE](https://nakryiko.com/posts/bpf-portability-and-co-re/) - Write once, run everywhere
- [bpftrace](https://github.com/iovisor/bpftrace) - High-level tracing language

## 🚀 Quick Start Script

```bash
#!/bin/bash
# setup_ebpf_dev.sh

echo "Setting up eBPF development environment on Mac..."

# Install Docker if not present
if ! command -v docker &> /dev/null; then
    echo "Installing Docker..."
    brew install --cask docker
fi

# Pull eBPF builder image
docker pull ghcr.io/cilium/ebpf-builder:latest

# Create development directory structure
mkdir -p bpf_src bin

# Create sample eBPF program
cat > bpf_src/hello.c << 'EOF'
#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>

SEC("tracepoint/syscalls/sys_enter_open")
int trace_open(void *ctx) {
    bpf_printk("Hello from eBPF!\n");
    return 0;
}

char _license[] SEC("license") = "GPL";
EOF

# Compile in Docker
docker run --rm -v $(pwd):/src -w /src \
  ghcr.io/cilium/ebpf-builder:latest \
  clang -O2 -g -target bpf -c bpf_src/hello.c -o bpf_src/hello.o

echo "✅ eBPF development environment ready!"
echo "📝 Next steps:"
echo "  1. Edit bpf_src/hello.c"
echo "  2. Run: make ebpf"
echo "  3. Test in Docker or Lima VM"
```

## ⚠️ Common Pitfalls

1. **Memory Access**: eBPF has strict memory access rules. Always use `bpf_probe_read_*` helpers.
2. **Stack Size**: Limited to 512 bytes. Use per-CPU maps for larger data.
3. **Loop Bounds**: Loops must have verifiable bounds (max 8192 iterations).
4. **Map Size**: Consider memory limits when sizing maps.
5. **Compatibility**: Use CO-RE (Compile Once, Run Everywhere) for portability.

## 🎓 Best Practices

1. **Always develop with Linux headers matching your target kernel**
2. **Use build tags to separate platform-specific code**
3. **Test in containers or VMs that match production environment**
4. **Version your eBPF bytecode alongside Go code**
5. **Document kernel version requirements**
6. **Handle graceful degradation when eBPF isn't available**

This guide provides everything needed to develop production-ready eBPF collectors on macOS for Linux deployment.