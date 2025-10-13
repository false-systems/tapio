package bpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 Network ./network_monitor.c -- -I. -Wall -Werror
