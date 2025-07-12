#!/bin/bash

# ==========================================
# Tapio Pre-commit Setup Script
# ==========================================

set -euo pipefail

echo "🔧 Setting up Tapio development environment..."

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Helper functions
log_info() {
    echo -e "${BLUE}ℹ️  $1${NC}"
}

log_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

log_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

log_error() {
    echo -e "${RED}❌ $1${NC}"
}

# Check if running in git repository
if ! git rev-parse --git-dir > /dev/null 2>&1; then
    log_error "Not in a git repository!"
    exit 1
fi

# Check system dependencies
log_info "Checking system dependencies..."

# Check Python
if ! command -v python3 &> /dev/null; then
    log_error "Python 3 is required but not installed"
    exit 1
fi
log_success "Python 3 found"

# Check Go
if ! command -v go &> /dev/null; then
    log_error "Go is required but not installed"
    exit 1
fi
log_success "Go $(go version | cut -d' ' -f3) found"

# Install pre-commit if not installed
if ! command -v pre-commit &> /dev/null; then
    log_info "Installing pre-commit..."
    if command -v pip3 &> /dev/null; then
        pip3 install pre-commit
    elif command -v brew &> /dev/null; then
        brew install pre-commit
    else
        log_error "Cannot install pre-commit. Please install it manually:"
        echo "  pip3 install pre-commit"
        echo "  # or"
        echo "  brew install pre-commit"
        exit 1
    fi
fi
log_success "Pre-commit available"

# Install Go development tools
log_info "Installing Go development tools..."
make install-tools || {
    log_warning "Failed to install tools via Makefile, trying manual installation..."
    
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.55.2
    go install golang.org/x/tools/cmd/goimports@latest
    go install github.com/securego/gosec/v2/cmd/gosec@latest
    go install sigs.k8s.io/kind@latest
    go install github.com/sonatypecommunity/nancy@latest
}
log_success "Go tools installed"

# Install pre-commit hooks
log_info "Installing pre-commit hooks..."
pre-commit install
pre-commit install --hook-type commit-msg
log_success "Pre-commit hooks installed"

# Create secrets baseline if it doesn't exist
if [[ ! -f .secrets.baseline ]]; then
    log_info "Creating secrets baseline..."
    detect-secrets scan --baseline .secrets.baseline || {
        log_warning "Could not create secrets baseline (detect-secrets not available)"
        echo "{}" > .secrets.baseline
    }
    log_success "Secrets baseline created"
fi

# Run initial quality check
log_info "Running initial quality check..."
if make ci-quality; then
    log_success "Initial quality check passed"
else
    log_warning "Initial quality check failed - run 'make fmt' to fix formatting"
fi

# Test pre-commit hooks
log_info "Testing pre-commit hooks..."
if pre-commit run --all-files --show-diff-on-failure; then
    log_success "Pre-commit hooks test passed"
else
    log_warning "Pre-commit hooks found issues - they have been auto-fixed where possible"
fi

# Setup git hooks for commit message validation
cat > .git/hooks/commit-msg << 'EOF'
#!/bin/bash

# Tapio commit message validation
# Format: type(scope): description
#
# Examples:
#   feat(cli): add new command for health checking
#   fix(ebpf): resolve memory leak in event processing
#   docs(readme): update installation instructions

commit_regex='^(feat|fix|docs|style|refactor|perf|test|chore)(\(.+\))?: .{1,50}'

if ! grep -qE "$commit_regex" "$1"; then
    echo "❌ Invalid commit message format!"
    echo ""
    echo "Format: type(scope): description"
    echo ""
    echo "Types: feat, fix, docs, style, refactor, perf, test, chore"
    echo "Scope: component being changed (optional)"
    echo "Description: brief description (1-50 chars)"
    echo ""
    echo "Examples:"
    echo "  feat(cli): add new health check command"
    echo "  fix(ebpf): resolve memory leak in events"
    echo "  docs: update installation guide"
    exit 1
fi
EOF

chmod +x .git/hooks/commit-msg
log_success "Commit message validation enabled"

# Create development environment summary
cat << EOF

🎉 Development environment setup complete!

Quick Start:
  make help              # Show all available commands
  make dev               # Run development cycle (format, lint, test, build)
  make ci                # Run full CI pipeline locally
  make pr-ready          # Prepare for pull request

Quality Gates:
  ✅ Code formatting (gofmt, goimports)
  ✅ Linting (golangci-lint with 20+ linters)
  ✅ Security scanning (gosec)
  ✅ Pre-commit hooks installed
  ✅ Commit message validation

CI Pipeline:
  Stage 1: Quality checks (< 3 minutes)
  Stage 2: Multi-platform builds (< 5 minutes)  
  Stage 3: Test execution (< 10 minutes)
  Stage 4: Security scanning

Agent Workflow:
  make agent-start       # Start new agent task
  make agent-status      # Show agent work status

Development Flow:
  1. make fmt            # Format code
  2. make dev            # Development cycle
  3. git commit          # Commit with validation
  4. make pr-ready       # Final PR preparation

Happy coding! 🚀

EOF

log_success "Setup complete! Read the summary above for next steps."