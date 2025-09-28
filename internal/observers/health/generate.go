package health

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags $BPF_CFLAGS -target amd64,arm64 healthMonitor ./bpf_src/health_monitor.c
