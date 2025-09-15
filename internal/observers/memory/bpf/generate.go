package bpf

// CO-RE eBPF generation for Memory Observer
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 -cc clang Memory ../bpf_src/memory.c -- -I../../bpf_common -g -O2 -Wall -Wextra -Wno-compare-distinct-pointer-types
