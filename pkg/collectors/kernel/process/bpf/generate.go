package bpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall" -target amd64,arm64 processmonitor ../bpf_src/process_monitor.c -- -I../../../bpf_common -I../bpf_src
