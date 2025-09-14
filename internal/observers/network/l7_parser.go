package network

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// L7Parser parses application layer protocols
type L7Parser struct {
	logger *zap.Logger
	config *Config
	mu     sync.RWMutex

	// Connection tracking
	httpConnections map[string]*HTTPConnectionState
	dnsTransactions map[string]*DNSTransaction

	// Statistics
	stats struct {
		httpRequestsParsed  int64
		httpResponsesParsed int64
		dnsQueriesParsed    int64
		dnsResponsesParsed  int64
		parseErrors         int64
	}
}

// HTTPConnectionState tracks HTTP connection state
type HTTPConnectionState struct {
	ConnectionID   string
	State          string // "request", "response", "idle"
	Request        *HTTPRequest
	Response       *HTTPResponse
	StartTime      time.Time
	LastActivity   time.Time
	RequestBuffer  []byte
	ResponseBuffer []byte
}

// HTTPRequest represents parsed HTTP request
type HTTPRequest struct {
	Method      string
	URL         string
	Path        string
	Query       string
	Headers     map[string]string
	UserAgent   string
	ContentType string
	Host        string
	Referer     string
	HTTPVersion string
	BodySize    int64
}

// HTTPResponse represents parsed HTTP response
type HTTPResponse struct {
	StatusCode    int
	StatusText    string
	Headers       map[string]string
	ContentType   string
	ContentLength int64
	BodySize      int64
	ResponseTime  time.Duration
}

// DNSTransaction tracks DNS query/response
type DNSTransaction struct {
	TransactionID uint16
	Query         *DNSQuery
	Response      *DNSResponse
	StartTime     time.Time
	ResponseTime  time.Duration
}

// DNSQuery represents a DNS query
type DNSQuery struct {
	Name  string
	Type  string // A, AAAA, CNAME, etc.
	Class string
}

// DNSResponse represents a DNS response
type DNSResponse struct {
	ResponseCode string // NOERROR, NXDOMAIN, etc.
	Answers      []DNSAnswer
}

// DNSAnswer represents a DNS answer
type DNSAnswer struct {
	Name string
	Type string
	TTL  uint32
	Data string
}

// NewL7Parser creates a new L7 parser
func NewL7Parser(logger *zap.Logger, config *Config) *L7Parser {
	return &L7Parser{
		logger:          logger,
		config:          config,
		httpConnections: make(map[string]*HTTPConnectionState),
		dnsTransactions: make(map[string]*DNSTransaction),
	}
}

// ParseHTTPRequest parses HTTP request from packet data
func (p *L7Parser) ParseHTTPRequest(connID string, data []byte) (*HTTPRequest, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Get or create connection state
	conn, exists := p.httpConnections[connID]
	if !exists {
		conn = &HTTPConnectionState{
			ConnectionID:  connID,
			State:         "request",
			StartTime:     time.Now(),
			LastActivity:  time.Now(),
			RequestBuffer: make([]byte, 0),
		}
		p.httpConnections[connID] = conn
	}

	// Append to buffer for multi-packet requests
	conn.RequestBuffer = append(conn.RequestBuffer, data...)
	conn.LastActivity = time.Now()

	// Try to parse HTTP request
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(conn.RequestBuffer)))
	if err != nil {
		// Not enough data yet, wait for more packets
		if len(conn.RequestBuffer) < 8192 { // Max header size
			return nil, nil
		}
		p.stats.parseErrors++
		return nil, err
	}

	// Extract request details
	httpReq := &HTTPRequest{
		Method:      req.Method,
		URL:         req.URL.String(),
		Path:        req.URL.Path,
		Query:       req.URL.RawQuery,
		HTTPVersion: req.Proto,
		Headers:     make(map[string]string),
		Host:        req.Host,
		UserAgent:   req.UserAgent(),
		Referer:     req.Referer(),
	}

	// Copy headers
	for k, v := range req.Header {
		if len(v) > 0 {
			httpReq.Headers[k] = v[0]
		}
	}

	if ct := req.Header.Get("Content-Type"); ct != "" {
		httpReq.ContentType = ct
	}

	if req.ContentLength > 0 {
		httpReq.BodySize = req.ContentLength
	}

	conn.Request = httpReq
	conn.State = "response"
	p.stats.httpRequestsParsed++

	// Clear buffer after successful parse
	conn.RequestBuffer = nil

	return httpReq, nil
}

