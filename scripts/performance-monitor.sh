#!/bin/bash
set -euo pipefail

# Performance monitoring and optimization script for eBPF memory tracking
# This script monitors performance metrics and provides optimization recommendations

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
PKG_DIR="$PROJECT_ROOT/pkg/collectors/ebpf"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_metric() {
    echo -e "${BLUE}[METRIC]${NC} $1"
}

# Check if we're running on Linux
check_platform() {
    if [[ "$OSTYPE" != "linux-gnu"* ]]; then
        log_warn "Performance monitoring is optimized for Linux, current OS: $OSTYPE"
    fi
}

# Run performance benchmarks
run_benchmarks() {
    log_info "Running performance benchmarks..."
    
    cd "$PKG_DIR"
    
    if ! go test -tags ebpf -bench=. -benchtime=5s -count=3 ./... > /tmp/bench_results.txt 2>&1; then
        log_warn "Some benchmarks failed or not available"
        return 0
    fi
    
    log_info "Benchmark Results:"
    echo "===================="
    
    # Parse and display key metrics
    while IFS= read -r line; do
        if [[ $line =~ ^Benchmark.*-[0-9]+.*[0-9]+\.[0-9]+.*ns/op ]]; then
            benchmark_name=$(echo "$line" | awk '{print $1}')
            operations=$(echo "$line" | awk '{print $2}')
            ns_per_op=$(echo "$line" | awk '{print $3}')
            
            log_metric "$benchmark_name: $operations iterations, $ns_per_op"
            
            # Extract numeric value for analysis
            ns_value=$(echo "$ns_per_op" | sed 's/ns\/op//')
            
            # Performance targets analysis
            case $benchmark_name in
                *"ProcessBatchedEvents"*)
                    if (( $(echo "$ns_value > 20000" | bc -l) )); then
                        log_warn "Batch processing slower than target (>20μs per batch)"
                    else
                        log_info "Batch processing meets performance target"
                    fi
                    ;;
                *"PredictOOM"*)
                    if (( $(echo "$ns_value > 1000000" | bc -l) )); then
                        log_warn "OOM prediction slower than target (>1ms)"
                    else
                        log_info "OOM prediction meets performance target"
                    fi
                    ;;
                *"ParseMemoryEvent"*)
                    if (( $(echo "$ns_value > 500" | bc -l) )); then
                        log_warn "Event parsing slower than target (>500ns)"
                    else
                        log_info "Event parsing meets performance target"
                    fi
                    ;;
            esac
        fi
    done < /tmp/bench_results.txt
    
    echo "===================="
}

# Monitor system performance metrics
monitor_system_metrics() {
    log_info "Monitoring system performance metrics..."
    
    # CPU usage
    if command -v top &> /dev/null; then
        cpu_usage=$(top -bn1 | grep "Cpu(s)" | awk '{print $2}' | sed 's/%us,//')
        log_metric "CPU Usage: $cpu_usage%"
        
        if (( $(echo "$cpu_usage > 80" | bc -l) )); then
            log_warn "High CPU usage detected, may affect eBPF performance"
        fi
    fi
    
    # Memory usage
    if command -v free &> /dev/null; then
        mem_info=$(free -h | grep "Mem:")
        total_mem=$(echo "$mem_info" | awk '{print $2}')
        used_mem=$(echo "$mem_info" | awk '{print $3}')
        available_mem=$(echo "$mem_info" | awk '{print $7}')
        
        log_metric "Memory: $used_mem used / $total_mem total ($available_mem available)"
        
        # Check if available memory is less than 1GB
        available_mb=$(free -m | grep "Mem:" | awk '{print $7}')
        if [ "$available_mb" -lt 1024 ]; then
            log_warn "Low available memory ($available_mb MB), may affect performance"
        fi
    fi
    
    # Kernel ring buffer stats (if available)
    if [ -f "/proc/sys/kernel/perf_event_max_sample_rate" ]; then
        max_sample_rate=$(cat /proc/sys/kernel/perf_event_max_sample_rate)
        log_metric "Kernel max sample rate: $max_sample_rate events/sec"
    fi
    
    # eBPF limits
    if [ -f "/proc/sys/kernel/bpf_stats_enabled" ]; then
        bpf_stats=$(cat /proc/sys/kernel/bpf_stats_enabled)
        log_metric "BPF stats enabled: $bpf_stats"
    fi
}

