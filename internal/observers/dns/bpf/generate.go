package bpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 DNS ../bpf_src/dns.c -- -I../../bpf_common -O2 -Wall
