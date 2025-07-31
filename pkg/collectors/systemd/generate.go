package systemd

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 -cc clang systemdMonitor bpf/systemd_monitor.c
