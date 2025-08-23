package bpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 syscallmonitor ../bpf_src/syscall_monitor.c -- -I../bpf_src
