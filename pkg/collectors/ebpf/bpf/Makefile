# eBPF Program Build Configuration
CLANG ?= clang
LLC ?= llc
LLVM_STRIP ?= llvm-strip

# Build flags
CFLAGS := -O2 -g -Wall

# Target architectures
ARCHS := amd64 arm64

# eBPF programs to build
PROGRAMS := network_monitor packet_analyzer dns_monitor protocol_analyzer oom_detector memory_tracker http_tracer grpc_tracer

# Output directory
OUTPUT_DIR := ..

# Include directories
INCLUDES := -I. -I/usr/include -I/usr/include/bpf -I/usr/include/x86_64-linux-gnu

# BPF target flags
BPF_CFLAGS := $(CFLAGS) \
	-target bpf \
	-D__TARGET_ARCH_x86 \
	-D__BPF_TRACING__ \
	$(INCLUDES)

.PHONY: all clean generate

all: generate

generate:
	@echo "Generating eBPF Go bindings..."
	@cd $(OUTPUT_DIR) && for prog in $(PROGRAMS); do \
		echo "Building $$prog..."; \
		$(CLANG) $(BPF_CFLAGS) -c bpf/$$prog.c -o bpf/$$prog.o; \
		GOPACKAGE=ebpf go run github.com/cilium/ebpf/cmd/bpf2go \
			-cc $(CLANG) \
			-cflags "$(BPF_CFLAGS)" \
			-target amd64,arm64 \
			$$prog bpf/$$prog.c -- $(INCLUDES); \
	done

clean:
	@echo "Cleaning eBPF objects..."
	@rm -f *.o
	@rm -f $(OUTPUT_DIR)/*_bpfel.go
	@rm -f $(OUTPUT_DIR)/*_bpfeb.go
	@rm -f $(OUTPUT_DIR)/*_bpfel.o
	@rm -f $(OUTPUT_DIR)/*_bpfeb.o

# Individual program targets
network_monitor:
	@echo "Building network_monitor..."
	@$(CLANG) $(BPF_CFLAGS) -c network_monitor.c -o network_monitor.o
	@cd $(OUTPUT_DIR) && GOPACKAGE=ebpf go run github.com/cilium/ebpf/cmd/bpf2go \
		-cc $(CLANG) \
		-cflags "$(BPF_CFLAGS)" \
		-target amd64,arm64 \
		networkmonitor bpf/network_monitor.c -- $(INCLUDES)

packet_analyzer:
	@echo "Building packet_analyzer..."
	@$(CLANG) $(BPF_CFLAGS) -c packet_analyzer.c -o packet_analyzer.o
	@cd $(OUTPUT_DIR) && GOPACKAGE=ebpf go run github.com/cilium/ebpf/cmd/bpf2go \
		-cc $(CLANG) \
		-cflags "$(BPF_CFLAGS)" \
		-target amd64,arm64 \
		packetanalyzer bpf/packet_analyzer.c -- $(INCLUDES)

dns_monitor:
	@echo "Building dns_monitor..."
	@$(CLANG) $(BPF_CFLAGS) -c dns_monitor.c -o dns_monitor.o
	@cd $(OUTPUT_DIR) && GOPACKAGE=ebpf go run github.com/cilium/ebpf/cmd/bpf2go \
		-cc $(CLANG) \
		-cflags "$(BPF_CFLAGS)" \
		-target amd64,arm64 \
		dnsmonitor bpf/dns_monitor.c -- $(INCLUDES)

protocol_analyzer:
	@echo "Building protocol_analyzer..."
	@$(CLANG) $(BPF_CFLAGS) -c protocol_analyzer.c -o protocol_analyzer.o
	@cd $(OUTPUT_DIR) && GOPACKAGE=ebpf go run github.com/cilium/ebpf/cmd/bpf2go \
		-cc $(CLANG) \
		-cflags "$(BPF_CFLAGS)" \
		-target amd64,arm64 \
		protocolanalyzer bpf/protocol_analyzer.c -- $(INCLUDES)

oom_detector:
	@echo "Building oom_detector..."
	@$(CLANG) $(BPF_CFLAGS) -c oom_detector.c -o oom_detector.o
	@cd $(OUTPUT_DIR) && GOPACKAGE=ebpf go run github.com/cilium/ebpf/cmd/bpf2go \
		-cc $(CLANG) \
		-cflags "$(BPF_CFLAGS)" \
		-target amd64,arm64 \
		oomdetector bpf/oom_detector.c -- $(INCLUDES)

memory_tracker:
	@echo "Building memory_tracker..."
	@$(CLANG) $(BPF_CFLAGS) -c memory_tracker.c -o memory_tracker.o
	@cd $(OUTPUT_DIR) && GOPACKAGE=ebpf go run github.com/cilium/ebpf/cmd/bpf2go \
		-cc $(CLANG) \
		-cflags "$(BPF_CFLAGS)" \
		-target amd64,arm64 \
		memorytracker bpf/memory_tracker.c -- $(INCLUDES)

http_tracer:
	@echo "Building http_tracer..."
	@$(CLANG) $(BPF_CFLAGS) -c http_tracer.c -o http_tracer.o
	@cd $(OUTPUT_DIR) && GOPACKAGE=ebpf go run github.com/cilium/ebpf/cmd/bpf2go \
		-cc $(CLANG) \
		-cflags "$(BPF_CFLAGS)" \
		-target amd64,arm64 \
		httptracer bpf/http_tracer.c -- $(INCLUDES)

grpc_tracer:
	@echo "Building grpc_tracer..."
	@$(CLANG) $(BPF_CFLAGS) -c grpc_tracer.c -o grpc_tracer.o
	@cd $(OUTPUT_DIR) && GOPACKAGE=ebpf go run github.com/cilium/ebpf/cmd/bpf2go \
		-cc $(CLANG) \
		-cflags "$(BPF_CFLAGS)" \
		-target amd64,arm64 \
		grpctracer bpf/grpc_tracer.c -- $(INCLUDES)

# Helper targets
check-deps:
	@echo "Checking dependencies..."
	@which $(CLANG) > /dev/null || (echo "Error: clang not found" && exit 1)
	@which go > /dev/null || (echo "Error: go not found" && exit 1)
	@go list -m github.com/cilium/ebpf > /dev/null || (echo "Error: cilium/ebpf not found" && exit 1)

help:
	@echo "eBPF Build System"
	@echo "================="
	@echo ""
	@echo "Targets:"
	@echo "  all              - Build all eBPF programs"
	@echo "  generate         - Generate Go bindings for all programs"
	@echo "  clean            - Clean all build artifacts"
	@echo "  check-deps       - Check build dependencies"
	@echo ""
	@echo "Individual program targets:"
	@echo "  network_monitor  - Build network monitoring program"
	@echo "  packet_analyzer  - Build packet analysis program"
	@echo "  dns_monitor      - Build DNS monitoring program"
	@echo "  protocol_analyzer- Build protocol analysis program"
	@echo "  oom_detector     - Build OOM detection program"
	@echo "  memory_tracker   - Build memory tracking program"
	@echo "  http_tracer      - Build HTTP tracing program"
	@echo "  grpc_tracer      - Build gRPC tracing program"
	@echo ""
	@echo "Variables:"
	@echo "  CLANG           - Clang compiler (default: clang)"
	@echo "  CFLAGS          - C compiler flags"
	@echo "  OUTPUT_DIR      - Output directory for generated files"