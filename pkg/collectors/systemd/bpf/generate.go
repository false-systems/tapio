package bpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 -cc clang systemdMonitor systemd_monitor.c -- -I../../bpf_common -g -O2 -Wall -Wextra
