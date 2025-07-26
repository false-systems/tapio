# Building Tapio with Docker

This document explains how to build Tapio using Docker, which provides a consistent build environment across all platforms and handles eBPF compilation on Linux.

## 🚀 Quick Start

### Build for your current platform:
```bash
make docker-image
```

### Build Linux binary with eBPF support:
```bash
make docker-build-linux-ebpf
```

### Build binaries for all platforms:
```bash
make docker-build
```

## 🐳 Docker Build Options

### 1. Using Make (Recommended)

```bash
# Build standard Linux binary
make docker-build-linux-ebpf

# Build all platform binaries
make docker-build

# Run development environment
make docker-dev

# Build production Docker image
make docker-image
```

### 2. Using Docker Compose

```bash
# Build specific target
docker-compose -f docker-compose.build.yml run --rm build-ebpf

# Build all targets
docker-compose -f docker-compose.build.yml up

# Development environment
docker-compose -f docker-compose.build.yml run --rm dev
```

### 3. Using Build Script

```bash
# Build standard Linux binary
./scripts/docker-build.sh

# Build with eBPF support
./scripts/docker-build.sh --ebpf

# Build all platforms
./scripts/docker-build.sh --all

# Custom output directory
./scripts/docker-build.sh --output dist/
```

## 📦 Build Outputs

All binaries are placed in the `bin/` directory:

- `tapio-linux-amd64` - Standard Linux binary
- `tapio-linux-amd64-ebpf` - Linux binary with eBPF support
- `tapio-darwin-amd64` - macOS Intel binary
- `tapio-darwin-arm64` - macOS Apple Silicon binary
- `tapio-windows-amd64.exe` - Windows binary

## 🏗️ Build Architecture

### Multi-Stage Build Process

1. **eBPF Builder Stage** (Linux only)
   - Installs clang, LLVM, and kernel headers
   - Generates eBPF program bindings
   - Compiles eBPF C code

2. **Go Builder Stage**
   - Downloads Go dependencies
   - Copies eBPF bindings from stage 1
   - Compiles Go code with appropriate tags

3. **Final Stage**
   - Minimal Alpine Linux image
   - Contains only the compiled binary
   - Runs as non-root user

## 🔧 Development with Docker

### Start Development Container

```bash
make docker-dev
```

This provides a full development environment with:
- Go 1.21
- eBPF development tools (clang, llvm, bpftool)
- Kubernetes tools (kubectl)
- Debugging tools (delve, strace, tcpdump)
- Editor support (vim, tmux)

### Development Workflow

1. Start the dev container:
   ```bash
   make docker-dev
   ```

2. Inside the container:
   ```bash
   # Build with eBPF
   make build-ebpf
   
   # Run tests
   make test
   
   # Generate eBPF bindings
   cd ebpf && make generate
   ```

## 🚢 Running Tapio in Docker

### Basic Usage

```bash
# Build the image
docker build -t tapio:latest .

# Run without eBPF
docker run --rm tapio:latest check

# Run with eBPF (requires privileges)
docker run --rm --privileged \
  -v ~/.kube/config:/home/tapio/.kube/config:ro \
  tapio:latest check --enable-ebpf
```

### Docker Compose for Production

```yaml
version: '3.8'

services:
  tapio:
    image: tapio:latest
    command: check --all
    volumes:
      - ~/.kube/config:/home/tapio/.kube/config:ro
    environment:
      - KUBECONFIG=/home/tapio/.kube/config
    # For eBPF support
    privileged: true
    cap_add:
      - CAP_BPF
      - CAP_PERFMON
      - CAP_NET_ADMIN
      - CAP_SYS_RESOURCE
```

## 🔒 Security Considerations

### Running with eBPF

eBPF requires elevated privileges. Options:

1. **Full privileges** (development only):
   ```bash
   docker run --privileged tapio:latest
   ```

2. **Specific capabilities** (recommended):
   ```bash
   docker run --cap-add=CAP_BPF,CAP_PERFMON tapio:latest
   ```

3. **Host PID namespace** (for process visibility):
   ```bash
   docker run --pid=host --cap-add=CAP_BPF tapio:latest
   ```

### Best Practices

- Always use specific capabilities instead of `--privileged` in production
- Mount kubeconfig as read-only
- Use non-root user (default in our image)
- Regularly update base images

## 🐛 Troubleshooting

### eBPF Build Failures

If eBPF generation fails:
1. Ensure you're building on Linux
2. Check kernel headers are available
3. Verify clang/LLVM installation

### Permission Denied

If you get permission errors:
1. Add required capabilities
2. Check container user has access to mounted files
3. Verify Docker daemon permissions

### Cross-Platform Issues

- eBPF only works on Linux
- Use build tags to conditionally compile eBPF code
- Stubs are automatically used on non-Linux platforms

## 📝 CI/CD Integration

The project includes GitHub Actions workflow for automated Docker builds:

```yaml
# .github/workflows/docker-build.yml
- Builds binaries for all platforms
- Creates Docker images
- Publishes to GitHub Container Registry
- Generates release artifacts
```

See `.github/workflows/docker-build.yml` for the complete CI/CD setup.