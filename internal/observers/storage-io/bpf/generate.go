package bpf

// CO-RE eBPF generation for Storage I/O Observer
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 -cc clang Storage ../bpf_src/storage.c -- -I../../bpf_common -g -O2 -Wall -Wextra -Wno-compare-distinct-pointer-types -mllvm -bpf-stack-size=8192
