package bpf

// CO-RE eBPF generation for Network Observer
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 -cc clang Network ../bpf_src/network_monitor.c -- -I../../bpf_common -g -O2 -Wall -Wextra -Wno-compare-distinct-pointer-types
