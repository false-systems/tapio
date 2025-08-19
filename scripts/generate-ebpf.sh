#!/bin/bash
set -euo pipefail

# Enhanced eBPF Generation Script for Tapio
# Handles local development, CI, and cross-platform builds

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
readonly BPF_COLLECTORS_DIR="${PROJECT_ROOT}/pkg/collectors"

# Colors for output
readonly RED='\033[0;31m'
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly NC='\033[0m'

# Configuration
readonly REQUIRED_GO_VERSION="1.21"
readonly BPF2GO_VERSION="latest"
readonly CLANG_MIN_VERSION="14"

log() {
    echo -e "${GREEN}[eBPF Gen]${NC} $*"
}

warn() {
    echo -e "${YELLOW}[eBPF Gen]${NC} $*"
}

error() {
    echo -e "${RED}[eBPF Gen]${NC} $*" >&2
}

die() {
    error "$*"
    exit 1
}

check_platform() {
    case "$(uname -s)" in
        Linux*)
            log "Running on Linux - full eBPF generation available"
            return 0
            ;;
        Darwin*)
            warn "Running on macOS - will attempt generation or download artifacts"
            return 1
            ;;
        *)
            die "Unsupported platform: $(uname -s)"
            ;;
    esac
}

check_dependencies() {
    log "Checking dependencies..."
    
    # Check Go version
    if ! command -v go &> /dev/null; then
        die "Go is not installed"
    fi
    
    local go_version
    go_version=$(go version | awk '{print $3}' | sed 's/go//')
    if ! printf '%s\n%s\n' "${REQUIRED_GO_VERSION}" "${go_version}" | sort -V | head -n 1 | grep -q "${REQUIRED_GO_VERSION}"; then
        die "Go version ${REQUIRED_GO_VERSION} or higher required, found ${go_version}"
    fi
    
    # Check bpf2go - always reinstall in CI to avoid architecture mismatches
    if [[ "${CI:-false}" == "true" ]] || ! command -v bpf2go &> /dev/null; then
        log "Installing bpf2go for current architecture..."
        # Clear any cached version to avoid exec format errors
        rm -f "$(go env GOPATH)/bin/bpf2go" 2>/dev/null || true
        CGO_ENABLED=0 go install "github.com/cilium/ebpf/cmd/bpf2go@${BPF2GO_VERSION}"
    fi
    
    # Check clang (Linux only)
    if [[ "$(uname -s)" == "Linux" ]]; then
        if ! command -v clang &> /dev/null; then
            die "clang is required for eBPF compilation on Linux"
        fi
        
        local clang_version
        clang_version=$(clang --version | head -n1 | grep -o '[0-9]\+' | head -n1)
        if (( clang_version < CLANG_MIN_VERSION )); then
            die "clang version ${CLANG_MIN_VERSION} or higher required, found ${clang_version}"
        fi
    fi
}

find_collectors() {
    local collectors=()
    
    # Find all directories with generate.go files
    while IFS= read -r -d '' file; do
        local dir
        dir="$(dirname "${file}")"
        local collector_name
        collector_name="$(basename "$(dirname "${dir}")")"
        if [[ "${collector_name}" == "bpf" ]]; then
            collector_name="$(basename "$(dirname "$(dirname "${dir}")")")"
        fi
        collectors+=("${collector_name}:${dir}")
    done < <(find "${BPF_COLLECTORS_DIR}" -name "generate.go" -print0)
    
    printf '%s\n' "${collectors[@]}"
}

