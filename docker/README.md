# Tapio Docker Development Environment

## Prerequisites

**Mac users:** Start Docker or Colima
```bash
# Option 1: Docker Desktop
# Just start Docker Desktop app

# Option 2: Colima (lightweight)
colima start
```

## Quick Start

```bash
# Build dev container (once)
make docker-dev-build

# Start dev shell (Linux environment on Mac!)
make docker-dev

# Inside container:
go build ./...
go test ./...
make ebpf
```

## Why Docker?

**Problem:** eBPF requires Linux, but development happens on Mac
**Solution:** Docker provides instant Linux environment

## Usage

### Development Shell
```bash
make docker-dev
# Opens bash in Ubuntu 24.04 with eBPF tooling
# Your code is mounted at /tapio
# Changes on Mac reflect instantly in container
```

### Run Tests
```bash
make docker-test
# Runs all tests in Linux container
```

### Build eBPF Programs
```bash
make docker-build-ebpf
# Compiles eBPF programs in Linux container
```

## What's Inside?

- **Ubuntu 24.04** - Stable, eBPF-friendly base
- **clang-18/llvm-18** - eBPF compilation
- **libbpf-dev** - eBPF library headers
- **linux-headers-generic** - Kernel headers for eBPF
- **golang-1.24** - Go toolchain
- **git/make** - Build tools

## Agent Workflow

Both Agent 1 and Agent 2 use the same container:

**Agent 1 (Decoders):**
```bash
make docker-dev
go build ./pkg/decoders/...
go test ./pkg/decoders/...
```

**Agent 2 (eBPF):**
```bash
make docker-dev
cd internal/observers/network/bpf_src
make
go build ../...
```

## CI Integration

Same container used locally and in GitHub Actions:

```yaml
- name: Test
  run: make docker-test
```

No "works on my machine" - identical environment everywhere!
