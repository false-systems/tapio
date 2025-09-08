package network

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"go.uber.org/zap"
)

// L7Parser handles deep parsing of Layer 7 protocols
type L7Parser struct {
	logger *zap.Logger

	// HTTP state tracking
	httpConnections map[string]*HTTPConnectionState

	// Configuration
	maxHTTPHeaderSize int
	maxHTTPBodySize   int
	maxDNSPacketSize  int

	// Protocol detection
	detectHTTP2 bool
	detectGRPC  bool
}

// NewL7Parser creates a new Layer 7 protocol parser
func NewL7Parser(logger *zap.Logger) *L7Parser {
	return &L7Parser{
		logger:            logger,
		httpConnections:   make(map[string]*HTTPConnectionState),
		maxHTTPHeaderSize: 8192,        // 8KB
		maxHTTPBodySize:   1024 * 1024, // 1MB
		maxDNSPacketSize:  512,         // Standard DNS packet size
		detectHTTP2:       true,
		detectGRPC:        true,
	}
}

// ParseHTTPRequest parses HTTP request from raw bytes with state machine
func (p *L7Parser) ParseHTTPRequest(connectionID string, data []byte) (*HTTPRequest, error) {
	// Get or create connection state
	state := p.getHTTPConnectionState(connectionID)

	// Append new data to buffer
	state.RequestBuffer = append(state.RequestBuffer, data...)
	state.LastActivity = time.Now()

	// Try to parse complete request
	return p.parseHTTPRequestFromBuffer(state)
}

// ParseHTTPResponse parses HTTP response from raw bytes with state machine
func (p *L7Parser) ParseHTTPResponse(connectionID string, data []byte) (*HTTPResponse, error) {
	state := p.getHTTPConnectionState(connectionID)
	state.ResponseBuffer = append(state.ResponseBuffer, data...)
	state.LastActivity = time.Now()

	return p.parseHTTPResponseFromBuffer(state)
}

// getHTTPConnectionState gets or creates HTTP connection state
func (p *L7Parser) getHTTPConnectionState(connectionID string) *HTTPConnectionState {
	state, exists := p.httpConnections[connectionID]
	if !exists {
		state = &HTTPConnectionState{
			ConnectionID:   connectionID,
			State:          HTTPStateIdle,
			StartTime:      time.Now(),
			LastActivity:   time.Now(),
			RequestBuffer:  make([]byte, 0, p.maxHTTPHeaderSize),
			ResponseBuffer: make([]byte, 0, p.maxHTTPHeaderSize),
		}
		p.httpConnections[connectionID] = state
	}
	return state
}

// parseHTTPRequestFromBuffer parses HTTP request from buffer using state machine
func (p *L7Parser) parseHTTPRequestFromBuffer(state *HTTPConnectionState) (*HTTPRequest, error) {
	buffer := state.RequestBuffer
	if len(buffer) == 0 {
		return nil, fmt.Errorf("empty buffer")
	}

	switch state.State {
	case HTTPStateIdle, HTTPStateReadingRequestHeaders:
		// Look for end of headers (\r\n\r\n)
		headerEndIdx := bytes.Index(buffer, []byte("\r\n\r\n"))
		if headerEndIdx == -1 {
			// Headers incomplete, need more data
			state.State = HTTPStateReadingRequestHeaders
			return nil, fmt.Errorf("incomplete headers")
		}

		// Parse headers
		headerBytes := buffer[:headerEndIdx]
		request, err := p.parseHTTPRequestHeaders(headerBytes)
		if err != nil {
			state.State = HTTPStateError
			return nil, fmt.Errorf("failed to parse headers: %w", err)
		}

		state.Request = request

		// Check if there's a body
		if request.Headers["content-length"] != "" {
			contentLength, err := strconv.ParseInt(request.Headers["content-length"], 10, 64)
			if err != nil || contentLength <= 0 {
				state.State = HTTPStateComplete
				return request, nil
			}

			request.BodySize = contentLength
			bodyStartIdx := headerEndIdx + 4 // Skip \r\n\r\n

			if int64(len(buffer)-bodyStartIdx) >= contentLength {
				// Complete body available
				state.State = HTTPStateComplete
				return request, nil
			} else {
				// Incomplete body
				state.State = HTTPStateReadingRequestBody
				return nil, fmt.Errorf("incomplete body")
			}
		}

		state.State = HTTPStateComplete
		return request, nil

	case HTTPStateReadingRequestBody:
		// Continue reading body based on content-length
		// This is a simplified version - full implementation would handle chunked encoding
		if state.Request != nil && state.Request.BodySize > 0 {
			headerEndIdx := bytes.Index(buffer, []byte("\r\n\r\n"))
			if headerEndIdx != -1 {
				bodyStartIdx := headerEndIdx + 4
				if int64(len(buffer)-bodyStartIdx) >= state.Request.BodySize {
					state.State = HTTPStateComplete
					return state.Request, nil
				}
			}
		}
		return nil, fmt.Errorf("incomplete body")

	default:
		return state.Request, nil
	}
}