generate_collector() {
    local collector_info="$1"
    local collector_name="${collector_info%:*}"
    local bpf_dir="${collector_info#*:}"
    
    log "Generating eBPF for collector: ${collector_name}"
    log "  Directory: ${bpf_dir}"
    
    # Check if source files exist
    local src_dir
    src_dir="$(dirname "${bpf_dir}")/bpf_src"
    if [[ ! -d "${src_dir}" ]]; then
        warn "  No bpf_src directory found, skipping..."
        return 0
    fi
    
    # Generate using go generate
    pushd "${bpf_dir}" &> /dev/null
    
    # Set environment for cross-compilation if needed
    local original_goarch="${GOARCH:-}"
    local original_goos="${GOOS:-}"
    
    if [[ -n "${TARGET_ARCH:-}" ]]; then
        export GOARCH="${TARGET_ARCH}"
        export GOOS="linux"
    fi
    
    # Clean old generated files
    rm -f ./*_bpfel_*.go ./*.o
    
    # Generate
    if go generate .; then
        log "  ✅ Generated successfully"
        
        # Verify generated files
        local generated_files
        generated_files=$(find . -name "*_bpfel_*.go" -o -name "*.o" | wc -l)
        log "  📁 Generated ${generated_files} files"
    else
        error "  ❌ Generation failed"
        return 1
    fi
    
    # Restore environment
    if [[ -n "${original_goarch}" ]]; then
        export GOARCH="${original_goarch}"
    else
        unset GOARCH
    fi
    
    if [[ -n "${original_goos}" ]]; then
        export GOOS="${original_goos}"
    else
        unset GOOS
    fi
    
    popd &> /dev/null
}

download_artifacts() {
    local arch="${1:-$(uname -m)}"
    
    # Convert arch names
    case "${arch}" in
        x86_64) arch="amd64" ;;
        aarch64) arch="arm64" ;;
    esac
    
    warn "Attempting to download pre-built eBPF artifacts for ${arch}..."
    
    # Check if GitHub CLI is available
    if ! command -v gh &> /dev/null; then
        warn "GitHub CLI not available, cannot download artifacts"
        return 1
    fi
    
    # Download latest artifacts
    if gh run download --name "ebpf-bytecode-${arch}" --dir "${PROJECT_ROOT}/tmp/ebpf-artifacts" 2>/dev/null; then
        log "Downloaded artifacts, extracting..."
        
        # Extract to correct locations
        find "${PROJECT_ROOT}/tmp/ebpf-artifacts" -name "*.o" -o -name "*_bpfel_*.go" | while read -r file; do
            local relative_path
            relative_path="${file#${PROJECT_ROOT}/tmp/ebpf-artifacts/}"
            local target_dir
            target_dir="$(dirname "${PROJECT_ROOT}/${relative_path}")"
            
            mkdir -p "${target_dir}"
            cp "${file}" "${target_dir}/"
        done
        
        rm -rf "${PROJECT_ROOT}/tmp/ebpf-artifacts"
        log "✅ Artifacts extracted successfully"
        return 0
    else
        warn "Failed to download artifacts"
        return 1
    fi
}

generate_all() {
    local target_arch="${1:-}"
    local force_download="${2:-false}"
    
    # Set target architecture
    if [[ -n "${target_arch}" ]]; then
        export TARGET_ARCH="${target_arch}"
        log "Targeting architecture: ${target_arch}"
    fi
    
    # Find all collectors
    local collectors
    readarray -t collectors < <(find_collectors)
    
    if [[ ${#collectors[@]} -eq 0 ]]; then
        warn "No eBPF collectors found"
        return 0
    fi
    
    log "Found ${#collectors[@]} eBPF collectors"
    
    local failed_collectors=()
    local can_generate=true
    
    # Check if we can generate locally
    if ! check_platform || [[ "${force_download}" == "true" ]]; then
        can_generate=false
    fi
    
    # Try local generation first
    if [[ "${can_generate}" == "true" ]]; then
        for collector_info in "${collectors[@]}"; do
            if ! generate_collector "${collector_info}"; then
                failed_collectors+=("${collector_info}")
            fi
        done
    else
        # Try downloading artifacts
        if ! download_artifacts "${target_arch}"; then
            warn "Could not download artifacts, some builds may fail"
        fi
    fi
    
    # Report results
    if [[ ${#failed_collectors[@]} -gt 0 ]]; then
        error "Failed to generate for collectors:"
        printf '  - %s\n' "${failed_collectors[@]%:*}"
        return 1
    else
        log "✅ All eBPF collectors processed successfully"
    fi
}

verify_build() {
    log "Verifying build with generated eBPF files..."
    
    # Build all collector packages
    if go build -tags=linux "${BPF_COLLECTORS_DIR}/..."; then
        log "✅ Build verification successful"
    else
        error "❌ Build verification failed"
        return 1
    fi
}

main() {
    local command="${1:-generate}"
    local target_arch="${2:-}"
    local force_download="${3:-false}"
    
    case "${command}" in
        generate)
            check_dependencies
            generate_all "${target_arch}" "${force_download}"
            ;;
        download)
            download_artifacts "${target_arch}"
            ;;
        verify)
            verify_build
            ;;
        clean)
            log "Cleaning generated eBPF files..."
            find "${BPF_COLLECTORS_DIR}" -name "*_bpfel_*.go" -delete
            find "${BPF_COLLECTORS_DIR}" -name "*.o" -delete
            log "✅ Cleaned"
            ;;
        *)
            cat << 'EOF'
Usage: generate-ebpf.sh [COMMAND] [ARCH] [FORCE_DOWNLOAD]

Commands:
  generate     Generate eBPF bytecode (default)
  download     Download pre-built artifacts
  verify       Verify build with current files
  clean        Clean generated files

Architecture (optional):
  amd64        Target x86_64
  arm64        Target ARM64
  
Examples:
  ./scripts/generate-ebpf.sh                    # Generate for current platform
  ./scripts/generate-ebpf.sh generate arm64     # Generate for ARM64
  ./scripts/generate-ebpf.sh download amd64     # Download AMD64 artifacts
  ./scripts/generate-ebpf.sh generate "" true   # Force download instead of generate
EOF
            ;;
    esac
}

main "$@"