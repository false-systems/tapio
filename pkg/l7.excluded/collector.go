package l7

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/events"
	"go.uber.org/zap"
)

// L7Collector collects and parses L7 protocol data
type L7Collector struct {
	httpParser  *HTTPParser
	grpcParser  *GRPCParser
	kafkaParser *KafkaParser

	logger *zap.Logger
	config *L7Config

	// Channels for different protocol flows
	httpFlows  chan *HTTPFlow
	grpcFlows  chan *GRPCFlow
	kafkaFlows chan *KafkaFlow

	// Event output
	eventChan chan *events.NetworkEvent

	// State management
	activeFlows map[string]*ActiveFlow
	running     bool
	stopChan    chan struct{}
}

// L7Config configures L7 protocol parsing
type L7Config struct {
	EnableHTTP  bool `yaml:"enable_http"`
	EnableGRPC  bool `yaml:"enable_grpc"`
	EnableKafka bool `yaml:"enable_kafka"`

	// Parser settings
	MaxHTTPBodySize    int `yaml:"max_http_body_size"`
	MaxGRPCMessageSize int `yaml:"max_grpc_message_size"`
	MaxKafkaRecordSize int `yaml:"max_kafka_record_size"`

	ParseHTTPBody    bool `yaml:"parse_http_body"`
	ParseGRPCMessage bool `yaml:"parse_grpc_message"`
	ParseKafkaRecord bool `yaml:"parse_kafka_record"`

	// Flow tracking
	FlowTimeout    time.Duration `yaml:"flow_timeout"`
	MaxActiveFlows int           `yaml:"max_active_flows"`

	// Analysis
	EnableAnalysis bool `yaml:"enable_analysis"`
	EnableMetrics  bool `yaml:"enable_metrics"`
}

// ActiveFlow tracks an ongoing L7 conversation
type ActiveFlow struct {
	ID           string
	Protocol     string
	SrcIP        string
	SrcPort      uint16
	DstIP        string
	DstPort      uint16
	StartTime    time.Time
	LastActivity time.Time

	// Protocol-specific data
	HTTPFlow  *HTTPFlow
	GRPCFlow  *GRPCFlow
	KafkaFlow *KafkaFlow
}

// NewL7Collector creates a new L7 protocol collector
func NewL7Collector(config *L7Config, logger *zap.Logger) *L7Collector {
	if config == nil {
		config = DefaultL7Config()
	}

	collector := &L7Collector{
		config:      config,
		logger:      logger,
		httpFlows:   make(chan *HTTPFlow, 1000),
		grpcFlows:   make(chan *GRPCFlow, 1000),
		kafkaFlows:  make(chan *KafkaFlow, 1000),
		eventChan:   make(chan *events.NetworkEvent, 1000),
		activeFlows: make(map[string]*ActiveFlow),
		stopChan:    make(chan struct{}),
	}

	// Initialize parsers
	if config.EnableHTTP {
		collector.httpParser = NewHTTPParser(config.MaxHTTPBodySize, config.ParseHTTPBody)
	}

	if config.EnableGRPC {
		collector.grpcParser = NewGRPCParser(config.MaxGRPCMessageSize, config.ParseGRPCMessage)
	}

	if config.EnableKafka {
		collector.kafkaParser = NewKafkaParser(config.MaxKafkaRecordSize, 1024*1024, config.ParseKafkaRecord)
	}

	return collector
}

// DefaultL7Config returns default L7 collector configuration
func DefaultL7Config() *L7Config {
	return &L7Config{
		EnableHTTP:         true,
		EnableGRPC:         true,
		EnableKafka:        true,
		MaxHTTPBodySize:    64 * 1024,       // 64KB
		MaxGRPCMessageSize: 4 * 1024 * 1024, // 4MB
		MaxKafkaRecordSize: 1024 * 1024,     // 1MB
		ParseHTTPBody:      false,           // Privacy concerns
		ParseGRPCMessage:   false,           // Privacy concerns
		ParseKafkaRecord:   false,           // Privacy concerns
		FlowTimeout:        5 * time.Minute,
		MaxActiveFlows:     10000,
		EnableAnalysis:     true,
		EnableMetrics:      true,
	}
}

// Start starts the L7 collector
func (c *L7Collector) Start(ctx context.Context) error {
	c.running = true

	// Start flow processors
	go c.processHTTPFlows(ctx)
	go c.processGRPCFlows(ctx)
	go c.processKafkaFlows(ctx)

	// Start flow cleanup
	go c.cleanupFlows(ctx)

	c.logger.Info("L7 collector started")
	return nil
}

