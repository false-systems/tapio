package bpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go@latest -cc clang -cflags "-O2 -g -Wall -Werror -D__TARGET_ARCH_x86" -target amd64 -type network_event -type flow_key -type conn_info -type http_state networkmonitor ../bpf_src/network_monitor.c -- -I../../bpf_common
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go@latest -cc clang -cflags "-O2 -g -Wall -Werror -D__TARGET_ARCH_arm64" -target arm64 -type network_event -type flow_key -type conn_info -type http_state networkmonitor ../bpf_src/network_monitor.c -- -I../../bpf_common
