# Makefile for etcd eBPF programs

CLANG ?= clang
LLVM_STRIP ?= llvm-strip
BPFTOOL ?= bpftool
ARCH := $(shell uname -m | sed 's/x86_64/x86/' | sed 's/aarch64/arm64/')

# Includes
INCLUDES := -I../../../../bpf/headers \
            -I/usr/include/$(shell uname -m)-linux-gnu

# BPF Flags
BPF_CFLAGS := -g -O2 -Wall -Werror \
              -target bpf \
              -D__TARGET_ARCH_$(ARCH) \
              -D__BPF_TRACING__ \
              $(INCLUDES)

# Source and targets
SOURCES := etcd_monitor.c
OBJECTS := $(SOURCES:.c=.o)

.PHONY: all clean

all: $(OBJECTS)

%.o: %.c
	$(CLANG) $(BPF_CFLAGS) -c $< -o $@
	$(LLVM_STRIP) -g $@

clean:
	rm -f *.o

# Generate vmlinux.h if needed
vmlinux.h:
	$(BPFTOOL) btf dump file /sys/kernel/btf/vmlinux format c > $@