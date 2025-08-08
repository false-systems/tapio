#!/bin/bash

# Tapio Developer Environment Setup Script
# Sets up complete development environment with all tools

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🚀 Tapio Developer Environment Setup${NC}"
echo -e "${BLUE}=====================================>${NC}"

# Check Go version
echo -e "\n${YELLOW}Checking Go installation...${NC}"
if ! command -v go &> /dev/null; then
    echo -e "${RED}❌ Go is not installed${NC}"
    echo "Please install Go 1.21+ from https://golang.org/dl/"
    exit 1
fi

GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
echo -e "${GREEN}✅ Go ${GO_VERSION} installed${NC}"

# Install Go tools
echo -e "\n${YELLOW}Installing Go development tools...${NC}"

tools=(
    "golang.org/x/tools/cmd/goimports@latest"
    "github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
    "github.com/cilium/ebpf/cmd/bpf2go@latest"
    "golang.org/x/vuln/cmd/govulncheck@latest"
    "github.com/securego/gosec/v2/cmd/gosec@latest"
)

for tool in "${tools[@]}"; do
    echo -e "  Installing $(basename $tool | cut -d@ -f1)..."
    go install "$tool" 2>/dev/null || echo -e "    ${YELLOW}Warning: Failed to install $tool${NC}"
done

# Install pre-commit if available
echo -e "\n${YELLOW}Setting up pre-commit hooks...${NC}"
if command -v pre-commit &> /dev/null; then
    pre-commit install
    echo -e "${GREEN}✅ Pre-commit hooks installed${NC}"
else
    echo -e "${YELLOW}⚠️  pre-commit not installed. Install with: pip install pre-commit${NC}"
fi

# Check for required system tools
echo -e "\n${YELLOW}Checking system dependencies...${NC}"

# Check for clang (needed for eBPF)
if command -v clang &> /dev/null; then
    CLANG_VERSION=$(clang --version | head -1)
    echo -e "${GREEN}✅ Clang installed: ${CLANG_VERSION}${NC}"
else
    echo -e "${YELLOW}⚠️  Clang not installed (needed for eBPF development)${NC}"
    echo "  Install with:"
    echo "    macOS: brew install llvm"
    echo "    Linux: sudo apt-get install clang llvm libelf-dev"
fi

# Download Go dependencies
echo -e "\n${YELLOW}Downloading Go dependencies...${NC}"
go mod download
echo -e "${GREEN}✅ Dependencies downloaded${NC}"

# Create necessary directories
echo -e "\n${YELLOW}Creating project directories...${NC}"
mkdir -p build coverage .cache

# Run initial verification
echo -e "\n${YELLOW}Running initial verification...${NC}"
echo -e "  Checking formatting..."
UNFORMATTED=$(gofmt -l . | grep -v vendor | wc -l)
if [ "$UNFORMATTED" -ne "0" ]; then
    echo -e "  ${YELLOW}Warning: $UNFORMATTED files need formatting${NC}"
    echo "  Run: make fmt"
else
    echo -e "  ${GREEN}✅ Code is formatted${NC}"
fi

echo -e "  Checking build..."
if go build ./... 2>/dev/null; then
    echo -e "  ${GREEN}✅ Build successful${NC}"
else
    echo -e "  ${YELLOW}⚠️  Build has issues (some packages may have missing dependencies)${NC}"
fi

# Display helpful commands
echo -e "\n${GREEN}✅ Development environment setup complete!${NC}"
echo -e "\n${BLUE}Useful commands:${NC}"
echo -e "  ${GREEN}make help${NC}         - Show all available commands"
echo -e "  ${GREEN}make fmt${NC}          - Format code"
echo -e "  ${GREEN}make build${NC}        - Build all packages"
echo -e "  ${GREEN}make test${NC}         - Run tests"
echo -e "  ${GREEN}make ci-quick${NC}     - Quick CI check"
echo -e "  ${GREEN}make verify${NC}       - Run all verifications"

echo -e "\n${BLUE}Architecture commands:${NC}"
echo -e "  ${GREEN}make verify-architecture${NC}  - Check 5-level hierarchy"
echo -e "  ${GREEN}make debug-deps${NC}           - Debug dependency issues"
echo -e "  ${GREEN}make check-collector COLLECTOR=cni${NC} - Check specific collector"

echo -e "\n${YELLOW}📚 Remember the architecture hierarchy:${NC}"
echo -e "  Level 0: pkg/domain/       (zero dependencies)"
echo -e "  Level 1: pkg/collectors/   (domain only)"
echo -e "  Level 2: pkg/intelligence/ (domain + L1)"
echo -e "  Level 3: pkg/integrations/ (domain + L1 + L2)"
echo -e "  Level 4: pkg/interfaces/   (all above)"

echo -e "\n${YELLOW}⚠️  CRITICAL: Always run 'make fmt' before committing!${NC}"