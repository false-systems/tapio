package ebpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -no-strip -cflags "-target bpf" memorytracker bpf/memory_tracker.c -- -I./bpf -I./bpf/headers
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -target amd64,arm64 networkmonitor bpf/network_monitor.c -- -I./bpf -I./bpf/headers
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -target amd64,arm64 packetanalyzer bpf/packet_analyzer.c -- -I./bpf -I./bpf/headers
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -target amd64,arm64 dnsmonitor bpf/dns_monitor.c -- -I./bpf -I./bpf/headers
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -target amd64,arm64 protocolanalyzer bpf/protocol_analyzer.c -- -I./bpf -I./bpf/headers
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -target amd64,arm64 oomdetector bpf/oom_detector.c -- -I./bpf -I./bpf/headers
