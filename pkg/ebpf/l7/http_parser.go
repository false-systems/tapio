package l7

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// HTTPRequest represents parsed HTTP request data
type HTTPRequest struct {
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	Version     string            `json:"version"`
	Host        string            `json:"host"`
	Headers     map[string]string `json:"headers"`
	ContentType string            `json:"content_type"`
	UserAgent   string            `json:"user_agent"`
	Body        []byte            `json:"-"` // Don't include in JSON
	BodySize    int               `json:"body_size"`
	Timestamp   time.Time         `json:"timestamp"`
}

// HTTPResponse represents parsed HTTP response data
type HTTPResponse struct {
	Version      string            `json:"version"`
	StatusCode   int               `json:"status_code"`
	StatusText   string            `json:"status_text"`
	Headers      map[string]string `json:"headers"`
	ContentType  string            `json:"content_type"`
	Body         []byte            `json:"-"` // Don't include in JSON
	BodySize     int               `json:"body_size"`
	ResponseTime time.Duration     `json:"response_time"`
	Timestamp    time.Time         `json:"timestamp"`
}

// HTTPFlow represents a complete HTTP transaction
type HTTPFlow struct {
	ID       string        `json:"id"`
	Request  *HTTPRequest  `json:"request"`
	Response *HTTPResponse `json:"response,omitempty"`
	Error    string        `json:"error,omitempty"`

	// Connection info
	SrcIP   string `json:"src_ip"`
	SrcPort uint16 `json:"src_port"`
	DstIP   string `json:"dst_ip"`
	DstPort uint16 `json:"dst_port"`

	// Kubernetes context
	SrcPod       string `json:"src_pod,omitempty"`
	SrcNamespace string `json:"src_namespace,omitempty"`
	DstPod       string `json:"dst_pod,omitempty"`
	DstNamespace string `json:"dst_namespace,omitempty"`
	DstService   string `json:"dst_service,omitempty"`

	// Metrics
	Latency  time.Duration `json:"latency,omitempty"`
	BytesIn  uint64        `json:"bytes_in"`
	BytesOut uint64        `json:"bytes_out"`

	// Analysis
	Anomalies []string `json:"anomalies,omitempty"`
	Tags      []string `json:"tags,omitempty"`
}

// HTTPParser parses HTTP traffic from eBPF data
type HTTPParser struct {
	maxBodySize int
	parseBody   bool
}

// NewHTTPParser creates a new HTTP parser
func NewHTTPParser(maxBodySize int, parseBody bool) *HTTPParser {
	return &HTTPParser{
		maxBodySize: maxBodySize,
		parseBody:   parseBody,
	}
}

// ParseRequest parses HTTP request from raw bytes
func (p *HTTPParser) ParseRequest(data []byte) (*HTTPRequest, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty request data")
	}

	req := &HTTPRequest{
		Headers:   make(map[string]string),
		Timestamp: time.Now(),
	}

	// Split headers and body
	parts := bytes.SplitN(data, []byte("\r\n\r\n"), 2)
	if len(parts) == 0 {
		return nil, fmt.Errorf("invalid HTTP request format")
	}

	headerData := parts[0]
	lines := bytes.Split(headerData, []byte("\r\n"))

	// Parse request line
	if len(lines) > 0 {
		requestLine := string(lines[0])
		reqParts := strings.Split(requestLine, " ")
		if len(reqParts) >= 3 {
			req.Method = reqParts[0]
			req.Path = reqParts[1]
			req.Version = reqParts[2]
		}
	}

	// Parse headers
	for i := 1; i < len(lines); i++ {
		line := string(lines[i])
		if line == "" {
			break
		}

		colonIdx := strings.Index(line, ":")
		if colonIdx > 0 {
			key := strings.TrimSpace(line[:colonIdx])
			value := strings.TrimSpace(line[colonIdx+1:])
			req.Headers[key] = value

			// Extract common headers
			switch strings.ToLower(key) {
			case "host":
				req.Host = value
			case "content-type":
				req.ContentType = value
			case "user-agent":
				req.UserAgent = value
			}
		}
	}

	// Parse body if present and enabled
	if len(parts) > 1 && p.parseBody {
		req.Body = parts[1]
		req.BodySize = len(parts[1])

		// Truncate if too large
		if req.BodySize > p.maxBodySize {
			req.Body = req.Body[:p.maxBodySize]
		}
	}

	return req, nil
}

