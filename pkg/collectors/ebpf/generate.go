package ebpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -no-strip -cflags "-target bpf" memorytracker bpf/memory_tracker.c -- -I./bpf -I./bpf/headers
