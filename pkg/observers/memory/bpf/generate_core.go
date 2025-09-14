package bpf

// CO-RE compliant bpf2go generation - Per CLAUDE.md standards
// NO STUBS - Complete implementation only

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target arm64 -cc clang Memorymonitor_core ../bpf_src/memory_monitor_core_simple.c -- -I../../bpf_common -g -O2 -Wall -Wextra -D__TARGET_ARCH_arm64 -Wno-compare-distinct-pointer-types

// The -target bpf flag enables CO-RE (Compile Once Run Everywhere)
// This generates BTF-enabled BPF bytecode that works across kernel versions
// Requirements:
// - clang-14 or later with BTF support
// - Kernel 5.4+ with CONFIG_DEBUG_INFO_BTF=y
// - libbpf for CO-RE relocations