// ParseHTTPResponse parses HTTP response from packet data
func (p *L7Parser) ParseHTTPResponse(connID string, data []byte) (*HTTPResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	conn, exists := p.httpConnections[connID]
	if !exists {
		// Response without request, create new state
		conn = &HTTPConnectionState{
			ConnectionID:   connID,
			State:          "response",
			StartTime:      time.Now(),
			LastActivity:   time.Now(),
			ResponseBuffer: make([]byte, 0),
		}
		p.httpConnections[connID] = conn
	}

	// Append to buffer
	conn.ResponseBuffer = append(conn.ResponseBuffer, data...)
	conn.LastActivity = time.Now()

	// Try to parse HTTP response
	resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(conn.ResponseBuffer)), nil)
	if err != nil {
		if len(conn.ResponseBuffer) < 8192 {
			return nil, nil
		}
		p.stats.parseErrors++
		return nil, err
	}

	// Extract response details
	httpResp := &HTTPResponse{
		StatusCode:  resp.StatusCode,
		StatusText:  resp.Status,
		Headers:     make(map[string]string),
		ContentType: resp.Header.Get("Content-Type"),
	}

	// Copy headers
	for k, v := range resp.Header {
		if len(v) > 0 {
			httpResp.Headers[k] = v[0]
		}
	}

	if resp.ContentLength > 0 {
		httpResp.ContentLength = resp.ContentLength
		httpResp.BodySize = resp.ContentLength
	}

	// Calculate response time if we have request
	if conn.Request != nil {
		httpResp.ResponseTime = time.Since(conn.StartTime)
	}

	conn.Response = httpResp
	conn.State = "idle"
	p.stats.httpResponsesParsed++

	// Clear buffer after successful parse
	conn.ResponseBuffer = nil

	return httpResp, nil
}

// ParseDNS parses DNS packets
func (p *L7Parser) ParseDNS(transactionID uint16, data []byte, isQuery bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := fmt.Sprintf("%d", transactionID)

	if isQuery {
		// Parse DNS query
		query := p.parseDNSQuery(data)
		if query != nil {
			p.dnsTransactions[key] = &DNSTransaction{
				TransactionID: transactionID,
				Query:         query,
				StartTime:     time.Now(),
			}
			p.stats.dnsQueriesParsed++
		}
	} else {
		// Parse DNS response
		trans, exists := p.dnsTransactions[key]
		if exists {
			response := p.parseDNSResponse(data)
			if response != nil {
				trans.Response = response
				trans.ResponseTime = time.Since(trans.StartTime)
				p.stats.dnsResponsesParsed++
			}
		}
	}

	return nil
}

// parseDNSQuery parses DNS query from packet
func (p *L7Parser) parseDNSQuery(data []byte) *DNSQuery {
	// Basic DNS header is 12 bytes minimum
	if len(data) < 12 {
		return nil
	}

	// Parse DNS header flags to check if it's a query
	flags := binary.BigEndian.Uint16(data[2:4])
	if flags&0x8000 != 0 { // Check QR bit - 0 for query, 1 for response
		return nil
	}

	// Parse question count
	qdCount := binary.BigEndian.Uint16(data[4:6])
	if qdCount == 0 {
		return nil
	}

	// Parse the first question (starting at byte 12)
	offset := 12
	name := p.parseDNSName(data, offset)

	// Move offset past the name
	for offset < len(data) && data[offset] != 0 {
		if data[offset]&0xc0 == 0xc0 { // Compression pointer
			offset += 2
			break
		}
		offset += int(data[offset]) + 1
	}
	if offset < len(data) && data[offset] == 0 {
		offset++ // Skip null terminator
	}

	// Parse query type and class (4 bytes total)
	if offset+4 > len(data) {
		return nil
	}

	qType := binary.BigEndian.Uint16(data[offset : offset+2])
	qClass := binary.BigEndian.Uint16(data[offset+2 : offset+4])

	return &DNSQuery{
		Name:  name,
		Type:  p.getDNSType(qType),
		Class: p.getDNSClass(qClass),
	}
}

