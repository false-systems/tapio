# MEGA simple Makefile that matches CI
.PHONY: build test lint ci clean build-ebpf test-ebpf lint-ebpf

# Default build without eBPF
build:
	go build ./cmd/tapio

# Build with eBPF support (Linux only)
build-ebpf:
	go build -tags ebpf ./cmd/tapio

# Default tests without eBPF
test:
	go test -short ./...

# Tests with eBPF support
test-ebpf:
	go test -short -tags ebpf ./...

# Default lint
lint:
	gofmt -l . | grep -v vendor | tee fmt-issues.txt && test ! -s fmt-issues.txt
	go vet ./...

# Lint with eBPF build tags
lint-ebpf:
	gofmt -l . | grep -v vendor | tee fmt-issues.txt && test ! -s fmt-issues.txt
	go vet -tags ebpf ./...

# Default CI checks
ci: lint build test
	@echo "[OK] CI checks passed!"

# CI checks with eBPF
ci-ebpf: lint-ebpf build-ebpf test-ebpf
	@echo "[OK] CI checks with eBPF passed!"

clean:
	rm -f tapio fmt-issues.txt