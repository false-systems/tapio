//go:build linux
// +build linux

package dns

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractQueryName(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "simple domain name",
			input:    append([]byte("example.com"), make([]byte, 118)...), // pad to 128 bytes
			expected: "example.com",
		},
		{
			name:     "domain with null terminator",
			input:    append([]byte("test.example.com\x00"), make([]byte, 111)...), // pad to 128 bytes
			expected: "test.example.com",
		},
		{
			name:     "empty input",
			input:    make([]byte, 128),
			expected: "",
		},
		{
			name:     "input with null bytes in middle",
			input:    append([]byte("bad\x00domain.com"), make([]byte, 114)...), // pad to 128 bytes
			expected: "",
		},
		{
			name:     "very long domain name",
			input:    append([]byte("verylongdomainnamethatexceedslimits.example.com"), make([]byte, 80)...), // pad to 128 bytes
			expected: "verylongdomainnamethatexceedslimits.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.extractQueryName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCleanDNSName(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal domain name",
			input:    "example.com",
			expected: "example.com",
		},
		{
			name:     "domain with invalid characters",
			input:    "example\x01\x02.com",
			expected: "example.com",
		},
		{
			name:     "wire format domain",
			input:    "\x07example\x03com\x00",
			expected: "example.com",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "domain with control characters",
			input:    "test\x7f\x80domain.com",
			expected: "testdomain.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.cleanDNSName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseDNSWireFormat(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple wire format",
			input:    "\x07example\x03com\x00",
			expected: "example.com",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "invalid length",
			input:    "\x10short",
			expected: "",
		},
		{
			name:     "multiple labels",
			input:    "\x04test\x07example\x03com\x00",
			expected: "test.example.com",
		},
		{
			name:     "length too large",
			input:    "\x80toolarge",
			expected: "",
		},
		{
			name:     "single label",
			input:    "\x04test\x00",
			expected: "test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.parseDNSWireFormat(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsValidDNSLabel(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid label",
			input:    "example",
			expected: true,
		},
		{
			name:     "valid label with numbers",
			input:    "test123",
			expected: true,
		},
		{
			name:     "valid label with hyphen",
			input:    "test-example",
			expected: true,
		},
		{
			name:     "valid label with underscore",
			input:    "test_example",
			expected: true,
		},
		{
			name:     "empty label",
			input:    "",
			expected: false,
		},
		{
			name:     "label too long",
			input:    "thislabelistoolongandexceeds63characterswhichisthemaximumlengthallowed",
			expected: false,
		},
		{
			name:     "label with invalid character",
			input:    "test@example",
			expected: false,
		},
		{
			name:     "label with space",
			input:    "test example",
			expected: false,
		},
		{
			name:     "label with uppercase",
			input:    "TestExample",
			expected: true,
		},
		{
			name:     "label with dot",
			input:    "test.example",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isValidDNSLabel(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetProtocolName(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	tests := []struct {
		name     string
		protocol uint8
		expected string
	}{
		{
			name:     "UDP protocol",
			protocol: 17,
			expected: "UDP",
		},
		{
			name:     "TCP protocol",
			protocol: 6,
			expected: "TCP",
		},
		{
			name:     "unknown protocol",
			protocol: 99,
			expected: "unknown",
		},
		{
			name:     "ICMP protocol",
			protocol: 1,
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.getProtocolName(tt.protocol)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetDNSTypeName(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	tests := []struct {
		name     string
		qtype    uint16
		expected string
	}{
		{
			name:     "A record",
			qtype:    1,
			expected: "A",
		},
		{
			name:     "NS record",
			qtype:    2,
			expected: "NS",
		},
		{
			name:     "CNAME record",
			qtype:    5,
			expected: "CNAME",
		},
		{
			name:     "SOA record",
			qtype:    6,
			expected: "SOA",
		},
		{
			name:     "PTR record",
			qtype:    12,
			expected: "PTR",
		},
		{
			name:     "MX record",
			qtype:    15,
			expected: "MX",
		},
		{
			name:     "TXT record",
			qtype:    16,
			expected: "TXT",
		},
		{
			name:     "AAAA record",
			qtype:    28,
			expected: "AAAA",
		},
		{
			name:     "SRV record",
			qtype:    33,
			expected: "SRV",
		},
		{
			name:     "unknown record type",
			qtype:    999,
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.getDNSTypeName(tt.qtype)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetDNSRcodeName(t *testing.T) {
	cfg := DefaultConfig()
	collector, err := NewCollector("test", cfg)
	require.NoError(t, err)

	tests := []struct {
		name     string
		rcode    uint8
		expected string
	}{
		{
			name:     "NOERROR",
			rcode:    0,
			expected: "NOERROR",
		},
		{
			name:     "FORMERR",
			rcode:    1,
			expected: "FORMERR",
		},
		{
			name:     "SERVFAIL",
			rcode:    2,
			expected: "SERVFAIL",
		},
		{
			name:     "NXDOMAIN",
			rcode:    3,
			expected: "NXDOMAIN",
		},
		{
			name:     "NOTIMP",
			rcode:    4,
			expected: "NOTIMP",
		},
		{
			name:     "REFUSED",
			rcode:    5,
			expected: "REFUSED",
		},
		{
			name:     "unknown rcode",
			rcode:    99,
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.getDNSRcodeName(tt.rcode)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Benchmark tests for performance-critical helper functions

func BenchmarkExtractQueryName(b *testing.B) {
	cfg := DefaultConfig()
	collector, _ := NewCollector("bench", cfg)

	input := append([]byte("example.com"), make([]byte, 118)...)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.extractQueryName(input)
	}
}

func BenchmarkCleanDNSName(b *testing.B) {
	cfg := DefaultConfig()
	collector, _ := NewCollector("bench", cfg)

	input := "example.com"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.cleanDNSName(input)
	}
}

func BenchmarkParseDNSWireFormat(b *testing.B) {
	cfg := DefaultConfig()
	collector, _ := NewCollector("bench", cfg)

	input := "\x07example\x03com\x00"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.parseDNSWireFormat(input)
	}
}

func BenchmarkIsValidDNSLabel(b *testing.B) {
	cfg := DefaultConfig()
	collector, _ := NewCollector("bench", cfg)

	input := "example"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.isValidDNSLabel(input)
	}
}

func BenchmarkGetProtocolName(b *testing.B) {
	cfg := DefaultConfig()
	collector, _ := NewCollector("bench", cfg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.getProtocolName(17) // UDP
	}
}

func BenchmarkGetDNSTypeName(b *testing.B) {
	cfg := DefaultConfig()
	collector, _ := NewCollector("bench", cfg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.getDNSTypeName(1) // A record
	}
}
