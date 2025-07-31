package ebpf

// Unified CO-RE program generation
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -target bpf" -target amd64,arm64 -type event unified bpf/unified.c -- -I./bpf -I./bpf/headers

// K8s tracker CO-RE program generation
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -target bpf" -target amd64,arm64 -type k8s_event,k8s_pod_info k8stracker bpf/k8s_tracker.c -- -I./bpf -I./bpf/headers
