package bpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 -cc clang namespaceMonitor ../bpf_src/namespace_monitor.c -- -I../../bpf_common -I../bpf_src -g -O2 -Wall -Wextra
