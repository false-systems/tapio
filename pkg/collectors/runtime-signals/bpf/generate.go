package bpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 -cc clang runtimemonitor ../bpf_src/runtime_monitor.c -- -I../../bpf_common -g -O2 -Wall -Wextra