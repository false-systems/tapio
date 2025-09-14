package containerruntime

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags $BPF_CFLAGS -target amd64,arm64 crimonitor ../bpf_src/cri_monitor.c -- -I../../bpf_common