// Stop stops the L7 collector
func (c *L7Collector) Stop() error {
	c.running = false
	close(c.stopChan)
	c.logger.Info("L7 collector stopped")
	return nil
}

// ProcessPacket processes a network packet for L7 protocol extraction
func (c *L7Collector) ProcessPacket(srcIP string, srcPort uint16, dstIP string, dstPort uint16, data []byte) {
	if !c.running || len(data) == 0 {
		return
	}

	// Detect protocol
	protocol := c.detectProtocol(data, dstPort)
	if protocol == "" {
		return // Unknown protocol
	}

	// Create or get active flow
	flowID := fmt.Sprintf("%s:%d->%s:%d", srcIP, srcPort, dstIP, dstPort)
	flow := c.getOrCreateFlow(flowID, protocol, srcIP, srcPort, dstIP, dstPort)

	// Process based on protocol
	switch protocol {
	case "http":
		if c.config.EnableHTTP && c.httpParser != nil {
			c.processHTTPPacket(flow, data)
		}
	case "grpc":
		if c.config.EnableGRPC && c.grpcParser != nil {
			c.processGRPCPacket(flow, data)
		}
	case "kafka":
		if c.config.EnableKafka && c.kafkaParser != nil {
			c.processKafkaPacket(flow, data)
		}
	}

	flow.LastActivity = time.Now()
}

// detectProtocol detects L7 protocol from packet data
func (c *L7Collector) detectProtocol(data []byte, dstPort uint16) string {
	// HTTP detection
	if isHTTPTraffic(data) {
		// Check if it's actually gRPC over HTTP/2
		if DetectGRPCTraffic(data) {
			return "grpc"
		}
		return "http"
	}

	// Kafka detection
	if DetectKafkaTraffic(data, dstPort) {
		return "kafka"
	}

	// gRPC detection (fallback)
	if DetectGRPCTraffic(data) {
		return "grpc"
	}

	return ""
}

// isHTTPTraffic detects HTTP traffic
func isHTTPTraffic(data []byte) bool {
	dataStr := string(data)

	// HTTP methods
	httpMethods := []string{"GET ", "POST ", "PUT ", "DELETE ", "HEAD ", "OPTIONS ", "PATCH "}
	for _, method := range httpMethods {
		if len(dataStr) >= len(method) && dataStr[:len(method)] == method {
			return true
		}
	}

	// HTTP response
	if len(dataStr) >= 8 && dataStr[:5] == "HTTP/" {
		return true
	}

	return false
}

// getOrCreateFlow gets existing flow or creates new one
func (c *L7Collector) getOrCreateFlow(flowID, protocol, srcIP string, srcPort uint16, dstIP string, dstPort uint16) *ActiveFlow {
	if flow, exists := c.activeFlows[flowID]; exists {
		return flow
	}

	flow := &ActiveFlow{
		ID:           flowID,
		Protocol:     protocol,
		SrcIP:        srcIP,
		SrcPort:      srcPort,
		DstIP:        dstIP,
		DstPort:      dstPort,
		StartTime:    time.Now(),
		LastActivity: time.Now(),
	}

	// Initialize protocol-specific flow
	switch protocol {
	case "http":
		flow.HTTPFlow = &HTTPFlow{
			ID:      flowID,
			SrcIP:   srcIP,
			SrcPort: srcPort,
			DstIP:   dstIP,
			DstPort: dstPort,
		}
	case "grpc":
		flow.GRPCFlow = &GRPCFlow{
			ID:      flowID,
			SrcIP:   srcIP,
			SrcPort: srcPort,
			DstIP:   dstIP,
			DstPort: dstPort,
		}
	case "kafka":
		flow.KafkaFlow = &KafkaFlow{
			ID:      flowID,
			SrcIP:   srcIP,
			SrcPort: srcPort,
			DstIP:   dstIP,
			DstPort: dstPort,
		}
	}

	c.activeFlows[flowID] = flow
	return flow
}

