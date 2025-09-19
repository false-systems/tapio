package bpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 -cc clang StatusMonitor ../bpf_src/status_monitor.c -- -I../../bpf_common -g -O2 -Wall -Wextra -Wno-compare-distinct-pointer-types
