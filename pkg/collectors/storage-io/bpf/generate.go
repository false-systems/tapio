package bpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 storagemonitor ../bpf_src/storage_monitor.c -- -I../../bpf_common
