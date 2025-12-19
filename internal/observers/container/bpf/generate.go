package bpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 Container ./container_monitor.c -- -I. -I../../../base/bpf -I../../../observers/common/bpf/lib -Wall -Werror
