package bpf

// Generate eBPF programs
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -target bpf" -target amd64,arm64 etcdMonitor ../bpf_src/etcd_monitor.c -- -I../bpf_src -I../bpf_src/headers
