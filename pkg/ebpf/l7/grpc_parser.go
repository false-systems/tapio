package l7

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// gRPC message types
const (
	GRPCMessageTypeRequest  = "request"
	GRPCMessageTypeResponse = "response"
)

// GRPCMessage represents a parsed gRPC message
type GRPCMessage struct {
	Type        string                 `json:"type"` // request/response
	Service     string                 `json:"service"`
	Method      string                 `json:"method"`
	Headers     map[string]string      `json:"headers"`
	Metadata    map[string]interface{} `json:"metadata"`
	MessageSize uint32                 `json:"message_size"`
	StatusCode  uint32                 `json:"status_code,omitempty"`
	StatusMsg   string                 `json:"status_message,omitempty"`
	Timestamp   time.Time              `json:"timestamp"`
}

// GRPCFlow represents a complete gRPC call
type GRPCFlow struct {
	ID           string         `json:"id"`
	Service      string         `json:"service"`
	Method       string         `json:"method"`
	Request      *GRPCMessage   `json:"request,omitempty"`
	Response     *GRPCMessage   `json:"response,omitempty"`
	Status       string         `json:"status"`        // OK, CANCELLED, DEADLINE_EXCEEDED, etc.
	Error        string         `json:"error,omitempty"`
	
	// Connection info
	SrcIP        string         `json:"src_ip"`
	SrcPort      uint16         `json:"src_port"`
	DstIP        string         `json:"dst_ip"`
	DstPort      uint16         `json:"dst_port"`
	
	// Kubernetes context
	SrcPod       string         `json:"src_pod,omitempty"`
	SrcNamespace string         `json:"src_namespace,omitempty"`
	DstPod       string         `json:"dst_pod,omitempty"`
	DstNamespace string         `json:"dst_namespace,omitempty"`
	DstService   string         `json:"dst_service,omitempty"`
	
	// Metrics
	Latency      time.Duration  `json:"latency,omitempty"`
	MessagesIn   uint64         `json:"messages_in"`
	MessagesOut  uint64         `json:"messages_out"`
	BytesIn      uint64         `json:"bytes_in"`
	BytesOut     uint64         `json:"bytes_out"`
	
	// Stream tracking
	StreamID     uint32         `json:"stream_id,omitempty"`
	StreamType   string         `json:"stream_type,omitempty"` // unary, client_stream, server_stream, bidi_stream
	
	// Analysis
	Anomalies    []string       `json:"anomalies,omitempty"`
	Tags         []string       `json:"tags,omitempty"`
}

// GRPCParser parses gRPC traffic from eBPF data
type GRPCParser struct {
	maxMessageSize int
	parseMessages  bool
}

// NewGRPCParser creates a new gRPC parser
func NewGRPCParser(maxMessageSize int, parseMessages bool) *GRPCParser {
	return &GRPCParser{
		maxMessageSize: maxMessageSize,
		parseMessages:  parseMessages,
	}
}

// ParseMessage parses gRPC message from raw bytes
func (p *GRPCParser) ParseMessage(data []byte, msgType string) (*GRPCMessage, error) {
	if len(data) < 5 {
		return nil, fmt.Errorf("gRPC message too short")
	}

	msg := &GRPCMessage{
		Type:      msgType,
		Headers:   make(map[string]string),
		Metadata:  make(map[string]interface{}),
		Timestamp: time.Now(),
	}

	// Parse gRPC frame
	// First byte: compression flag
	compressed := data[0] != 0
	
	// Next 4 bytes: message length (big endian)
	msg.MessageSize = binary.BigEndian.Uint32(data[1:5])
	
	if compressed {
		msg.Metadata["compressed"] = true
	}

	// Parse HTTP/2 headers if present (simplified)
	if len(data) > 5 {
		headerData := data[5:]
		p.parseHTTP2Headers(headerData, msg)
	}

	return msg, nil
}