// ParseResponse parses HTTP response from raw bytes
func (p *HTTPParser) ParseResponse(data []byte) (*HTTPResponse, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty response data")
	}

	resp := &HTTPResponse{
		Headers:   make(map[string]string),
		Timestamp: time.Now(),
	}

	// Split headers and body
	parts := bytes.SplitN(data, []byte("\r\n\r\n"), 2)
	if len(parts) == 0 {
		return nil, fmt.Errorf("invalid HTTP response format")
	}

	headerData := parts[0]
	lines := bytes.Split(headerData, []byte("\r\n"))

	// Parse status line
	if len(lines) > 0 {
		statusLine := string(lines[0])
		statusParts := strings.SplitN(statusLine, " ", 3)
		if len(statusParts) >= 3 {
			resp.Version = statusParts[0]
			if code, err := strconv.Atoi(statusParts[1]); err == nil {
				resp.StatusCode = code
			}
			resp.StatusText = statusParts[2]
		}
	}

	// Parse headers
	for i := 1; i < len(lines); i++ {
		line := string(lines[i])
		if line == "" {
			break
		}

		colonIdx := strings.Index(line, ":")
		if colonIdx > 0 {
			key := strings.TrimSpace(line[:colonIdx])
			value := strings.TrimSpace(line[colonIdx+1:])
			resp.Headers[key] = value

			// Extract content type
			if strings.ToLower(key) == "content-type" {
				resp.ContentType = value
			}
		}
	}

	// Parse body if present and enabled
	if len(parts) > 1 && p.parseBody {
		resp.Body = parts[1]
		resp.BodySize = len(parts[1])

		// Truncate if too large
		if resp.BodySize > p.maxBodySize {
			resp.Body = resp.Body[:p.maxBodySize]
		}
	}

	return resp, nil
}

// AnalyzeFlow performs analysis on HTTP flow
func (p *HTTPParser) AnalyzeFlow(flow *HTTPFlow) {
	flow.Anomalies = []string{}
	flow.Tags = []string{}

	if flow.Request != nil {
		// Check for suspicious patterns
		if strings.Contains(flow.Request.Path, "..") {
			flow.Anomalies = append(flow.Anomalies, "path_traversal_attempt")
		}

		if strings.Contains(flow.Request.Path, "<script") {
			flow.Anomalies = append(flow.Anomalies, "potential_xss")
		}

		// Tag by method
		flow.Tags = append(flow.Tags, "method:"+strings.ToLower(flow.Request.Method))

		// Tag by content type
		if flow.Request.ContentType != "" {
			if strings.Contains(flow.Request.ContentType, "json") {
				flow.Tags = append(flow.Tags, "api:json")
			} else if strings.Contains(flow.Request.ContentType, "xml") {
				flow.Tags = append(flow.Tags, "api:xml")
			}
		}

		// Check for API endpoints
		if strings.HasPrefix(flow.Request.Path, "/api/") {
			flow.Tags = append(flow.Tags, "api_call")
		}

		// Check for health checks
		if flow.Request.Path == "/health" || flow.Request.Path == "/healthz" {
			flow.Tags = append(flow.Tags, "health_check")
		}
	}

	if flow.Response != nil {
		// Tag by status
		if flow.Response.StatusCode >= 500 {
			flow.Tags = append(flow.Tags, "error:5xx")
			flow.Anomalies = append(flow.Anomalies, "server_error")
		} else if flow.Response.StatusCode >= 400 {
			flow.Tags = append(flow.Tags, "error:4xx")
		} else if flow.Response.StatusCode >= 300 {
			flow.Tags = append(flow.Tags, "redirect")
		}

		// Check latency
		if flow.Latency > 1*time.Second {
			flow.Anomalies = append(flow.Anomalies, "high_latency")
		}

		// Check response size
		if flow.Response.BodySize > 10*1024*1024 { // 10MB
			flow.Anomalies = append(flow.Anomalies, "large_response")
		}
	}

	// Check for potential issues
	if flow.Request != nil && flow.Response == nil && flow.Error == "" {
		flow.Anomalies = append(flow.Anomalies, "no_response")
	}
}

// GetMetrics extracts metrics from HTTP flow
func (p *HTTPParser) GetMetrics(flow *HTTPFlow) map[string]interface{} {
	metrics := make(map[string]interface{})

	if flow.Request != nil {
		metrics["method"] = flow.Request.Method
		metrics["path"] = flow.Request.Path
		metrics["user_agent"] = flow.Request.UserAgent
	}

	if flow.Response != nil {
		metrics["status_code"] = flow.Response.StatusCode
		metrics["response_size"] = flow.Response.BodySize
	}

	metrics["latency_ms"] = flow.Latency.Milliseconds()
	metrics["bytes_in"] = flow.BytesIn
	metrics["bytes_out"] = flow.BytesOut

	return metrics
}
