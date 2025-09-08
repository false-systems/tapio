package bpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Wextra" -target amd64,arm64 servicemonitor ../bpf_src/service_monitor.c -- -I../../bpf_common -I../bpf_src
