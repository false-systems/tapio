package dns

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags $BPF_CFLAGS -target amd64,arm64 dnsMonitor ./bpf_src/dns_monitor.c -- -I../bpf_common
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags $BPF_CFLAGS -target amd64,arm64 dnsSniffer ./bpf_src/dns_sniffer.c -- -I../bpf_common
