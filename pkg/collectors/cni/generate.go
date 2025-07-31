package cni

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 cniMonitor ./bpf/cni_monitor.c -- -I./bpf
