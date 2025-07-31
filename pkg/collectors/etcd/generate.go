package etcd

// Generate eBPF programs
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -target bpf" -target amd64,arm64 etcdMonitor bpf/etcd_monitor.c -- -I./bpf -I./bpf/headers