// parseHTTPRequestHeaders parses HTTP request headers
func (p *L7Parser) parseHTTPRequestHeaders(headerBytes []byte) (*HTTPRequest, error) {
	scanner := bufio.NewScanner(bytes.NewReader(headerBytes))

	var request *HTTPRequest
	lineNum := 0

	for scanner.Scan() {
		line := scanner.Text()

		if lineNum == 0 {
			// Parse request line: METHOD /path HTTP/1.1
			parts := strings.Fields(line)
			if len(parts) != 3 {
				return nil, fmt.Errorf("invalid request line: %s", line)
			}

			method := parts[0]
			fullURL := parts[1]
			httpVersion := parts[2]

			// Parse URL and query
			var path, query string
			if idx := strings.Index(fullURL, "?"); idx != -1 {
				path = fullURL[:idx]
				query = fullURL[idx+1:]
			} else {
				path = fullURL
			}

			request = &HTTPRequest{
				Method:      method,
				URL:         fullURL,
				Path:        path,
				Query:       query,
				HTTPVersion: httpVersion,
				Headers:     make(map[string]string),
			}
		} else {
			// Parse header: Name: Value
			if idx := strings.Index(line, ":"); idx != -1 {
				name := strings.ToLower(strings.TrimSpace(line[:idx]))
				value := strings.TrimSpace(line[idx+1:])
				request.Headers[name] = value

				// Extract commonly used headers
				switch name {
				case "host":
					request.Host = value
				case "user-agent":
					request.UserAgent = value
				case "content-type":
					request.ContentType = value
				case "referer":
					request.Referer = value
				}
			}
		}
		lineNum++
	}

	return request, nil
}

// parseHTTPResponseFromBuffer parses HTTP response from buffer
func (p *L7Parser) parseHTTPResponseFromBuffer(state *HTTPConnectionState) (*HTTPResponse, error) {
	buffer := state.ResponseBuffer
	if len(buffer) == 0 {
		return nil, fmt.Errorf("empty response buffer")
	}

	// Look for end of headers
	headerEndIdx := bytes.Index(buffer, []byte("\r\n\r\n"))
	if headerEndIdx == -1 {
		return nil, fmt.Errorf("incomplete response headers")
	}

	headerBytes := buffer[:headerEndIdx]
	response, err := p.parseHTTPResponseHeaders(headerBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response headers: %w", err)
	}

	// Calculate response time if we have the request start time
	if state.Request != nil {
		response.ResponseTime = time.Since(state.StartTime)
	}

	return response, nil
}

// parseHTTPResponseHeaders parses HTTP response headers
func (p *L7Parser) parseHTTPResponseHeaders(headerBytes []byte) (*HTTPResponse, error) {
	scanner := bufio.NewScanner(bytes.NewReader(headerBytes))

	var response *HTTPResponse
	lineNum := 0

	for scanner.Scan() {
		line := scanner.Text()

		if lineNum == 0 {
			// Parse status line: HTTP/1.1 200 OK
			parts := strings.Fields(line)
			if len(parts) < 2 {
				return nil, fmt.Errorf("invalid status line: %s", line)
			}

			statusCode, err := strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid status code: %s", parts[1])
			}

			statusText := ""
			if len(parts) > 2 {
				statusText = strings.Join(parts[2:], " ")
			}

			response = &HTTPResponse{
				StatusCode: statusCode,
				StatusText: statusText,
				Headers:    make(map[string]string),
			}
		} else {
			// Parse header
			if idx := strings.Index(line, ":"); idx != -1 {
				name := strings.ToLower(strings.TrimSpace(line[:idx]))
				value := strings.TrimSpace(line[idx+1:])
				response.Headers[name] = value

				// Extract commonly used headers
				switch name {
				case "content-type":
					response.ContentType = value
				case "content-length":
					if length, err := strconv.ParseInt(value, 10, 64); err == nil {
						response.ContentLength = length
					}
				}
			}
		}
		lineNum++
	}

	return response, nil
}