// parseDNSResponse parses DNS response from packet
func (p *L7Parser) parseDNSResponse(data []byte) *DNSResponse {
	// Basic DNS header is 12 bytes minimum
	if len(data) < 12 {
		return nil
	}

	// Parse DNS header
	flags := binary.BigEndian.Uint16(data[2:4])
	if flags&0x8000 == 0 { // Check QR bit - should be 1 for response
		return nil
	}

	// Get response code (RCODE)
	rcode := flags & 0x000f

	// Get answer count
	anCount := binary.BigEndian.Uint16(data[6:8])

	// Skip questions section
	qdCount := binary.BigEndian.Uint16(data[4:6])
	offset := 12
	for i := uint16(0); i < qdCount && offset < len(data); i++ {
		// Skip name
		for offset < len(data) && data[offset] != 0 {
			if data[offset]&0xc0 == 0xc0 {
				offset += 2
				break
			}
			offset += int(data[offset]) + 1
		}
		if offset < len(data) && data[offset] == 0 {
			offset++
		}
		offset += 4 // Skip type and class
	}

	// Parse answers
	answers := make([]DNSAnswer, 0, anCount)
	for i := uint16(0); i < anCount && offset < len(data)-10; i++ {
		// Parse name
		name := p.parseDNSName(data, offset)

		// Skip name in packet
		for offset < len(data) && data[offset] != 0 {
			if data[offset]&0xc0 == 0xc0 {
				offset += 2
				break
			}
			offset += int(data[offset]) + 1
		}
		if offset < len(data) && data[offset] == 0 {
			offset++
		}

		if offset+10 > len(data) {
			break
		}

		// Parse type, class, TTL, and data length
		aType := binary.BigEndian.Uint16(data[offset : offset+2])
		offset += 4 // Skip type and class
		ttl := binary.BigEndian.Uint32(data[offset : offset+4])
		offset += 4
		dataLen := binary.BigEndian.Uint16(data[offset : offset+2])
		offset += 2

		if offset+int(dataLen) > len(data) {
			break
		}

		// Parse data based on type
		var dataStr string
		if aType == 1 && dataLen == 4 { // A record
			dataStr = fmt.Sprintf("%d.%d.%d.%d",
				data[offset], data[offset+1], data[offset+2], data[offset+3])
		} else if aType == 28 && dataLen == 16 { // AAAA record
			dataStr = p.formatIPv6(data[offset : offset+16])
		} else {
			// For other types, store as hex for now
			dataStr = fmt.Sprintf("%x", data[offset:offset+int(dataLen)])
		}

		answers = append(answers, DNSAnswer{
			Name: name,
			Type: p.getDNSType(aType),
			TTL:  ttl,
			Data: dataStr,
		})

		offset += int(dataLen)
	}

	return &DNSResponse{
		ResponseCode: p.getDNSRCode(rcode),
		Answers:      answers,
	}
}

// parseDNSName extracts a domain name from DNS packet
func (p *L7Parser) parseDNSName(data []byte, offset int) string {
	var name []string
	maxJumps := 5 // Prevent infinite loops
	jumps := 0

	for offset < len(data) && jumps < maxJumps {
		length := data[offset]

		// Check for compression pointer
		if length&0xc0 == 0xc0 {
			if offset+1 >= len(data) {
				break
			}
			// Follow pointer
			newOffset := int(binary.BigEndian.Uint16(data[offset:offset+2]) & 0x3fff)
			if newOffset >= len(data) {
				break
			}
			offset = newOffset
			jumps++
			continue
		}

		// End of name
		if length == 0 {
			break
		}

		// Read label
		offset++
		if offset+int(length) > len(data) {
			break
		}
		name = append(name, string(data[offset:offset+int(length)]))
		offset += int(length)
	}

	if len(name) == 0 {
		return ""
	}
	return strings.Join(name, ".")
}

