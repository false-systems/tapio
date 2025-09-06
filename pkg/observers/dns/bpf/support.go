package bpf

import "runtime"

// IsSupported checks if eBPF is supported on this platform
func IsSupported() bool {
	// eBPF is only supported on Linux
	return runtime.GOOS == "linux"
}

// Export generated types for DNS monitoring
type DnsmonitorObjects = dnsmonitorObjects
type DnsmonitorMaps = dnsmonitorMaps
type DnsmonitorPrograms = dnsmonitorPrograms

// Export the generated loader functions
var LoadDnsmonitor = loadDnsmonitor
var LoadDnsmonitorObjects = loadDnsmonitorObjects
