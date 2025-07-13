#!/bin/bash

# Profile-Guided Optimization (PGO) benchmark script
# This script runs benchmarks to generate profiles for PGO compilation

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
BENCHMARK_DIR="${PROJECT_ROOT}/pkg/otel/benchmarks"
PROFILES_DIR="${PROJECT_ROOT}/profiles"
PGO_DIR="${PROJECT_ROOT}/pgo"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Create directories
mkdir -p "${PROFILES_DIR}"
mkdir -p "${PGO_DIR}"

cd "${PROJECT_ROOT}"

log_info "Starting Profile-Guided Optimization (PGO) benchmark process..."

# Step 1: Clean previous builds
log_info "Cleaning previous builds..."
go clean -cache
go clean -testcache
rm -f "${PROFILES_DIR}"/*.prof
rm -f "${PGO_DIR}"/*.prof

# Step 2: Run baseline benchmarks (without PGO)
log_info "Running baseline benchmarks (without PGO)..."
BASELINE_RESULTS="${PROFILES_DIR}/baseline.txt"
go test -bench=. -benchmem -count=3 "${BENCHMARK_DIR}" > "${BASELINE_RESULTS}" 2>&1 || {
    log_error "Baseline benchmarks failed"
    exit 1
}
log_success "Baseline benchmarks completed"

# Step 3: Generate CPU profile from benchmarks
log_info "Generating CPU profile from benchmarks..."
CPU_PROFILE="${PROFILES_DIR}/cpu.prof"
go test -bench=BenchmarkTraceAggregateCreation -benchmem -cpuprofile="${CPU_PROFILE}" "${BENCHMARK_DIR}" || {
    log_error "CPU profile generation failed"
    exit 1
}

# Generate memory profile
log_info "Generating memory profile..."
MEM_PROFILE="${PROFILES_DIR}/mem.prof"
go test -bench=BenchmarkArenaSpanAllocation -benchmem -memprofile="${MEM_PROFILE}" "${BENCHMARK_DIR}" || {
    log_error "Memory profile generation failed"
    exit 1
}

# Generate blocking profile
log_info "Generating blocking profile..."
BLOCK_PROFILE="${PROFILES_DIR}/block.prof"
go test -bench=BenchmarkRingBufferOperations -benchmem -blockprofile="${BLOCK_PROFILE}" "${BENCHMARK_DIR}" || {
    log_error "Blocking profile generation failed"
    exit 1
}

# Step 4: Generate comprehensive profile with realistic workload
log_info "Generating comprehensive profile with realistic workload..."
COMPREHENSIVE_PROFILE="${PROFILES_DIR}/comprehensive.prof"
go test -bench=BenchmarkPGO -benchmem -benchtime=10s -cpuprofile="${COMPREHENSIVE_PROFILE}" "${BENCHMARK_DIR}" || {
    log_warning "Comprehensive profile generation had issues, continuing..."
}

# Step 5: Create PGO profile (default.pgo)
log_info "Creating PGO profile for compilation..."
PGO_PROFILE="${PGO_DIR}/default.pgo"

# Use the CPU profile as the PGO profile
if [[ -f "${CPU_PROFILE}" ]]; then
    cp "${CPU_PROFILE}" "${PGO_PROFILE}"
    log_success "PGO profile created: ${PGO_PROFILE}"
else
    log_error "CPU profile not found, cannot create PGO profile"
    exit 1
fi

# Step 6: Build with PGO enabled
log_info "Building with PGO enabled..."
cd "${PROJECT_ROOT}"

# Set PGO environment
export GOPGODIR="${PGO_DIR}"

# Build the main binary with PGO
go build -pgo="${PGO_PROFILE}" -o "${PROJECT_ROOT}/bin/tapio-pgo" ./cmd/tapio || {
    log_error "PGO build failed"
    exit 1
}
log_success "PGO build completed: ${PROJECT_ROOT}/bin/tapio-pgo"

# Step 7: Run benchmarks with PGO-optimized build
log_info "Running benchmarks with PGO-optimized build..."
PGO_RESULTS="${PROFILES_DIR}/pgo-optimized.txt"

# Set up environment for PGO benchmarks
export CGO_ENABLED=1
export GOFLAGS="-pgo=${PGO_PROFILE}"

go test -bench=. -benchmem -count=3 "${BENCHMARK_DIR}" > "${PGO_RESULTS}" 2>&1 || {
    log_error "PGO benchmarks failed"
    exit 1
}
log_success "PGO benchmarks completed"

# Step 8: Compare results
log_info "Comparing baseline vs PGO performance..."
COMPARISON_REPORT="${PROFILES_DIR}/pgo-comparison.txt"

cat > "${COMPARISON_REPORT}" << EOF
# Profile-Guided Optimization (PGO) Performance Comparison
# Generated: $(date)

## Baseline Results (without PGO):
EOF

grep "^Benchmark" "${BASELINE_RESULTS}" >> "${COMPARISON_REPORT}" || true

cat >> "${COMPARISON_REPORT}" << EOF

## PGO-Optimized Results:
EOF

grep "^Benchmark" "${PGO_RESULTS}" >> "${COMPARISON_REPORT}" || true

cat >> "${COMPARISON_REPORT}" << EOF

## Analysis:
- CPU Profile: ${CPU_PROFILE}
- Memory Profile: ${MEM_PROFILE}
- Blocking Profile: ${BLOCK_PROFILE}
- PGO Profile: ${PGO_PROFILE}

To view profiles:
  go tool pprof ${CPU_PROFILE}
  go tool pprof ${MEM_PROFILE}
  go tool pprof ${BLOCK_PROFILE}

To use PGO in builds:
  go build -pgo=${PGO_PROFILE} ./cmd/tapio
EOF

log_success "Performance comparison saved to: ${COMPARISON_REPORT}"

# Step 9: Profile analysis
log_info "Performing profile analysis..."

# Analyze CPU profile
if command -v go tool pprof >/dev/null 2>&1; then
    log_info "Generating CPU profile analysis..."
    CPU_ANALYSIS="${PROFILES_DIR}/cpu-analysis.txt"
    go tool pprof -text "${CPU_PROFILE}" > "${CPU_ANALYSIS}" 2>/dev/null || {
        log_warning "CPU profile analysis failed"
    }
    
    # Generate call graph
    CPU_CALLGRAPH="${PROFILES_DIR}/cpu-callgraph.svg"
    go tool pprof -svg "${CPU_PROFILE}" > "${CPU_CALLGRAPH}" 2>/dev/null || {
        log_warning "CPU call graph generation failed"
    }
    
    # Analyze memory profile
    log_info "Generating memory profile analysis..."
    MEM_ANALYSIS="${PROFILES_DIR}/mem-analysis.txt"
    go tool pprof -text "${MEM_PROFILE}" > "${MEM_ANALYSIS}" 2>/dev/null || {
        log_warning "Memory profile analysis failed"
    }
    
    log_success "Profile analysis completed"
else
    log_warning "go tool pprof not available, skipping profile analysis"
fi

# Step 10: Generate optimization recommendations
log_info "Generating optimization recommendations..."
RECOMMENDATIONS="${PROFILES_DIR}/optimization-recommendations.md"

cat > "${RECOMMENDATIONS}" << EOF
# Profile-Guided Optimization (PGO) Recommendations

## Generated Profiles
- **CPU Profile**: ${CPU_PROFILE}
- **Memory Profile**: ${MEM_PROFILE}
- **Blocking Profile**: ${BLOCK_PROFILE}
- **PGO Profile**: ${PGO_PROFILE}

## Build Commands

### Standard Build
\`\`\`bash
go build ./cmd/tapio
\`\`\`

### PGO-Optimized Build
\`\`\`bash
go build -pgo=${PGO_PROFILE} ./cmd/tapio
\`\`\`

## Profile Analysis Commands

### View CPU Hotspots
\`\`\`bash
go tool pprof ${CPU_PROFILE}
# In pprof: top10, list <function_name>
\`\`\`

### View Memory Usage
\`\`\`bash
go tool pprof ${MEM_PROFILE}
# In pprof: top10, list <function_name>
\`\`\`

### Generate Web Interface
\`\`\`bash
go tool pprof -http=:8080 ${CPU_PROFILE}
\`\`\`

## Benchmark Comparison
- **Baseline**: ${BASELINE_RESULTS}
- **PGO-Optimized**: ${PGO_RESULTS}
- **Comparison**: ${COMPARISON_REPORT}

## Next Steps

1. **Review Hot Paths**: Identify functions consuming most CPU time
2. **Memory Optimization**: Focus on high-allocation functions
3. **Concurrency Analysis**: Check for blocking operations
4. **SIMD Opportunities**: Look for vectorizable operations
5. **Algorithm Improvements**: Consider algorithmic optimizations

## Continuous Integration

Add PGO to your CI/CD pipeline:

\`\`\`yaml
# .github/workflows/pgo.yml
- name: Generate PGO Profile
  run: |
    go test -bench=. -cpuprofile=cpu.prof ./pkg/otel/benchmarks
    
- name: Build with PGO
  run: |
    go build -pgo=cpu.prof ./cmd/tapio
\`\`\`

## Monitoring

Track performance metrics over time:
- Benchmark execution time
- Memory allocations per operation
- CPU utilization patterns
- Throughput measurements

EOF

log_success "Optimization recommendations saved to: ${RECOMMENDATIONS}"

# Step 11: Validate PGO improvements
log_info "Validating PGO improvements..."

# Extract performance numbers for comparison
extract_benchmark_time() {
    local file="$1"
    local benchmark="$2"
    grep "^${benchmark}" "$file" | awk '{print $3}' | sed 's/ns\/op//' | head -1
}

# Compare key benchmarks
if [[ -f "${BASELINE_RESULTS}" && -f "${PGO_RESULTS}" ]]; then
    VALIDATION_REPORT="${PROFILES_DIR}/pgo-validation.txt"
    
    cat > "${VALIDATION_REPORT}" << EOF
# PGO Validation Report
# Generated: $(date)

EOF
    
    for benchmark in "BenchmarkTraceAggregateCreation" "BenchmarkSpanCreation" "BenchmarkArenaSpanAllocation"; do
        baseline_time=$(extract_benchmark_time "${BASELINE_RESULTS}" "${benchmark}")
        pgo_time=$(extract_benchmark_time "${PGO_RESULTS}" "${benchmark}")
        
        if [[ -n "${baseline_time}" && -n "${pgo_time}" ]]; then
            improvement=$(echo "scale=2; (${baseline_time} - ${pgo_time}) / ${baseline_time} * 100" | bc -l 2>/dev/null || echo "N/A")
            
            cat >> "${VALIDATION_REPORT}" << EOF
## ${benchmark}
- Baseline: ${baseline_time} ns/op
- PGO: ${pgo_time} ns/op
- Improvement: ${improvement}%

EOF
        fi
    done
    
    log_success "PGO validation report saved to: ${VALIDATION_REPORT}"
fi

# Summary
log_success "Profile-Guided Optimization (PGO) process completed successfully!"
echo
log_info "Generated files:"
echo "  📊 Baseline results: ${BASELINE_RESULTS}"
echo "  📊 PGO results: ${PGO_RESULTS}"
echo "  📈 Comparison: ${COMPARISON_REPORT}"
echo "  🎯 PGO Profile: ${PGO_PROFILE}"
echo "  📝 Recommendations: ${RECOMMENDATIONS}"
echo "  ✅ Validation: ${VALIDATION_REPORT:-"N/A"}"
echo
log_info "To use PGO in your builds:"
echo "  go build -pgo=${PGO_PROFILE} ./cmd/tapio"
echo
log_info "To view profile in browser:"
echo "  go tool pprof -http=:8080 ${CPU_PROFILE}"

exit 0