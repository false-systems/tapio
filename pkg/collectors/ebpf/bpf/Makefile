# eBPF Program Build Configuration with CO-RE Support
CLANG ?= clang
LLVM_STRIP ?= llvm-strip
BPFTOOL ?= bpftool
ARCH := $(shell uname -m | sed 's/x86_64/x86/' | sed 's/aarch64/arm64/')

# Output directory
OUTPUT_DIR := ..

# Include directories
INCLUDES := -I. -I./headers

# CO-RE compilation flags
BPF_CFLAGS := -g -O2 -target bpf -D__TARGET_ARCH_$(ARCH)
BPF_CFLAGS += -I/usr/include/$(shell uname -m)-linux-gnu
BPF_CFLAGS += -Wall -Wno-unused-value -Wno-pointer-sign
BPF_CFLAGS += -Wno-compare-distinct-pointer-types
BPF_CFLAGS += -Wno-gnu-variable-sized-type-not-at-end
BPF_CFLAGS += -Wno-address-of-packed-member

# BTF flags for CO-RE
BTF_FLAGS := -g

.PHONY: all clean unified legacy check-deps vmlinux

# Default target builds unified CO-RE program
all: check-deps unified

# Check build dependencies
check-deps:
	@echo "Checking dependencies..."
	@which $(CLANG) > /dev/null || (echo "Error: clang not found" && exit 1)
	@which $(LLVM_STRIP) > /dev/null || (echo "Error: llvm-strip not found" && exit 1)
	@which $(BPFTOOL) > /dev/null || (echo "Error: bpftool not found" && exit 1)
	@which go > /dev/null || (echo "Error: go not found" && exit 1)
	@go list -m github.com/cilium/ebpf > /dev/null || (echo "Error: cilium/ebpf not found" && exit 1)

# Generate vmlinux.h for CO-RE if it doesn't exist
headers/vmlinux.h:
	@echo "Generating vmlinux.h for CO-RE..."
	@mkdir -p headers
	@if [ -f /sys/kernel/btf/vmlinux ]; then \
		$(BPFTOOL) btf dump file /sys/kernel/btf/vmlinux format c > headers/vmlinux.h; \
	else \
		echo "Warning: /sys/kernel/btf/vmlinux not found. Using pre-generated vmlinux.h"; \
	fi

# Build unified CO-RE program
unified: headers/vmlinux.h
	@echo "Building unified CO-RE eBPF program..."
	@$(CLANG) $(BPF_CFLAGS) -c unified.c -o unified.bpf.o
	@$(LLVM_STRIP) -g unified.bpf.o
	
	@echo "Generating Go bindings..."
	@cd $(OUTPUT_DIR) && GOPACKAGE=ebpf go run github.com/cilium/ebpf/cmd/bpf2go \
		-cc $(CLANG) \
		-cflags "$(BPF_CFLAGS) $(INCLUDES)" \
		-target amd64,arm64 \
		-type event \
		unified bpf/unified.c -- $(INCLUDES)


# Clean build artifacts
clean:
	@echo "Cleaning eBPF objects..."
	@rm -f *.o
	@rm -f unified.bpf.o
	@rm -f $(OUTPUT_DIR)/unified_bpfel*.go
	@rm -f $(OUTPUT_DIR)/unified_bpfel*.o
	@rm -rf headers/vmlinux.h


# Test CO-RE compatibility
test-core: unified
	@echo "Testing CO-RE compatibility..."
	@if [ -f /sys/kernel/btf/vmlinux ]; then \
		echo "BTF available: CO-RE should work"; \
		$(BPFTOOL) btf dump file unified.bpf.o | head -20; \
	else \
		echo "Warning: BTF not available on this system"; \
	fi

# Debug info
info:
	@echo "Build Configuration:"
	@echo "===================="
	@echo "ARCH:        $(ARCH)"
	@echo "CLANG:       $(CLANG)"
	@echo "BPFTOOL:     $(BPFTOOL)"
	@echo "BPF_CFLAGS:  $(BPF_CFLAGS)"
	@echo ""
	@echo "Kernel BTF:"
	@ls -la /sys/kernel/btf/ 2>/dev/null || echo "BTF not available"

help:
	@echo "eBPF Build System with CO-RE Support"
	@echo "===================================="
	@echo ""
	@echo "Targets:"
	@echo "  all         - Build unified CO-RE program (default)"
	@echo "  unified     - Build unified CO-RE eBPF program"
	@echo "  clean       - Clean build artifacts"
	@echo "  check-deps  - Check build dependencies"
	@echo "  test-core   - Test CO-RE compatibility"
	@echo "  info        - Show build configuration"
	@echo ""
	@echo "CO-RE Benefits:"
	@echo "  - Single binary works across kernel versions"
	@echo "  - No need for kernel headers at runtime"
	@echo "  - Smaller binary size (~200KB vs 5.8MB)"
	@echo "  - Better performance"
	@echo "  - K8s-focused minimal collection"