// ParseDNSPacket parses DNS packet from raw bytes (RFC 1035)
func (p *L7Parser) ParseDNSPacket(data []byte) (*DNSQuery, *DNSResponse, error) {
	if len(data) < 12 {
		return nil, nil, fmt.Errorf("DNS packet too short: %d bytes", len(data))
	}

	// Parse DNS header
	header := parseDNSHeader(data[:12])

	var query *DNSQuery
	var response *DNSResponse

	// Parse question section (query)
	offset := 12
	if header.QuestionCount > 0 {
		name, qtype, qclass, newOffset, err := parseDNSQuestion(data, offset)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse DNS question: %w", err)
		}

		query = &DNSQuery{
			Name:  name,
			Type:  getDNSRecordTypeName(qtype),
			Class: getDNSClassName(qclass),
		}
		offset = newOffset
	}

	// Parse answer section (if this is a response)
	if header.IsResponse && header.AnswerCount > 0 {
		answers := make([]DNSAnswer, 0, header.AnswerCount)

		for i := 0; i < int(header.AnswerCount); i++ {
			answer, newOffset, err := parseDNSAnswer(data, offset)
			if err != nil {
				p.logger.Warn("Failed to parse DNS answer", zap.Error(err), zap.Int("answer_index", i))
				break
			}
			answers = append(answers, answer)
			offset = newOffset
		}

		response = &DNSResponse{
			ResponseCode:       getDNSResponseCodeName(header.ResponseCode),
			Answers:            answers,
			Authoritative:      header.Authoritative,
			Truncated:          header.Truncated,
			RecursionDesired:   header.RecursionDesired,
			RecursionAvailable: header.RecursionAvailable,
		}

		if query != nil {
			response.Queries = []DNSQuery{*query}
		}
	}

	return query, response, nil
}

// DNS header structure
type dnsHeader struct {
	ID                 uint16
	IsResponse         bool
	Opcode             uint8
	Authoritative      bool
	Truncated          bool
	RecursionDesired   bool
	RecursionAvailable bool
	ResponseCode       uint8
	QuestionCount      uint16
	AnswerCount        uint16
	AuthorityCount     uint16
	AdditionalCount    uint16
}

// parseDNSHeader parses DNS header from 12 bytes
func parseDNSHeader(data []byte) dnsHeader {
	id := binary.BigEndian.Uint16(data[0:2])
	flags := binary.BigEndian.Uint16(data[2:4])

	return dnsHeader{
		ID:                 id,
		IsResponse:         (flags & 0x8000) != 0,
		Opcode:             uint8((flags >> 11) & 0x0F),
		Authoritative:      (flags & 0x0400) != 0,
		Truncated:          (flags & 0x0200) != 0,
		RecursionDesired:   (flags & 0x0100) != 0,
		RecursionAvailable: (flags & 0x0080) != 0,
		ResponseCode:       uint8(flags & 0x000F),
		QuestionCount:      binary.BigEndian.Uint16(data[4:6]),
		AnswerCount:        binary.BigEndian.Uint16(data[6:8]),
		AuthorityCount:     binary.BigEndian.Uint16(data[8:10]),
		AdditionalCount:    binary.BigEndian.Uint16(data[10:12]),
	}
}

// parseDNSQuestion parses DNS question section
func parseDNSQuestion(data []byte, offset int) (name string, qtype uint16, qclass uint16, newOffset int, err error) {
	name, newOffset, err = parseDNSName(data, offset)
	if err != nil {
		return "", 0, 0, offset, err
	}

	if newOffset+4 > len(data) {
		return "", 0, 0, offset, fmt.Errorf("insufficient data for question type and class")
	}

	qtype = binary.BigEndian.Uint16(data[newOffset : newOffset+2])
	qclass = binary.BigEndian.Uint16(data[newOffset+2 : newOffset+4])

	return name, qtype, qclass, newOffset + 4, nil
}

