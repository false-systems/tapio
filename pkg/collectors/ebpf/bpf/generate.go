package bpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall" -target amd64,arm64 kernelmonitor ../bpf_src/kernel_monitor.c -- -I../../bpf_common -I../bpf_src
