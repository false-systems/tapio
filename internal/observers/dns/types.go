package dns

import (
	"time"
)

// DNSProblemType represents types of DNS failures we detect
type DNSProblemType uint8

const (
	DNSProblemNone      DNSProblemType = 0 // No problem
	DNSProblemSlow      DNSProblemType = 1 // Query took too long
	DNSProblemNXDomain  DNSProblemType = 2 // Domain doesn't exist
	DNSProblemServfail  DNSProblemType = 3 // Server failure
	DNSProblemTimeout   DNSProblemType = 4 // No response
	DNSProblemRefused   DNSProblemType = 5 // Query refused
	DNSProblemTruncated DNSProblemType = 6 // Response truncated (TCP fallback needed)
)

// DNSEvent represents a DNS failure detected by eBPF
type DNSEvent struct {
	// Core problem info
	Timestamp   uint64         // Kernel timestamp
	ProblemType DNSProblemType // What went wrong
	LatencyNs   uint64         // How long it took (nanoseconds)

	// Query details
	QueryName [253]byte // Max DNS name length
	QueryType uint16    // A, AAAA, etc.
	ServerIP  [16]byte  // DNS server (v4 or v6)

	// Process context
	PID      uint32
	TID      uint32
	UID      uint32
	GID      uint32
	CgroupID uint64
	Comm     [16]byte // Process name

	// Network context
	SrcPort uint16
	DstPort uint16

	// Error details
	ResponseCode uint8 // DNS RCODE
	Retries      uint8 // How many retries
}

// String returns problem type as string
func (p DNSProblemType) String() string {
	switch p {
	case DNSProblemSlow:
		return "slow"
	case DNSProblemNXDomain:
		return "nxdomain"
	case DNSProblemServfail:
		return "servfail"
	case DNSProblemTimeout:
		return "timeout"
	case DNSProblemRefused:
		return "refused"
	case DNSProblemTruncated:
		return "truncated"
	default:
		return "unknown"
	}
}

// GetQueryName extracts the DNS query name from the byte array
func (e *DNSEvent) GetQueryName() string {
	for i, b := range e.QueryName {
		if b == 0 {
			return string(e.QueryName[:i])
		}
	}
	return string(e.QueryName[:])
}

// GetComm extracts the process command from the byte array
func (e *DNSEvent) GetComm() string {
	for i, b := range e.Comm {
		if b == 0 {
			return string(e.Comm[:i])
		}
	}
	return string(e.Comm[:])
}

// GetLatencyMs returns latency in milliseconds
func (e *DNSEvent) GetLatencyMs() int64 {
	return int64(e.LatencyNs / 1_000_000)
}

// IsSlow checks if the query was slow based on threshold
func (e *DNSEvent) IsSlow(thresholdMs float64) bool {
	return float64(e.GetLatencyMs()) > thresholdMs
}

// IsTimeout checks if this was a timeout
func (e *DNSEvent) IsTimeout() bool {
	return e.ProblemType == DNSProblemTimeout
}

// IsError checks if this was an error response
func (e *DNSEvent) IsError() bool {
	return e.ProblemType != DNSProblemSlow
}

// QueryStats tracks DNS problem statistics
type QueryStats struct {
	TotalProblems   uint64
	SlowQueries     uint64
	Timeouts        uint64
	NXDomains       uint64
	ServerFailures  uint64
	LastProblemTime time.Time
}
