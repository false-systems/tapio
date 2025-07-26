# Linux Optimization Guide for Tapio

This guide covers Linux-specific optimizations for running Tapio with maximum performance and observability capabilities.

## Table of Contents
- [Native eBPF Performance](#native-ebpf-performance)
- [Kernel Tuning](#kernel-tuning)
- [SystemD Integration](#systemd-integration)
- [Container Runtime Optimization](#container-runtime-optimization)
- [Performance Monitoring](#performance-monitoring)
- [Security Considerations](#security-considerations)

## Native eBPF Performance

Linux provides native eBPF support without virtualization overhead:

### 1. Enable BPF JIT Compilation
```bash
# Enable JIT for better performance
sudo sysctl -w net.core.bpf_jit_enable=1

# Make permanent
echo "net.core.bpf_jit_enable = 1" | sudo tee -a /etc/sysctl.conf
```

### 2. Run eBPF Programs Without Root
```bash
# Grant capabilities to Tapio binaries
sudo setcap cap_sys_admin,cap_sys_resource,cap_net_admin+eip /opt/tapio/bin/ebpf-collector

# Or use our helper script after installation
sudo tapio-setcap
```

### 3. Increase BPF Limits
```bash
# Increase program size limits
sudo sysctl -w kernel.bpf_stats_enabled=1
sudo sysctl -w kernel.unprivileged_bpf_disabled=0

# Increase locked memory for BPF maps
ulimit -l unlimited
```

## Kernel Tuning

### 1. Performance Settings
```bash
# Apply Tapio kernel optimizations
sudo sysctl -p /etc/sysctl.d/99-tapio.conf
```

Key optimizations included:
- **BPF JIT**: Hardware acceleration for eBPF programs
- **Perf buffers**: Larger ring buffers for event collection
- **Network buffers**: Optimized for high-throughput packet processing
- **Kernel tracing**: Full ftrace capabilities enabled

### 2. CPU Performance Governor
```bash
# Set performance mode for consistent latency
echo performance | sudo tee /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor

# Or use cpupower
sudo cpupower frequency-set -g performance
```

### 3. NUMA Optimization
```bash
# For multi-socket systems, bind Tapio to specific NUMA nodes
numactl --cpunodebind=0 --membind=0 /opt/tapio/bin/tapio-collector
```

## SystemD Integration

### 1. Service Management
```bash
# Install services
sudo ./scripts/systemd/install-services.sh

# Start services
sudo systemctl start tapio-collector tapio-intelligence

# Enable at boot
sudo systemctl enable tapio-collector tapio-intelligence

# View logs
journalctl -u tapio-collector -f
```

### 2. Resource Control
```bash
# Adjust CPU quota (default 200%)
sudo systemctl set-property tapio-collector CPUQuota=400%

# Adjust memory limits
sudo systemctl set-property tapio-collector MemoryMax=8G

# Make changes permanent
sudo systemctl daemon-reload
```

### 3. Custom Configuration
Edit `/etc/systemd/system/tapio-collector.service.d/override.conf`:
```ini
[Service]
# Increase workers for high-load systems
Environment="TAPIO_WORKERS=64"
Environment="TAPIO_BUFFER_SIZE=100000"

# Enable debug logging
Environment="TAPIO_LOG_LEVEL=debug"
```

## Container Runtime Optimization

### 1. Native Docker Performance
```bash
# Use native overlayfs2 driver
sudo systemctl stop docker
sudo rm -rf /var/lib/docker
sudo systemctl start docker

# Verify driver
docker info | grep "Storage Driver"
```

### 2. Podman for Rootless Containers
```bash
# Run collectors without root
podman run --cap-add=SYS_ADMIN,NET_ADMIN \
    --security-opt label=disable \
    tapio/ebpf-collector

# Use cgroup v2 for better resource tracking
podman run --cgroup-manager=systemd \
    --cpus=2 --memory=4g \
    tapio/collector
```

### 3. Container Debugging
```bash
# Inspect container layers
dive tapio/collector

# Profile container resource usage
docker stats --format "table {{.Container}}\t{{.CPUPerc}}\t{{.MemUsage}}"

# Trace container syscalls
sudo strace -p $(docker inspect -f '{{.State.Pid}}' tapio-collector)
```

## Performance Monitoring

### 1. Real-time CPU Profiling
```bash
# Profile Tapio for 60 seconds
sudo tapio-profile 60

# Analyze flame graphs
perf script -i /tmp/tapio-profile-*/perf.data | flamegraph.pl > flame.svg
```

### 2. eBPF Program Analysis
```bash
# List loaded programs
sudo bpftool prog list

# Show program statistics
sudo bpftool prog show id 42 stats

# Dump BPF maps
sudo bpftool map dump id 10
```

### 3. System-wide Monitoring
```bash
# Monitor all subsystems
sudo bcc-tools/execsnoop    # Process execution
sudo bcc-tools/opensnoop    # File opens
sudo bcc-tools/tcpconnect   # TCP connections
sudo bcc-tools/biolatency   # Block I/O latency
```

### 4. Network Performance
```bash
# Capture packets with BPF filter
sudo tcpdump -i any -w tapio.pcap 'port 8080'

# Analyze with tshark
tshark -r tapio.pcap -Y 'grpc' -T fields -e frame.time -e ip.src -e grpc.method

# Monitor live traffic
sudo iftop -i eth0 -f "port 8080"
```

## Security Considerations

### 1. Capabilities Instead of Root
```bash
# Check current capabilities
getcap /opt/tapio/bin/ebpf-collector

# Grant minimal required capabilities
sudo setcap cap_sys_admin,cap_net_admin+eip /opt/tapio/bin/ebpf-collector
```

### 2. SELinux Policies
```bash
# Create custom policy for Tapio
sudo semodule -i tapio.pp

# Set contexts
sudo semanage fcontext -a -t tapio_exec_t '/opt/tapio/bin(/.*)?'
sudo restorecon -Rv /opt/tapio
```

### 3. AppArmor Profile
```bash
# Load Tapio profile
sudo apparmor_parser -r /etc/apparmor.d/tapio-collector

# Check status
sudo aa-status | grep tapio
```

## Advanced Optimizations

### 1. Huge Pages
```bash
# Enable transparent huge pages
echo always | sudo tee /sys/kernel/mm/transparent_hugepage/enabled

# Allocate huge pages
echo 1024 | sudo tee /proc/sys/vm/nr_hugepages
```

### 2. IRQ Affinity
```bash
# Bind network IRQs to specific CPUs
sudo ./scripts/set_irq_affinity.sh eth0 0-3

# Isolate CPUs for Tapio
sudo systemctl set-property tapio-collector AllowedCPUs=4-7
```

### 3. Kernel Bypass with XDP
```bash
# Load XDP program for ultra-low latency
sudo ip link set dev eth0 xdp obj tapio_xdp.o sec xdp

# Monitor XDP statistics
sudo bpftool net xdp show
```

## Troubleshooting

### Check eBPF Permissions
```bash
# Verify BPF syscall access
sudo bpftool feature probe | grep bpf_syscall

# Check capability requirements
sudo capsh --print | grep cap_sys_admin
```

### Debug Performance Issues
```bash
# System bottlenecks
sudo perf top

# I/O bottlenecks
sudo iotop -o

# Memory pressure
sudo pmap -x $(pgrep tapio-collector)
```

### Kernel Compatibility
```bash
# Check kernel version (need 5.8+)
uname -r

# Verify BPF features
sudo bpftool feature probe kernel
```

## Benchmarking

### 1. Event Processing
```bash
# Generate load
sudo stress-ng --cpu 8 --io 4 --vm 2 --vm-bytes 128M --timeout 60s

# Monitor Tapio metrics
curl -s localhost:9090/metrics | grep tapio_events_processed
```

### 2. Network Performance
```bash
# Test gRPC throughput
ghz --insecure --proto ./proto/tapio/v1/tapio.proto \
    --call tapio.v1.TapioService.StreamEvents \
    -d '{"events":[{"id":"test"}]}' \
    -n 10000 -c 50 \
    localhost:8080
```

## Production Recommendations

1. **Use cgroup v2** for better resource isolation
2. **Enable BPF JIT** for 5-10x performance improvement
3. **Run on dedicated NUMA node** for consistent latency
4. **Use native Linux features** instead of virtualization
5. **Monitor with Prometheus** + node_exporter
6. **Set appropriate ulimits** for file descriptors and locked memory
7. **Use systemd for** service management and resource control
8. **Regular kernel updates** for latest eBPF features

## Further Reading

- [BPF Performance Tools](http://www.brendangregg.com/bpf-performance-tools-book.html)
- [Linux Kernel eBPF Documentation](https://docs.kernel.org/bpf/)
- [SystemD Resource Control](https://www.freedesktop.org/software/systemd/man/systemd.resource-control.html)
- [Linux Network Tuning Guide](https://www.kernel.org/doc/Documentation/networking/scaling.txt)