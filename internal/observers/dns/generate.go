package dns

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags $BPF_CFLAGS -target amd64,arm64 DNS ./bpf_src/dns.c -- -I../bpf_common
