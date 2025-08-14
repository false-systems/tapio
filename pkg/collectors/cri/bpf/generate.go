//go:build ignore

package main

import (
	"log"
	"os"
	"os/exec"
)

func main() {
	// Generate eBPF Go code from C source
	cmd := exec.Command("go", "run", "github.com/cilium/ebpf/cmd/bpf2go",
		"-cc", "clang",
		"-cflags", "$BPF_CFLAGS",
		"-target", "native",
		"crimonitor", "../bpf_src/cri_monitor.c",
		"--", "-I../../bpf_common")

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatalf("failed to generate eBPF code: %v", err)
	}
}
