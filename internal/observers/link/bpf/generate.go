//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 LinkMonitor ../bpf_src/link_monitor.c

package bpf