// parseHTTP2Headers parses HTTP/2 headers (simplified implementation)
func (p *GRPCParser) parseHTTP2Headers(data []byte, msg *GRPCMessage) {
	// This is a simplified parser - real implementation would need HPACK decoding
	
	// Look for common gRPC headers in the data
	dataStr := string(data)
	
	// Extract :path header (contains service and method)
	if pathStart := strings.Index(dataStr, ":path"); pathStart != -1 {
		pathEnd := strings.Index(dataStr[pathStart:], "\x00")
		if pathEnd != -1 {
			path := dataStr[pathStart+5 : pathStart+pathEnd]
			if strings.HasPrefix(path, "/") {
				parts := strings.Split(path[1:], "/")
				if len(parts) >= 2 {
					msg.Service = parts[0]
					msg.Method = parts[1]
				}
			}
		}
	}
	
	// Extract content-type
	if ctStart := strings.Index(dataStr, "application/grpc"); ctStart != -1 {
		msg.Headers["content-type"] = "application/grpc"
	}
	
	// Extract grpc-status (for responses)
	if msg.Type == GRPCMessageTypeResponse {
		if statusStart := strings.Index(dataStr, "grpc-status"); statusStart != -1 {
			// Try to extract status code
			statusEnd := strings.Index(dataStr[statusStart:], "\x00")
			if statusEnd != -1 {
				statusStr := dataStr[statusStart+11 : statusStart+statusEnd]
				if len(statusStr) > 0 && statusStr[0] >= '0' && statusStr[0] <= '9' {
					msg.StatusCode = uint32(statusStr[0] - '0')
				}
			}
		}
		
		// Extract grpc-message
		if msgStart := strings.Index(dataStr, "grpc-message"); msgStart != -1 {
			msgEnd := strings.Index(dataStr[msgStart:], "\x00")
			if msgEnd != -1 {
				msg.StatusMsg = dataStr[msgStart+12 : msgStart+msgEnd]
			}
		}
	}
	
	// Extract user-agent
	if uaStart := strings.Index(dataStr, "user-agent"); uaStart != -1 {
		uaEnd := strings.Index(dataStr[uaStart:], "\x00")
		if uaEnd != -1 {
			msg.Headers["user-agent"] = dataStr[uaStart+10 : uaStart+uaEnd]
		}
	}
	
	// Extract grpc-timeout
	if timeoutStart := strings.Index(dataStr, "grpc-timeout"); timeoutStart != -1 {
		timeoutEnd := strings.Index(dataStr[timeoutStart:], "\x00")
		if timeoutEnd != -1 {
			msg.Headers["grpc-timeout"] = dataStr[timeoutStart+12 : timeoutStart+timeoutEnd]
		}
	}
}

