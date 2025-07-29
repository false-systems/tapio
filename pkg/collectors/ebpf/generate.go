package ebpf

// Unified CO-RE program generation
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -target bpf" -target amd64,arm64 -type event unified bpf/unified.c -- -I./bpf -I./bpf/headers