// parseDNSAnswer parses DNS answer section
func parseDNSAnswer(data []byte, offset int) (DNSAnswer, int, error) {
	name, newOffset, err := parseDNSName(data, offset)
	if err != nil {
		return DNSAnswer{}, offset, err
	}

	if newOffset+10 > len(data) {
		return DNSAnswer{}, offset, fmt.Errorf("insufficient data for answer header")
	}

	rtype := binary.BigEndian.Uint16(data[newOffset : newOffset+2])
	rclass := binary.BigEndian.Uint16(data[newOffset+2 : newOffset+4])
	ttl := binary.BigEndian.Uint32(data[newOffset+4 : newOffset+8])
	rdlength := binary.BigEndian.Uint16(data[newOffset+8 : newOffset+10])

	newOffset += 10

	if newOffset+int(rdlength) > len(data) {
		return DNSAnswer{}, offset, fmt.Errorf("insufficient data for answer data")
	}

	// Parse answer data based on type
	var answerData string
	switch rtype {
	case 1: // A record
		if rdlength == 4 {
			answerData = fmt.Sprintf("%d.%d.%d.%d",
				data[newOffset], data[newOffset+1], data[newOffset+2], data[newOffset+3])
		}
	case 5: // CNAME
		answerData, _, _ = parseDNSName(data, newOffset)
	case 28: // AAAA record
		if rdlength == 16 {
			answerData = fmt.Sprintf("%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x:%02x%02x",
				data[newOffset], data[newOffset+1], data[newOffset+2], data[newOffset+3],
				data[newOffset+4], data[newOffset+5], data[newOffset+6], data[newOffset+7],
				data[newOffset+8], data[newOffset+9], data[newOffset+10], data[newOffset+11],
				data[newOffset+12], data[newOffset+13], data[newOffset+14], data[newOffset+15])
		}
	default:
		// Raw data for unknown types
		answerData = fmt.Sprintf("%x", data[newOffset:newOffset+int(rdlength)])
	}

	return DNSAnswer{
		Name:  name,
		Type:  getDNSRecordTypeName(rtype),
		Class: getDNSClassName(rclass),
		TTL:   ttl,
		Data:  answerData,
	}, newOffset + int(rdlength), nil
}

// parseDNSName parses DNS name with compression support
func parseDNSName(data []byte, offset int) (string, int, error) {
	var name strings.Builder
	originalOffset := offset
	jumped := false

	for {
		if offset >= len(data) {
			return "", originalOffset, fmt.Errorf("unexpected end of data while parsing name")
		}

		length := data[offset]

		// Check for compression (top two bits set)
		if (length & 0xC0) == 0xC0 {
			if offset+1 >= len(data) {
				return "", originalOffset, fmt.Errorf("incomplete compression pointer")
			}

			// Compression pointer
			pointer := binary.BigEndian.Uint16(data[offset:offset+2]) & 0x3FFF

			if !jumped {
				originalOffset = offset + 2
			}

			offset = int(pointer)
			jumped = true
			continue
		}

		if length == 0 {
			// End of name
			if !jumped {
				originalOffset = offset + 1
			}
			break
		}

		offset++

		if offset+int(length) > len(data) {
			return "", originalOffset, fmt.Errorf("label extends beyond packet")
		}

		if name.Len() > 0 {
			name.WriteByte('.')
		}

		for i := 0; i < int(length); i++ {
			c := data[offset+i]
			if unicode.IsPrint(rune(c)) {
				name.WriteByte(c)
			} else {
				name.WriteString(fmt.Sprintf("\\%03d", c))
			}
		}

		offset += int(length)
	}

	return name.String(), originalOffset, nil
}

// Helper functions for DNS mappings
func getDNSRecordTypeName(rtype uint16) string {
	if name, exists := DNSRecordTypes[int(rtype)]; exists {
		return name
	}
	return fmt.Sprintf("TYPE%d", rtype)
}

func getDNSClassName(class uint16) string {
	switch class {
	case 1:
		return "IN"
	case 3:
		return "CH"
	case 4:
		return "HS"
	default:
		return fmt.Sprintf("CLASS%d", class)
	}
}

func getDNSResponseCodeName(rcode uint8) string {
	if name, exists := DNSResponseCodes[int(rcode)]; exists {
		return name
	}
	return fmt.Sprintf("RCODE%d", rcode)
}

// DetectProtocol detects the L7 protocol from payload data
func (p *L7Parser) DetectProtocol(data []byte) string {
	if len(data) == 0 {
		return "unknown"
	}

	// HTTP/2 connection preface detection
	if len(data) >= 24 && string(data[:24]) == "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n" {
		return "http2"
	}

	// HTTP/2 frame detection (starts with 9-byte frame header)
	if len(data) >= 9 && p.isHTTP2Frame(data) {
		return "http2"
	}

	// gRPC detection (typically HTTP/2 with specific content-type)
	if p.detectGRPC && p.isGRPCTraffic(data) {
		return "grpc"
	}

	// HTTP/1.x detection
	if p.isHTTP1Traffic(data) {
		return "http1"
	}

	return "unknown"
}