// processHTTPPacket processes HTTP packet data
func (c *L7Collector) processHTTPPacket(flow *ActiveFlow, data []byte) {
	if flow.HTTPFlow == nil {
		return
	}

	// Determine if request or response
	isRequest := isHTTPTraffic(data)

	if isRequest && flow.HTTPFlow.Request == nil {
		// Parse HTTP request
		req, err := c.httpParser.ParseRequest(data)
		if err != nil {
			c.logger.Debug("Failed to parse HTTP request", zap.Error(err))
			return
		}

		flow.HTTPFlow.Request = req
		flow.HTTPFlow.BytesIn += uint64(len(data))

	} else if !isRequest && flow.HTTPFlow.Response == nil {
		// Parse HTTP response
		resp, err := c.httpParser.ParseResponse(data)
		if err != nil {
			c.logger.Debug("Failed to parse HTTP response", zap.Error(err))
			return
		}

		flow.HTTPFlow.Response = resp
		flow.HTTPFlow.BytesOut += uint64(len(data))

		// Calculate latency if we have both request and response
		if flow.HTTPFlow.Request != nil {
			flow.HTTPFlow.Latency = resp.Timestamp.Sub(flow.HTTPFlow.Request.Timestamp)
		}

		// Analyze and send flow
		if c.config.EnableAnalysis {
			c.httpParser.AnalyzeFlow(flow.HTTPFlow)
		}

		// Send to channel for processing
		select {
		case c.httpFlows <- flow.HTTPFlow:
		default:
			c.logger.Warn("HTTP flow channel full, dropping flow")
		}
	}
}

// processGRPCPacket processes gRPC packet data
func (c *L7Collector) processGRPCPacket(flow *ActiveFlow, data []byte) {
	if flow.GRPCFlow == nil {
		return
	}

	// Simple heuristic: if we don't have a request yet, treat as request
	msgType := "request"
	if flow.GRPCFlow.Request != nil {
		msgType = "response"
	}

	msg, err := c.grpcParser.ParseMessage(data, msgType)
	if err != nil {
		c.logger.Debug("Failed to parse gRPC message", zap.Error(err))
		return
	}

	if msgType == "request" {
		flow.GRPCFlow.Request = msg
		flow.GRPCFlow.Service = msg.Service
		flow.GRPCFlow.Method = msg.Method
		flow.GRPCFlow.MessagesIn++
		flow.GRPCFlow.BytesIn += uint64(len(data))
	} else {
		flow.GRPCFlow.Response = msg
		flow.GRPCFlow.MessagesOut++
		flow.GRPCFlow.BytesOut += uint64(len(data))

		// Calculate latency
		if flow.GRPCFlow.Request != nil {
			flow.GRPCFlow.Latency = msg.Timestamp.Sub(flow.GRPCFlow.Request.Timestamp)
		}

		// Analyze and send flow
		if c.config.EnableAnalysis {
			c.grpcParser.AnalyzeFlow(flow.GRPCFlow)
		}

		// Send to channel for processing
		select {
		case c.grpcFlows <- flow.GRPCFlow:
		default:
			c.logger.Warn("gRPC flow channel full, dropping flow")
		}
	}
}

// processKafkaPacket processes Kafka packet data
func (c *L7Collector) processKafkaPacket(flow *ActiveFlow, data []byte) {
	if flow.KafkaFlow == nil {
		return
	}

	// Simple heuristic: if we don't have a request yet, treat as request
	msgType := "request"
	if flow.KafkaFlow.Request != nil {
		msgType = "response"
	}

	msg, err := c.kafkaParser.ParseMessage(data, msgType)
	if err != nil {
		c.logger.Debug("Failed to parse Kafka message", zap.Error(err))
		return
	}

	if msgType == "request" {
		flow.KafkaFlow.Request = msg
		flow.KafkaFlow.Topics = msg.Topics
		flow.KafkaFlow.ClientID = msg.ClientID
		flow.KafkaFlow.RecordsIn++
		flow.KafkaFlow.BytesIn += uint64(len(data))
	} else {
		flow.KafkaFlow.Response = msg
		flow.KafkaFlow.RecordsOut++
		flow.KafkaFlow.BytesOut += uint64(len(data))

		// Calculate latency
		if flow.KafkaFlow.Request != nil {
			flow.KafkaFlow.Latency = msg.Timestamp.Sub(flow.KafkaFlow.Request.Timestamp)
		}

		// Analyze and send flow
		if c.config.EnableAnalysis {
			c.kafkaParser.AnalyzeFlow(flow.KafkaFlow)
		}

		// Send to channel for processing
		select {
		case c.kafkaFlows <- flow.KafkaFlow:
		default:
			c.logger.Warn("Kafka flow channel full, dropping flow")
		}
	}
}

