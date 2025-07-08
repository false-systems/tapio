# Tapio eBPF Testing Makefile

.PHONY: setup test test-quick clean help

# Default target
help:
	@echo "🧪 Tapio eBPF Testing Suite"
	@echo "==========================="
	@echo ""
	@echo "Available targets:"
	@echo "  setup      - Set up test environment (Minikube + eBPF deps)"
	@echo "  test       - Run complete test suite (~20 minutes)"
	@echo "  test-quick - Run quick memory leak test only (~5 minutes)"
	@echo "  clean      - Clean up test resources"
	@echo "  help       - Show this help message"
	@echo ""
	@echo "Requirements:"
	@echo "  • Linux with kernel 4.15+ (for eBPF)"
	@echo "  • Minikube or other Kubernetes cluster"
	@echo "  • Root privileges (for eBPF access)"
	@echo ""
	@echo "Quick start:"
	@echo "  sudo make setup"
	@echo "  sudo make test"

# Setup test environment
setup:
	@echo "🚀 Setting up eBPF test environment..."
	@if [ "$$(id -u)" != "0" ]; then \
		echo "❌ Setup requires root privileges. Run with sudo."; \
		exit 1; \
	fi
	./setup-test-env.sh

# Run complete test suite
test:
	@echo "🧪 Running complete eBPF test suite..."
	@if [ "$$(id -u)" != "0" ]; then \
		echo "❌ Tests require root privileges. Run with sudo."; \
		exit 1; \
	fi
	./run-all-tests.sh

# Run quick memory leak test only
test-quick:
	@echo "⚡ Running quick memory leak test..."
	@if [ "$$(id -u)" != "0" ]; then \
		echo "❌ Tests require root privileges. Run with sudo."; \
		exit 1; \
	fi
	./test-memory-leak.sh

# Clean up test resources
clean:
	@echo "🧹 Cleaning up test resources..."
	kubectl delete namespace tapio-ebpf-tests --ignore-not-found=true
	@echo "✅ Cleanup complete"

# Individual test targets
test-memory-leak:
	@if [ "$$(id -u)" != "0" ]; then \
		echo "❌ Tests require root privileges. Run with sudo."; \
		exit 1; \
	fi
	./test-memory-leak.sh

test-oom-prediction:
	@if [ "$$(id -u)" != "0" ]; then \
		echo "❌ Tests require root privileges. Run with sudo."; \
		exit 1; \
	fi
	./test-oom-prediction.sh

# Check prerequisites
check-prereqs:
	@echo "🔍 Checking prerequisites..."
	@command -v minikube >/dev/null 2>&1 || { echo "❌ minikube required but not installed."; exit 1; }
	@command -v kubectl >/dev/null 2>&1 || { echo "❌ kubectl required but not installed."; exit 1; }
	@command -v docker >/dev/null 2>&1 || { echo "❌ docker required but not installed."; exit 1; }
	@if [ "$$(uname)" != "Linux" ]; then \
		echo "❌ eBPF testing requires Linux"; \
		exit 1; \
	fi
	@echo "✅ All prerequisites satisfied"