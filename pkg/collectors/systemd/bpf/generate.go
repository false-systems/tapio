//go:build ignore
// +build ignore

package main

// This file is used to generate Go bindings for the eBPF programs
// Run: go generate ./...

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -I." -target amd64,arm64 -type k8s_syscall_event k8sServiceSyscalls k8s_service_syscalls.c -- -I.
