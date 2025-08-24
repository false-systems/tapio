package etcdapi

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"runtime"
)

// hashString generates a hash of the input string
func hashString(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))[:16] // Return first 16 chars for brevity
}

// getHostname returns the hostname
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// getKernelVersion returns the kernel version (OS version on non-Linux)
func getKernelVersion() string {
	return runtime.GOOS + "-" + runtime.Version()
}

// getOSVersion returns the OS version
func getOSVersion() string {
	return runtime.GOOS
}

// getArchitecture returns the system architecture
func getArchitecture() string {
	return runtime.GOARCH
}
