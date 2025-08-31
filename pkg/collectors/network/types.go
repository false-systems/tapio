package network

import (
	"time"
)

// NetworkCollectorConfig configuration for network collector
type NetworkCollectorConfig struct {
	BufferSize         int           `json:"buffer_size"`
	FlushInterval      time.Duration `json:"flush_interval"`
	EnableIPv4         bool          `json:"enable_ipv4"`
	EnableTCP          bool          `json:"enable_tcp"`
	EnableUDP          bool          `json:"enable_udp"`
	EnableHTTP         bool          `json:"enable_http"`
	EnableHTTPS        bool          `json:"enable_https"`
	HTTPPorts          []int         `json:"http_ports"`
	HTTPSPorts         []int         `json:"https_ports"`
	MaxEventsPerSecond int           `json:"max_events_per_second"`
	SamplingRate       float64       `json:"sampling_rate"`
}

// Enhanced HTTP parsing structures for deeper L7 analysis
type HTTPRequest struct {
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	Path        string            `json:"path"`
	Query       string            `json:"query"`
	Headers     map[string]string `json:"headers"`
	UserAgent   string            `json:"user_agent"`
	ContentType string            `json:"content_type"`
	Host        string            `json:"host"`
	Referer     string            `json:"referer"`
	HTTPVersion string            `json:"http_version"`
	BodySize    int64             `json:"body_size"`
}

type HTTPResponse struct {
	StatusCode    int               `json:"status_code"`
	StatusText    string            `json:"status_text"`
	Headers       map[string]string `json:"headers"`
	ContentType   string            `json:"content_type"`
	ContentLength int64             `json:"content_length"`
	BodySize      int64             `json:"body_size"`
	ResponseTime  time.Duration     `json:"response_time"`
}

// Enhanced DNS parsing structures
type DNSQuery struct {
	Name  string `json:"name"`
	Type  string `json:"type"`  // A, AAAA, CNAME, MX, etc.
	Class string `json:"class"` // Usually IN
}

type DNSResponse struct {
	ResponseCode string      `json:"response_code"` // NOERROR, NXDOMAIN, SERVFAIL, etc.
	Queries      []DNSQuery  `json:"queries"`
	Answers      []DNSAnswer `json:"answers"`
	Authoritative bool       `json:"authoritative"`
	Truncated     bool       `json:"truncated"`
	RecursionDesired bool    `json:"recursion_desired"`
	RecursionAvailable bool  `json:"recursion_available"`
}

type DNSAnswer struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Class string `json:"class"`
	TTL   uint32 `json:"ttl"`
	Data  string `json:"data"`
}

// HTTP state tracking for multi-packet streams
type HTTPConnectionState struct {
	ConnectionID    string                 `json:"connection_id"`
	State          HTTPStreamState        `json:"state"`
	Request        *HTTPRequest           `json:"request"`
	Response       *HTTPResponse          `json:"response"`
	StartTime      time.Time              `json:"start_time"`
	LastActivity   time.Time              `json:"last_activity"`
	RequestBuffer  []byte                 `json:"-"` // Raw buffer for incomplete requests
	ResponseBuffer []byte                 `json:"-"` // Raw buffer for incomplete responses
	HTTP2StreamID  uint32                 `json:"http2_stream_id,omitempty"`
}

type HTTPStreamState int

const (
	HTTPStateIdle HTTPStreamState = iota
	HTTPStateReadingRequestHeaders
	HTTPStateReadingRequestBody
	HTTPStateReadingResponseHeaders
	HTTPStateReadingResponseBody
	HTTPStateComplete
	HTTPStateError
)

// DNS response code mappings
var DNSResponseCodes = map[int]string{
	0:  "NOERROR",
	1:  "FORMERR",
	2:  "SERVFAIL",
	3:  "NXDOMAIN",
	4:  "NOTIMP",
	5:  "REFUSED",
	6:  "YXDOMAIN",
	7:  "YXRRSET",
	8:  "NXRRSET",
	9:  "NOTAUTH",
	10: "NOTZONE",
}

// DNS record type mappings
var DNSRecordTypes = map[int]string{
	1:   "A",
	2:   "NS",
	5:   "CNAME",
	6:   "SOA",
	12:  "PTR",
	15:  "MX",
	16:  "TXT",
	28:  "AAAA",
	33:  "SRV",
	99:  "SPF",
	257: "CAA",
}

// HTTP/2 and gRPC structures for enhanced L7 parsing
type HTTP2Frame struct {
	Length   int    `json:"length"`
	Type     uint8  `json:"type"`
	Flags    uint8  `json:"flags"`
	StreamID uint32 `json:"stream_id"`
	Payload  []byte `json:"payload,omitempty"`
}

type GRPCMessage struct {
	Compressed bool   `json:"compressed"`
	Length     uint32 `json:"length"`
	Payload    []byte `json:"payload,omitempty"`
	Service    string `json:"service,omitempty"`
	Method     string `json:"method,omitempty"`
}