// AnalyzeFlow performs analysis on gRPC flow
func (p *GRPCParser) AnalyzeFlow(flow *GRPCFlow) {
	flow.Anomalies = []string{}
	flow.Tags = []string{}

	// Tag by service
	if flow.Service != "" {
		flow.Tags = append(flow.Tags, "service:"+flow.Service)
	}
	
	// Tag by method
	if flow.Method != "" {
		flow.Tags = append(flow.Tags, "method:"+flow.Method)
	}

	// Analyze status
	if flow.Response != nil {
		switch flow.Response.StatusCode {
		case 0: // OK
			flow.Status = "OK"
		case 1:
			flow.Status = "CANCELLED"
		case 2:
			flow.Status = "UNKNOWN"
		case 3:
			flow.Status = "INVALID_ARGUMENT"
			flow.Anomalies = append(flow.Anomalies, "invalid_argument")
		case 4:
			flow.Status = "DEADLINE_EXCEEDED"
			flow.Anomalies = append(flow.Anomalies, "deadline_exceeded")
		case 5:
			flow.Status = "NOT_FOUND"
		case 6:
			flow.Status = "ALREADY_EXISTS"
		case 7:
			flow.Status = "PERMISSION_DENIED"
			flow.Anomalies = append(flow.Anomalies, "permission_denied")
		case 8:
			flow.Status = "RESOURCE_EXHAUSTED"
			flow.Anomalies = append(flow.Anomalies, "resource_exhausted")
		case 9:
			flow.Status = "FAILED_PRECONDITION"
		case 10:
			flow.Status = "ABORTED"
		case 11:
			flow.Status = "OUT_OF_RANGE"
		case 12:
			flow.Status = "UNIMPLEMENTED"
		case 13:
			flow.Status = "INTERNAL"
			flow.Anomalies = append(flow.Anomalies, "internal_error")
		case 14:
			flow.Status = "UNAVAILABLE"
			flow.Anomalies = append(flow.Anomalies, "service_unavailable")
		case 15:
			flow.Status = "DATA_LOSS"
			flow.Anomalies = append(flow.Anomalies, "data_loss")
		case 16:
			flow.Status = "UNAUTHENTICATED"
			flow.Anomalies = append(flow.Anomalies, "unauthenticated")
		default:
			flow.Status = "UNKNOWN"
		}
		
		// Tag by status
		if flow.Response.StatusCode != 0 {
			flow.Tags = append(flow.Tags, "error")
		}
	}

	// Check latency
	if flow.Latency > 5*time.Second {
		flow.Anomalies = append(flow.Anomalies, "high_latency")
	}
	
	// Check message sizes
	if flow.Request != nil && flow.Request.MessageSize > 10*1024*1024 { // 10MB
		flow.Anomalies = append(flow.Anomalies, "large_request")
	}
	
	if flow.Response != nil && flow.Response.MessageSize > 10*1024*1024 { // 10MB
		flow.Anomalies = append(flow.Anomalies, "large_response")
	}
	
	// Detect stream types
	if flow.MessagesIn > 1 && flow.MessagesOut > 1 {
		flow.StreamType = "bidi_stream"
		flow.Tags = append(flow.Tags, "streaming:bidirectional")
	} else if flow.MessagesIn > 1 {
		flow.StreamType = "client_stream"
		flow.Tags = append(flow.Tags, "streaming:client")
	} else if flow.MessagesOut > 1 {
		flow.StreamType = "server_stream"
		flow.Tags = append(flow.Tags, "streaming:server")
	} else {
		flow.StreamType = "unary"
		flow.Tags = append(flow.Tags, "unary")
	}
	
	// Check for common patterns
	if strings.Contains(flow.Service, "health") || flow.Method == "Check" {
		flow.Tags = append(flow.Tags, "health_check")
	}
	
	if strings.Contains(flow.Service, "reflection") {
		flow.Tags = append(flow.Tags, "service_reflection")
	}
	
	// Check for potential issues
	if flow.Request != nil && flow.Response == nil && flow.Error == "" {
		flow.Anomalies = append(flow.Anomalies, "no_response")
	}
}

// GetMetrics extracts metrics from gRPC flow
func (p *GRPCParser) GetMetrics(flow *GRPCFlow) map[string]interface{} {
	metrics := make(map[string]interface{})
	
	metrics["service"] = flow.Service
	metrics["method"] = flow.Method
	metrics["status"] = flow.Status
	metrics["stream_type"] = flow.StreamType
	metrics["latency_ms"] = flow.Latency.Milliseconds()
	metrics["messages_in"] = flow.MessagesIn
	metrics["messages_out"] = flow.MessagesOut
	metrics["bytes_in"] = flow.BytesIn
	metrics["bytes_out"] = flow.BytesOut
	
	if flow.Response != nil {
		metrics["status_code"] = flow.Response.StatusCode
	}
	
	return metrics
}

// DetectGRPCTraffic determines if traffic is gRPC based on patterns
func DetectGRPCTraffic(data []byte) bool {
	dataStr := string(data)
	
	// Look for gRPC indicators
	indicators := []string{
		"application/grpc",
		"grpc-status",
		"grpc-message",
		"grpc-timeout",
		":method\x00POST",
		"HTTP/2.0",
	}
	
	for _, indicator := range indicators {
		if strings.Contains(dataStr, indicator) {
			return true
		}
	}
	
	// Check for gRPC frame format (5-byte header)
	if len(data) >= 5 {
		// First byte should be 0 or 1 (compression flag)
		if data[0] <= 1 {
			// Next 4 bytes are message length
			msgLen := binary.BigEndian.Uint32(data[1:5])
			// Reasonable message size
			if msgLen > 0 && msgLen < 100*1024*1024 { // < 100MB
				return true
			}
		}
	}
	
	return false
}