package bpf

// CO-RE eBPF generation for DNS Observer
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 -cc clang DNS ../bpf_src/dns.c -- -I../../bpf_common -g -O2 -Wall -Wextra -Wno-compare-distinct-pointer-types -mllvm -bpf-stack-size=8192
