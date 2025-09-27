# Build eBPF programs for DNS monitoring

CLANG ?= clang
LLC ?= llc
STRIP ?= llvm-strip

ARCH := $(shell uname -m | sed 's/x86_64/x86/')

BPF_CFLAGS := -target bpf \
	-D__TARGET_ARCH_$(ARCH) \
	-D__KERNEL__ \
	-O2 -g \
	-Wall -Werror \
	-I/usr/include \
	-I/usr/include/linux \
	-I/usr/include/x86_64-linux-gnu

.PHONY: all clean

all: dns_monitor.o

dns_monitor.o: dns_monitor.c
	$(CLANG) $(BPF_CFLAGS) -c $< -o $@
	$(STRIP) -g $@

clean:
	rm -f *.o *.ll

# Generate Go bindings
generate:
	go generate ../

# Test compilation
test-compile: dns_monitor.o
	@echo "eBPF program compiled successfully"
	@file dns_monitor.o