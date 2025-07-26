# eBPF Compatibility Guide for Tapio

## 🚀 Quick Start

```bash
# Check if your system is ready
./scripts/install-ebpf-deps.sh

# Build and run
make build
sudo ./bin/tapio check
```

## 📊 Compatibility Matrix

| Distribution | Kernel | eBPF Support | Notes |
|-------------|---------|--------------|-------|
| **Ubuntu 22.04** | 5.15+ | ✅ Excellent | Full features, BTF support |
| **Ubuntu 20.04** | 5.4+ | ✅ Excellent | Full features, BTF support |
| **Ubuntu 18.04** | 4.15 | ⚠️ Limited | Basic eBPF only, consider upgrade |
| **Debian 12** | 6.1+ | ✅ Excellent | Latest eBPF features |
| **Debian 11** | 5.10+ | ✅ Excellent | Full features |
| **RHEL 9** | 5.14+ | ✅ Excellent | Full enterprise support |
| **RHEL 8** | 4.18 | ⚠️ Limited | Basic eBPF, no BTF |
| **Fedora 38+** | 6.2+ | ✅ Excellent | Latest features |
| **Arch Linux** | Latest | ✅ Excellent | Always up-to-date |
| **Amazon Linux 2** | 4.14 | ⚠️ Limited | Basic eBPF only |
| **Amazon Linux 2023** | 6.1+ | ✅ Excellent | Full features |

## 🔍 Feature Support by Kernel Version

### Kernel 4.14+ (Minimum)
- ✅ Basic eBPF programs
- ✅ Tracepoints
- ❌ BTF (BPF Type Format)
- ❌ CO-RE (Compile Once, Run Everywhere)

### Kernel 4.18+ (Better)
- ✅ Everything from 4.14
- ✅ BPF to BPF calls
- ✅ Better verifier
- ⚠️ Limited BTF support

### Kernel 5.4+ (Recommended)
- ✅ Everything from 4.18
- ✅ Full BTF support
- ✅ CO-RE compatibility
- ✅ Ring buffer support
- ✅ Better performance

### Kernel 5.10+ (Ideal)
- ✅ Everything from 5.4
- ✅ Sleepable BPF programs
- ✅ Task local storage
- ✅ Advanced features

## 🐳 Container/Kubernetes Environments

### Standard Kubernetes
```bash
# eBPF requires privileged mode
apiVersion: v1
kind: Pod
spec:
  containers:
  - name: tapio
    image: tapio:latest
    securityContext:
      privileged: true  # Required for eBPF
```

### GKE (Google Kubernetes Engine)
- ✅ COS nodes: Kernel 5.4+, works great
- ✅ Ubuntu nodes: Full support

### EKS (Amazon Elastic Kubernetes)
- ⚠️ AL2 nodes: Kernel 4.14, limited eBPF
- ✅ AL2023 nodes: Kernel 6.1+, full support
- ✅ Bottlerocket: Kernel 5.10+, full support

### AKS (Azure Kubernetes Service)
- ✅ Ubuntu nodes: Full support
- ✅ CBL-Mariner: Kernel 5.15+, full support

## 🔧 Troubleshooting

### "Operation not permitted"
```bash
# Need CAP_BPF or CAP_SYS_ADMIN
sudo setcap cap_bpf,cap_sys_admin+eip ./bin/tapio
# Or just run with sudo
sudo ./bin/tapio check
```

### "Cannot find kernel headers"
```bash
# Ubuntu/Debian
sudo apt install linux-headers-$(uname -r)

# RHEL/Fedora
sudo dnf install kernel-headers-$(uname -r)
```

### "BTF not available"
This is OK! Tapio will work but with some limitations:
- Slightly larger eBPF binaries
- Less portable across kernel versions
- Still get memory tracking and predictions

### WSL2 (Windows Subsystem for Linux)
```bash
# Check kernel version
uname -r
# If 5.10+, full eBPF support!
# If older, limited support
```

## 🚨 Security Considerations

### Required Capabilities
- `CAP_BPF` (kernel 5.8+) or `CAP_SYS_ADMIN`
- `CAP_PERFMON` for performance data
- `CAP_NET_ADMIN` for network tracing (future)

### Running Without Root
```bash
# Option 1: Set capabilities on binary
sudo setcap cap_bpf,cap_perfmon+eip ./bin/tapio

# Option 2: Use ambient capabilities
sudo capsh --caps="cap_bpf,cap_perfmon+eip" -- -c "./bin/tapio check"

# Option 3: Run in container with capabilities
docker run --cap-add BPF --cap-add PERFMON tapio check
```

## 🎯 Quick Decision Tree

```
Is your kernel >= 5.4?
├─ YES → Full eBPF support! 🎉
└─ NO → Is it >= 4.14?
    ├─ YES → Basic eBPF works
    └─ NO → Upgrade kernel or use without eBPF
```

## 📈 Performance Impact

| Kernel Version | CPU Overhead | Memory Usage | Features |
|----------------|--------------|--------------|----------|
| 5.10+ | < 0.05% | ~1MB | All features |
| 5.4+ | < 0.1% | ~1MB | Most features |
| 4.18+ | < 0.2% | ~2MB | Basic features |
| 4.14+ | < 0.3% | ~2MB | Minimal features |

## 🏗 Building from Source on Older Systems

If your distro's packages are too old:

```bash
# Build latest libbpf from source
git clone https://github.com/libbpf/libbpf.git
cd libbpf/src
make
sudo make install

# Build latest bpftool
git clone https://github.com/libbpf/bpftool.git
cd bpftool/src
make
sudo make install
```

## 📝 Summary

- **Best Experience**: Ubuntu 20.04+, Fedora 34+, RHEL 9+
- **Minimum Viable**: Any Linux with kernel 4.14+
- **Not Supported**: Kernel < 4.14, non-Linux systems

When in doubt, run `./scripts/install-ebpf-deps.sh` - it will tell you exactly what your system supports!