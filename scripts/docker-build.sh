#!/bin/bash
# Docker-based build script for Tapio

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Default values
BUILD_TYPE="standard"
OUTPUT_DIR="bin"
IMAGE_NAME="tapio-builder"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --ebpf)
            BUILD_TYPE="ebpf"
            shift
            ;;
        --all)
            BUILD_TYPE="all"
            shift
            ;;
        --output)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 [options]"
            echo "Options:"
            echo "  --ebpf      Build with eBPF support (Linux only)"
            echo "  --all       Build all platform binaries"
            echo "  --output    Output directory (default: bin)"
            echo "  --help      Show this help message"
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

# Create output directory
mkdir -p "$OUTPUT_DIR"

echo -e "${GREEN}🐳 Building Tapio with Docker...${NC}"
echo "Build type: $BUILD_TYPE"
echo "Output directory: $OUTPUT_DIR"

# Build the Docker image
echo -e "${YELLOW}Building Docker image...${NC}"
docker build -t "$IMAGE_NAME" --target go-builder .

# Function to build a specific target
build_target() {
    local os=$1
    local arch=$2
    local tags=$3
    local suffix=$4
    
    echo -e "${YELLOW}Building $os/$arch$suffix...${NC}"
    
    docker run --rm \
        -v "$PWD/$OUTPUT_DIR:/output" \
        -e TARGETOS="$os" \
        -e TARGETARCH="$arch" \
        -e BUILD_TAGS="$tags" \
        "$IMAGE_NAME" \
        sh -c "CGO_ENABLED=0 GOOS=\$TARGETOS GOARCH=\$TARGETARCH \
               go build -tags \"\$BUILD_TAGS\" \
               -ldflags=\"-w -s -X github.com/yairfalse/tapio/internal/cli.version=docker\" \
               -o /output/tapio-\$TARGETOS-\$TARGETARCH$suffix ./cmd/tapio"
}

# Build based on type
case $BUILD_TYPE in
    standard)
        build_target "linux" "amd64" "" ""
        ;;
    ebpf)
        build_target "linux" "amd64" "ebpf" "-ebpf"
        ;;
    all)
        build_target "linux" "amd64" "" ""
        build_target "linux" "amd64" "ebpf" "-ebpf"
        build_target "darwin" "amd64" "" ""
        build_target "darwin" "arm64" "" ""
        build_target "windows" "amd64" "" ".exe"
        ;;
esac

echo -e "${GREEN}✅ Build complete!${NC}"
echo "Binaries available in $OUTPUT_DIR:"
ls -la "$OUTPUT_DIR"/tapio-*