// isHTTP2Frame checks if data starts with valid HTTP/2 frame header
func (p *L7Parser) isHTTP2Frame(data []byte) bool {
	if len(data) < 9 {
		return false
	}

	// HTTP/2 frame format: 3-byte length, 1-byte type, 1-byte flags, 4-byte stream ID
	frameLength := int(data[0])<<16 | int(data[1])<<8 | int(data[2])
	frameType := data[3]

	// Validate frame type (0-10 are defined frame types in HTTP/2)
	if frameType > 10 {
		return false
	}

	// Validate frame length (must not exceed max frame size, typically 16KB)
	if frameLength > 16384 || frameLength < 0 {
		return false
	}

	return true
}

// isGRPCTraffic detects gRPC traffic patterns
func (p *L7Parser) isGRPCTraffic(data []byte) bool {
	// gRPC over HTTP/2 typically has specific patterns:
	// 1. HTTP/2 frames with gRPC headers
	// 2. Content-Type: application/grpc
	// 3. gRPC message framing (5-byte header + protobuf payload)

	if len(data) >= 5 && p.isGRPCMessage(data) {
		return true
	}

	// Check for gRPC HTTP/2 headers
	dataStr := string(data)
	if strings.Contains(dataStr, "application/grpc") ||
		strings.Contains(dataStr, "grpc-") ||
		strings.Contains(dataStr, ":method\tPOST") {
		return true
	}

	return false
}

// isGRPCMessage checks if data starts with gRPC message framing
func (p *L7Parser) isGRPCMessage(data []byte) bool {
	if len(data) < 5 {
		return false
	}

	// gRPC message format: 1-byte compression flag + 4-byte length
	compressionFlag := data[0]
	messageLength := binary.BigEndian.Uint32(data[1:5])

	// Compression flag should be 0 or 1
	if compressionFlag > 1 {
		return false
	}

	// Message length should be reasonable (less than 32MB)
	if messageLength > 32*1024*1024 {
		return false
	}

	return true
}

// isHTTP1Traffic detects HTTP/1.x traffic
func (p *L7Parser) isHTTP1Traffic(data []byte) bool {
	if len(data) < 4 {
		return false
	}

	// Check for HTTP methods
	methods := []string{"GET ", "POST", "PUT ", "DELETE", "HEAD", "OPTIONS", "TRACE", "CONNECT", "PATCH"}
	dataStr := string(data[:min(len(data), 20)])

	for _, method := range methods {
		if strings.HasPrefix(dataStr, method) {
			return true
		}
	}

	// Check for HTTP response
	if strings.HasPrefix(dataStr, "HTTP/1.") {
		return true
	}

	return false
}

// ParseHTTP2Frame parses HTTP/2 frame data
func (p *L7Parser) ParseHTTP2Frame(data []byte) (*HTTP2Frame, error) {
	if len(data) < 9 {
		return nil, fmt.Errorf("HTTP/2 frame too short")
	}

	length := int(data[0])<<16 | int(data[1])<<8 | int(data[2])
	frameType := data[3]
	flags := data[4]
	streamID := binary.BigEndian.Uint32(data[5:9]) & 0x7FFFFFFF // Clear reserved bit

	frame := &HTTP2Frame{
		Length:   length,
		Type:     frameType,
		Flags:    flags,
		StreamID: streamID,
	}

	if len(data) >= 9+length {
		frame.Payload = data[9 : 9+length]
	}

	return frame, nil
}

// ParseGRPCMessage parses gRPC message from payload
func (p *L7Parser) ParseGRPCMessage(data []byte) (*GRPCMessage, error) {
	if len(data) < 5 {
		return nil, fmt.Errorf("gRPC message too short")
	}

	compressionFlag := data[0] != 0
	messageLength := binary.BigEndian.Uint32(data[1:5])

	if int(messageLength)+5 > len(data) {
		return nil, fmt.Errorf("incomplete gRPC message")
	}

	message := &GRPCMessage{
		Compressed: compressionFlag,
		Length:     messageLength,
		Payload:    data[5 : 5+messageLength],
	}

	return message, nil
}

// min helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// CleanupStaleConnections removes old HTTP connection states
func (p *L7Parser) CleanupStaleConnections(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)

	for connID, state := range p.httpConnections {
		if state.LastActivity.Before(cutoff) {
			delete(p.httpConnections, connID)
		}
	}
}