// getDNSType converts DNS type code to string
func (p *L7Parser) getDNSType(typeCode uint16) string {
	types := map[uint16]string{
		1:   "A",
		2:   "NS",
		5:   "CNAME",
		6:   "SOA",
		12:  "PTR",
		15:  "MX",
		16:  "TXT",
		28:  "AAAA",
		33:  "SRV",
		257: "CAA",
	}
	if t, ok := types[typeCode]; ok {
		return t
	}
	return fmt.Sprintf("TYPE%d", typeCode)
}

// getDNSClass converts DNS class code to string
func (p *L7Parser) getDNSClass(classCode uint16) string {
	switch classCode {
	case 1:
		return "IN"
	case 2:
		return "CS"
	case 3:
		return "CH"
	case 4:
		return "HS"
	default:
		return fmt.Sprintf("CLASS%d", classCode)
	}
}

// getDNSRCode converts DNS response code to string
func (p *L7Parser) getDNSRCode(rcode uint16) string {
	codes := map[uint16]string{
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
	if c, ok := codes[rcode]; ok {
		return c
	}
	return fmt.Sprintf("RCODE%d", rcode)
}

// formatIPv6 formats IPv6 address bytes
func (p *L7Parser) formatIPv6(data []byte) string {
	if len(data) != 16 {
		return ""
	}
	return fmt.Sprintf("%x:%x:%x:%x:%x:%x:%x:%x",
		binary.BigEndian.Uint16(data[0:2]),
		binary.BigEndian.Uint16(data[2:4]),
		binary.BigEndian.Uint16(data[4:6]),
		binary.BigEndian.Uint16(data[6:8]),
		binary.BigEndian.Uint16(data[8:10]),
		binary.BigEndian.Uint16(data[10:12]),
		binary.BigEndian.Uint16(data[12:14]),
		binary.BigEndian.Uint16(data[14:16]))
}

// Flush cleans up old connections
func (p *L7Parser) Flush() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	timeout := 5 * time.Minute

	// Clean up old HTTP connections
	for id, conn := range p.httpConnections {
		if now.Sub(conn.LastActivity) > timeout {
			delete(p.httpConnections, id)
		}
	}

	// Clean up old DNS transactions
	for id, trans := range p.dnsTransactions {
		if now.Sub(trans.StartTime) > timeout {
			delete(p.dnsTransactions, id)
		}
	}
}

// GetStats returns parser statistics
func (p *L7Parser) GetStats() map[string]int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return map[string]int64{
		"http_requests_parsed":  p.stats.httpRequestsParsed,
		"http_responses_parsed": p.stats.httpResponsesParsed,
		"dns_queries_parsed":    p.stats.dnsQueriesParsed,
		"dns_responses_parsed":  p.stats.dnsResponsesParsed,
		"parse_errors":          p.stats.parseErrors,
	}
}

// IsHTTPPort checks if a port is configured for HTTP parsing
func (p *L7Parser) IsHTTPPort(port uint16) bool {
	for _, p := range p.config.HTTPPorts {
		if uint16(p) == port {
			return true
		}
	}
	return false
}

// IsHTTPSPort checks if a port is configured for HTTPS parsing
func (p *L7Parser) IsHTTPSPort(port uint16) bool {
	for _, p := range p.config.HTTPSPorts {
		if uint16(p) == port {
			return true
		}
	}
	return false
}

// IsDNSPort checks if a port is DNS port
func (p *L7Parser) IsDNSPort(port uint16) bool {
	return port == uint16(p.config.DNSPort)
}

// IdentifyProtocol tries to identify the protocol from packet data
func (p *L7Parser) IdentifyProtocol(data []byte) string {
	if len(data) < 4 {
		return "unknown"
	}

	// Check for HTTP methods
	httpMethods := []string{"GET ", "POST", "PUT ", "HEAD", "DELE", "OPTI", "PATC"}
	for _, method := range httpMethods {
		if strings.HasPrefix(string(data), method) {
			return "http"
		}
	}

	// Check for HTTP response
	if strings.HasPrefix(string(data), "HTTP/") {
		return "http"
	}

	// Check for DNS (simplified)
	if len(data) > 12 && (data[2]&0x80) == 0 {
		// Might be DNS query
		return "dns"
	}

	return "unknown"
}