// processHTTPFlows processes HTTP flows and converts to events
func (c *L7Collector) processHTTPFlows(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopChan:
			return
		case flow := <-c.httpFlows:
			event := c.httpFlowToEvent(flow)
			if event != nil {
				select {
				case c.eventChan <- event:
				default:
					c.logger.Warn("Event channel full, dropping HTTP event")
				}
			}
		}
	}
}

// processGRPCFlows processes gRPC flows and converts to events
func (c *L7Collector) processGRPCFlows(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopChan:
			return
		case flow := <-c.grpcFlows:
			event := c.grpcFlowToEvent(flow)
			if event != nil {
				select {
				case c.eventChan <- event:
				default:
					c.logger.Warn("Event channel full, dropping gRPC event")
				}
			}
		}
	}
}

// processKafkaFlows processes Kafka flows and converts to events
func (c *L7Collector) processKafkaFlows(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopChan:
			return
		case flow := <-c.kafkaFlows:
			event := c.kafkaFlowToEvent(flow)
			if event != nil {
				select {
				case c.eventChan <- event:
				default:
					c.logger.Warn("Event channel full, dropping Kafka event")
				}
			}
		}
	}
}

// cleanupFlows removes expired flows
func (c *L7Collector) cleanupFlows(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopChan:
			return
		case <-ticker.C:
			now := time.Now()
			for flowID, flow := range c.activeFlows {
				if now.Sub(flow.LastActivity) > c.config.FlowTimeout {
					delete(c.activeFlows, flowID)
				}
			}

			// Limit active flows
			if len(c.activeFlows) > c.config.MaxActiveFlows {
				// Remove oldest flows (simplified - would use LRU in production)
				count := 0
				for flowID := range c.activeFlows {
					delete(c.activeFlows, flowID)
					count++
					if count > 1000 {
						break
					}
				}
			}
		}
	}
}

// Event conversion methods
func (c *L7Collector) httpFlowToEvent(flow *HTTPFlow) *events.NetworkEvent {
	event := &events.NetworkEvent{
		SrcIP:     flow.SrcIP,
		SrcPort:   flow.SrcPort,
		DstIP:     flow.DstIP,
		DstPort:   flow.DstPort,
		Protocol:  "http",
		BytesIn:   flow.BytesIn,
		BytesOut:  flow.BytesOut,
		Timestamp: time.Now(),
	}

	if flow.Request != nil {
		event.L7Data = map[string]interface{}{
			"method":     flow.Request.Method,
			"path":       flow.Request.Path,
			"host":       flow.Request.Host,
			"user_agent": flow.Request.UserAgent,
		}
	}

	if flow.Response != nil {
		if event.L7Data == nil {
			event.L7Data = make(map[string]interface{})
		}
		event.L7Data["status_code"] = flow.Response.StatusCode
		event.L7Data["latency_ms"] = flow.Latency.Milliseconds()
	}

	return event
}

func (c *L7Collector) grpcFlowToEvent(flow *GRPCFlow) *events.NetworkEvent {
	event := &events.NetworkEvent{
		SrcIP:     flow.SrcIP,
		SrcPort:   flow.SrcPort,
		DstIP:     flow.DstIP,
		DstPort:   flow.DstPort,
		Protocol:  "grpc",
		BytesIn:   flow.BytesIn,
		BytesOut:  flow.BytesOut,
		Timestamp: time.Now(),
		L7Data: map[string]interface{}{
			"service":    flow.Service,
			"method":     flow.Method,
			"status":     flow.Status,
			"latency_ms": flow.Latency.Milliseconds(),
		},
	}

	return event
}

func (c *L7Collector) kafkaFlowToEvent(flow *KafkaFlow) *events.NetworkEvent {
	event := &events.NetworkEvent{
		SrcIP:     flow.SrcIP,
		SrcPort:   flow.SrcPort,
		DstIP:     flow.DstIP,
		DstPort:   flow.DstPort,
		Protocol:  "kafka",
		BytesIn:   flow.BytesIn,
		BytesOut:  flow.BytesOut,
		Timestamp: time.Now(),
		L7Data: map[string]interface{}{
			"operation":  flow.Operation,
			"topics":     flow.Topics,
			"client_id":  flow.ClientID,
			"latency_ms": flow.Latency.Milliseconds(),
		},
	}

	return event
}

// GetEventChannel returns the channel for L7 events
func (c *L7Collector) GetEventChannel() <-chan *events.NetworkEvent {
	return c.eventChan
}