# Check for performance bottlenecks
check_bottlenecks() {
    log_info "Checking for performance bottlenecks..."
    
    # Check swap usage
    if command -v swapon &> /dev/null; then
        swap_info=$(swapon --show=SIZE,USED --noheadings 2>/dev/null || echo "")
        if [ -n "$swap_info" ]; then
            log_warn "Swap is in use, this may degrade eBPF performance:"
            echo "$swap_info"
        else
            log_info "No swap usage detected"
        fi
    fi
    
    # Check for CPU frequency scaling
    if [ -d "/sys/devices/system/cpu/cpu0/cpufreq" ]; then
        scaling_governor=$(cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor 2>/dev/null || echo "unknown")
        log_metric "CPU scaling governor: $scaling_governor"
        
        if [ "$scaling_governor" != "performance" ]; then
            log_warn "CPU not in performance mode, consider: sudo cpupower frequency-set -g performance"
        fi
    fi
    
    # Check kernel features
    if [ -f "/boot/config-$(uname -r)" ]; then
        config_file="/boot/config-$(uname -r)"
        
        # Check BPF features
        if grep -q "CONFIG_BPF=y" "$config_file"; then
            log_info "BPF support enabled in kernel"
        else
            log_warn "BPF support not enabled in kernel config"
        fi
        
        if grep -q "CONFIG_BPF_SYSCALL=y" "$config_file"; then
            log_info "BPF syscall support enabled"
        else
            log_warn "BPF syscall support not enabled"
        fi
        
        if grep -q "CONFIG_CGROUP_BPF=y" "$config_file"; then
            log_info "BPF cgroup support enabled"
        fi
    fi
}

# Generate optimization recommendations
generate_recommendations() {
    log_info "Generating optimization recommendations..."
    
    RECOMMENDATIONS_FILE="$PROJECT_ROOT/performance-recommendations.txt"
    
    cat > "$RECOMMENDATIONS_FILE" << EOF
eBPF Memory Tracking Performance Recommendations
Generated on: $(date)
Kernel Version: $(uname -r)
System Load: $(uptime)

=== Performance Optimization Recommendations ===

1. KERNEL OPTIMIZATION:
   - Ensure kernel version 5.8+ for optimal ring buffer performance
   - Enable BPF JIT compiler: echo 1 > /proc/sys/net/core/bpf_jit_enable
   - Set CPU governor to performance: cpupower frequency-set -g performance
   - Disable swap if possible: swapoff -a

2. SYSTEM TUNING:
   - Increase max locked memory: ulimit -l unlimited
   - Tune ring buffer sizes based on load
   - Consider NUMA topology for multi-socket systems
   - Monitor /proc/sys/kernel/perf_event_max_sample_rate

3. APPLICATION OPTIMIZATION:
   - Use batch processing for high-throughput scenarios
   - Implement adaptive rate limiting during high load
   - Optimize memory allocation patterns to reduce GC pressure
   - Consider event filtering at the eBPF level

4. MONITORING:
   - Track event drop rates and processing latency
   - Monitor memory usage of tracking data structures
   - Set up alerting for prediction accuracy degradation
   - Use performance profiling tools regularly

5. SCALING CONSIDERATIONS:
   - Horizontal scaling: distribute collectors across nodes
   - Vertical scaling: increase CPU and memory resources
   - Consider event aggregation for very high-volume environments
   - Implement circuit breakers for overload protection

=== Current System Status ===
EOF
    
    # Add current system information
    echo "CPU Cores: $(nproc)" >> "$RECOMMENDATIONS_FILE"
    echo "Total Memory: $(free -h | grep Mem: | awk '{print $2}')" >> "$RECOMMENDATIONS_FILE"
    echo "Available Memory: $(free -h | grep Mem: | awk '{print $7}')" >> "$RECOMMENDATIONS_FILE"
    echo "Kernel Version: $(uname -r)" >> "$RECOMMENDATIONS_FILE"
    
    if command -v lscpu &> /dev/null; then
        echo "CPU Model: $(lscpu | grep 'Model name' | cut -d: -f2 | xargs)" >> "$RECOMMENDATIONS_FILE"
    fi
    
    log_info "Recommendations saved to: $RECOMMENDATIONS_FILE"
}

# Profile memory usage
profile_memory_usage() {
    log_info "Profiling memory usage patterns..."
    
    cd "$PKG_DIR"
    
    # Run memory profiling if Go tools are available
    if go test -tags ebpf -memprofile=mem.prof -bench=BenchmarkMemoryEventProcessing ./... &> /dev/null; then
        log_info "Memory profile generated: mem.prof"
        
        if command -v go &> /dev/null; then
            # Analyze memory profile
            go tool pprof -text mem.prof | head -20 > /tmp/memory_profile.txt
            log_info "Top memory allocations:"
            cat /tmp/memory_profile.txt
        fi
    else
        log_warn "Memory profiling not available"
    fi
}

# Test performance targets
test_performance_targets() {
    log_info "Testing against performance targets..."
    
    cd "$PKG_DIR"
    
    if go test -tags ebpf -run="TestPerformanceTargets" ./... -v; then
        log_info "All performance targets met"
    else
        log_warn "Some performance targets not met, check test output"
    fi
}

# Main monitoring function
main() {
    log_info "Starting eBPF memory tracking performance monitoring..."
    
    check_platform
    monitor_system_metrics
    check_bottlenecks
    run_benchmarks
    test_performance_targets
    profile_memory_usage
    generate_recommendations
    
    log_info "Performance monitoring completed"
    log_info "Check performance-recommendations.txt for optimization suggestions"
}

# Handle command line arguments
case "${1:-}" in
    "benchmarks")
        run_benchmarks
        ;;
    "system")
        monitor_system_metrics
        check_bottlenecks
        ;;
    "profile")
        profile_memory_usage
        ;;
    "recommendations")
        generate_recommendations
        ;;
    "targets")
        test_performance_targets
        ;;
    *)
        main
        ;;
esac