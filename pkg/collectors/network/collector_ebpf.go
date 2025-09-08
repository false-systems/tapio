//go:build linux
// +build linux

package network

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors/network/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// eBPF generation is handled by bpf/generate.go

// ebpfState holds eBPF components for Linux
type ebpfState struct {
	objs   *bpf.NetworkmonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// NetworkEvent represents a network event from eBPF
type NetworkEvent struct {
	Timestamp       uint64
	EventType       uint32
	PID             uint32
	TID             uint32
	UID             uint32
	GID             uint32
	CGroupID        uint64
	Comm            [16]byte
	SourceIP        [16]byte
	DestIP          [16]byte
	SourcePort      uint16
	DestPort        uint16
	Protocol        uint8
	Direction       uint8
	BytesSent       uint64
	BytesReceived   uint64
	Duration        uint64
	HTTPStatus      uint16
	HTTPMethod      [8]byte
	HTTPPath        [128]byte
	DNSName         [256]byte
	DNSResponseCode uint16
	PayloadSize     uint16
	Payload         [1500]byte // Max ethernet frame payload
}

// startEBPF initializes and attaches eBPF programs for Linux
func (c *Collector) startEBPF() error {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		c.logger.Warn("Failed to remove memlock", zap.Error(err))
	}

	// Load eBPF programs
	coll, err := bpf.LoadNetworkMonitor()
	if err != nil {
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}

	objs := coll.(*bpf.NetworkmonitorObjects)

	state := &ebpfState{
		objs:  objs,
		links: make([]link.Link, 0),
	}

	// Attach tracepoints for network monitoring
	if c.config.EnableTCP {
		// Attach to TCP connect/accept
		tcpConnectLink, err := link.Tracepoint("syscalls", "sys_enter_connect", objs.TraceConnect, nil)
		if err != nil {
			c.logger.Warn("Failed to attach TCP connect tracepoint", zap.Error(err))
		} else {
			state.links = append(state.links, tcpConnectLink)
		}

		tcpAcceptLink, err := link.Tracepoint("syscalls", "sys_enter_accept4", objs.TraceAccept, nil)
		if err != nil {
			c.logger.Warn("Failed to attach TCP accept tracepoint", zap.Error(err))
		} else {
			state.links = append(state.links, tcpAcceptLink)
		}
	}

	if c.config.EnableUDP {
		// Attach to UDP sendmsg/recvmsg
		udpSendLink, err := link.Tracepoint("syscalls", "sys_enter_sendto", objs.TraceSendto, nil)
		if err != nil {
			c.logger.Warn("Failed to attach UDP sendto tracepoint", zap.Error(err))
		} else {
			state.links = append(state.links, udpSendLink)
		}
	}

	// Attach HTTP monitoring if enabled
	if c.config.EnableHTTP || c.config.EnableHTTPS {
		// This would typically attach to HTTP-related probes
		// For now, we'll rely on the network-level monitoring
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		c.cleanupEBPF(state)
		return fmt.Errorf("failed to create ring buffer reader: %w", err)
	}
	state.reader = reader

	// Fail if no probes attached
	if len(state.links) == 0 {
		c.cleanupEBPF(state)
		return fmt.Errorf("no eBPF probes or tracepoints attached")
	}

	c.ebpfState = state

	// Start event processing using LifecycleManager
	c.LifecycleManager.Start("eBPF-reader", c.readEBPFEvents)
	c.LifecycleManager.Start("L7-cleanup", c.cleanupL7Parser)

	c.logger.Info("eBPF network monitoring started",
		zap.Int("links", len(state.links)),
		zap.Bool("tcp_enabled", c.config.EnableTCP),
		zap.Bool("udp_enabled", c.config.EnableUDP),
		zap.Bool("http_enabled", c.config.EnableHTTP),
	)

	return nil
}

// stopEBPF detaches eBPF programs for Linux
func (c *Collector) stopEBPF() {
	if state, ok := c.ebpfState.(*ebpfState); ok {
		c.cleanupEBPF(state)
	}
}

// cleanupEBPF cleans up eBPF resources
func (c *Collector) cleanupEBPF(state *ebpfState) {
	if state == nil {
		return
	}

	if state.reader != nil {
		state.reader.Close()
	}

	for _, l := range state.links {
		if l != nil {
			l.Close()
		}
	}

	if state.objs != nil {
		state.objs.Close()
	}
}

// readEBPFEvents reads events from eBPF ring buffer
func (c *Collector) readEBPFEvents() {

	state, ok := c.ebpfState.(*ebpfState)
	if !ok || state == nil || state.reader == nil {
		c.logger.Error("Invalid eBPF state")
		return
	}

	c.logger.Info("Starting eBPF event reader")

	for {
		select {
		case <-c.LifecycleManager.Context().Done():
			c.logger.Info("Stopping eBPF event reader")
			return
		default:
		}

		record, err := state.reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				c.logger.Info("Ring buffer closed")
				return
			}
			c.logger.Warn("Error reading from ring buffer", zap.Error(err))
			continue
		}

		// Parse and process the event
		if len(record.RawSample) < 4 {
			c.logger.Warn("Invalid event size", zap.Int("size", len(record.RawSample)))
			continue
		}

		c.processRawNetworkEvent(record.RawSample)
	}
}

// processRawNetworkEvent processes raw eBPF network event data
func (c *Collector) processRawNetworkEvent(data []byte) {
	// Parse the network event
	var event NetworkEvent
	buf := bytes.NewBuffer(data)

	// Read fields in order matching C struct
	binary.Read(buf, binary.LittleEndian, &event.Timestamp)
	binary.Read(buf, binary.LittleEndian, &event.EventType)
	binary.Read(buf, binary.LittleEndian, &event.PID)
	binary.Read(buf, binary.LittleEndian, &event.TID)
	binary.Read(buf, binary.LittleEndian, &event.UID)
	binary.Read(buf, binary.LittleEndian, &event.GID)
	binary.Read(buf, binary.LittleEndian, &event.CGroupID)
	copy(event.Comm[:], buf.Next(16))
	copy(event.SourceIP[:], buf.Next(16))
	copy(event.DestIP[:], buf.Next(16))
	binary.Read(buf, binary.LittleEndian, &event.SourcePort)
	binary.Read(buf, binary.LittleEndian, &event.DestPort)
	binary.Read(buf, binary.LittleEndian, &event.Protocol)
	binary.Read(buf, binary.LittleEndian, &event.Direction)
	binary.Read(buf, binary.LittleEndian, &event.BytesSent)
	binary.Read(buf, binary.LittleEndian, &event.BytesReceived)
	binary.Read(buf, binary.LittleEndian, &event.Duration)
	binary.Read(buf, binary.LittleEndian, &event.HTTPStatus)
	copy(event.HTTPMethod[:], buf.Next(8))
	copy(event.HTTPPath[:], buf.Next(128))
	copy(event.DNSName[:], buf.Next(256))
	binary.Read(buf, binary.LittleEndian, &event.DNSResponseCode)
	binary.Read(buf, binary.LittleEndian, &event.PayloadSize)
	copy(event.Payload[:], buf.Next(1500))

	// Convert to domain event
	domainEvent := c.convertNetworkEventToDomain(&event)
	if domainEvent == nil {
		return
	}

	// Send event using base EventChannelManager (handles drops automatically)
	if !c.EventChannelManager.SendEvent(domainEvent) {
		// Event was dropped (EventChannelManager already logged it)
		c.RecordError(fmt.Errorf("event dropped due to full channel"))
	} else {
		// Event sent successfully
		c.RecordEvent()
	}
}

// convertNetworkEventToDomain converts eBPF network event to domain event with L7 parsing
func (c *Collector) convertNetworkEventToDomain(event *NetworkEvent) *domain.CollectorEvent {
	state, ok := c.ebpfState.(*ebpfState)
	if !ok || state == nil {
		return nil
	}

	eventID := fmt.Sprintf("network-%d-%d", event.EventType, time.Now().UnixNano())

	// Determine event type and severity
	var eventType domain.CollectorEventType
	var severity domain.EventSeverity = domain.EventSeverityInfo

	switch event.EventType {
	case 1: // TCP connection
		eventType = domain.EventTypeTCP
	case 2: // UDP packet
		eventType = domain.EventTypeUDP
	case 3: // HTTP request/response
		eventType = domain.EventTypeHTTP
	case 4: // DNS query/response
		eventType = domain.EventTypeDNS
	default:
		eventType = domain.EventTypeKernelNetwork
	}

	// Build network data
	networkData := &domain.NetworkData{
		Protocol:      c.getProtocolName(event.Protocol),
		Direction:     c.getDirectionName(event.Direction),
		SourceIP:      c.formatIP(event.SourceIP[:]),
		DestIP:        c.formatIP(event.DestIP[:]),
		SourcePort:    int32(event.SourcePort),
		DestPort:      int32(event.DestPort),
		BytesSent:     int64(event.BytesSent),
		BytesReceived: int64(event.BytesReceived),
	}

	if event.Duration > 0 {
		networkData.Latency = time.Duration(event.Duration)
	}

	// Build process data
	processData := &domain.ProcessData{
		PID:     int32(event.PID),
		TID:     int32(event.TID),
		Command: c.extractString(event.Comm[:]),
		UID:     int32(event.UID),
		GID:     int32(event.GID),
	}

	// Build event data container
	eventDataContainer := domain.EventDataContainer{
		Network: networkData,
		Process: processData,
	}

	// Create connection ID for L7 parsing
	connectionID := fmt.Sprintf("%s:%d->%s:%d",
		networkData.SourceIP, networkData.SourcePort,
		networkData.DestIP, networkData.DestPort)

	// Enhanced L7 parsing with protocol detection
	if event.PayloadSize > 0 && event.PayloadSize <= 1500 {
		payload := event.Payload[:event.PayloadSize]

		// Detect actual L7 protocol from payload
		detectedProtocol := state.l7Parser.DetectProtocol(payload)

		// Use enhanced parsers for deeper L7 analysis
		switch detectedProtocol {
		case "http1", "http2":
			c.parseHTTPPayload(state.l7Parser, connectionID, payload, event, &eventDataContainer, &severity)
		case "grpc":
			c.parseGRPCPayload(state.l7Parser, connectionID, payload, event, &eventDataContainer, &severity)
		case "dns":
			if event.Protocol == 17 { // UDP
				c.parseDNSPayload(state.l7Parser, payload, event, &eventDataContainer, &severity)
			}
		default:
			// Fallback based on event type
			switch event.EventType {
			case 3: // HTTP traffic
				c.parseHTTPPayload(state.l7Parser, connectionID, payload, event, &eventDataContainer, &severity)
			case 4: // DNS traffic
				c.parseDNSPayload(state.l7Parser, payload, event, &eventDataContainer, &severity)
			}
		}

		// Add protocol detection metadata
		if eventDataContainer.Custom == nil {
			eventDataContainer.Custom = make(map[string]string)
		}
		eventDataContainer.Custom["detected_protocol"] = detectedProtocol
	} else {
		// Fallback to basic parsing for backwards compatibility
		c.parseBasicL7Data(event, &eventDataContainer, &severity)
	}

	return &domain.CollectorEvent{
		EventID:   eventID,
		Timestamp: time.Unix(0, int64(event.Timestamp)),
		Type:      eventType,
		Source:    c.name,
		Severity:  severity,
		EventData: eventDataContainer,
		Metadata: domain.EventMetadata{
			PID:      int32(event.PID),
			TID:      int32(event.TID),
			Command:  c.extractString(event.Comm[:]),
			CgroupID: event.CGroupID,
			Priority: domain.PriorityNormal,
			Tags:     []string{"network", "ebpf", "l7"},
			Labels: map[string]string{
				"protocol":     c.getProtocolName(event.Protocol),
				"direction":    c.getDirectionName(event.Direction),
				"payload_size": fmt.Sprintf("%d", event.PayloadSize),
			},
		},
	}
}

// parseHTTPPayload uses L7Parser to extract detailed HTTP information
func (c *Collector) parseHTTPPayload(parser *L7Parser, connectionID string, payload []byte, event *NetworkEvent, eventData *domain.EventDataContainer, severity *domain.EventSeverity) {
	if event.Direction == 1 { // Outbound (request)
		if httpReq, err := parser.ParseHTTPRequest(connectionID, payload); err == nil {
			eventData.HTTP = &domain.HTTPData{
				Method:      httpReq.Method,
				URL:         httpReq.URL,
				UserAgent:   httpReq.UserAgent,
				Headers:     httpReq.Headers,
				ContentType: httpReq.ContentType,
				RequestSize: httpReq.BodySize,
			}

			// Add enhanced metadata
			if eventData.HTTP.Headers != nil {
				for key, value := range eventData.HTTP.Headers {
					eventData.Custom = make(map[string]string)
					eventData.Custom[fmt.Sprintf("http_header_%s", key)] = value
				}
			}
		}
	} else { // Inbound (response)
		if httpResp, err := parser.ParseHTTPResponse(connectionID, payload); err == nil {
			if eventData.HTTP == nil {
				eventData.HTTP = &domain.HTTPData{}
			}

			eventData.HTTP.StatusCode = int32(httpResp.StatusCode)
			eventData.HTTP.Headers = httpResp.Headers // Response headers
			eventData.HTTP.ContentType = httpResp.ContentType
			eventData.HTTP.ResponseSize = httpResp.BodySize
			eventData.HTTP.Duration = httpResp.ResponseTime

			// Determine severity based on status code
			if httpResp.StatusCode >= 400 && httpResp.StatusCode < 500 {
				*severity = domain.EventSeverityWarning
			} else if httpResp.StatusCode >= 500 {
				*severity = domain.EventSeverityError
			}

			// Add response metadata
			if eventData.Custom == nil {
				eventData.Custom = make(map[string]string)
			}
			eventData.Custom["http_status_category"] = c.getHTTPStatusCategory(httpResp.StatusCode)
			eventData.Custom["http_response_time_ms"] = fmt.Sprintf("%.2f", httpResp.ResponseTime.Seconds()*1000)
		}
	}
}

// parseHTTP2Payload uses L7Parser to extract HTTP/2 frame information
func (c *Collector) parseHTTP2Payload(parser *L7Parser, connectionID string, payload []byte, event *NetworkEvent, eventData *domain.EventDataContainer, severity *domain.EventSeverity) {
	frame, err := parser.ParseHTTP2Frame(payload)
	if err != nil {
		c.logger.Debug("Failed to parse HTTP/2 frame", zap.Error(err))
		return
	}

	// Create HTTP data structure for HTTP/2
	if eventData.HTTP == nil {
		eventData.HTTP = &domain.HTTPData{}
	}

	// Add HTTP/2 specific metadata
	if eventData.Custom == nil {
		eventData.Custom = make(map[string]string)
	}

	eventData.Custom["http2_frame_type"] = fmt.Sprintf("%d", frame.Type)
	eventData.Custom["http2_frame_flags"] = fmt.Sprintf("%d", frame.Flags)
	eventData.Custom["http2_stream_id"] = fmt.Sprintf("%d", frame.StreamID)
	eventData.Custom["http2_frame_length"] = fmt.Sprintf("%d", frame.Length)

	// Parse different HTTP/2 frame types
	switch frame.Type {
	case 0: // DATA frame
		eventData.Custom["http2_frame_name"] = "DATA"
		eventData.HTTP.ResponseSize = int64(frame.Length)
	case 1: // HEADERS frame
		eventData.Custom["http2_frame_name"] = "HEADERS"
		// TODO: Parse HPACK encoded headers
	case 2: // PRIORITY frame
		eventData.Custom["http2_frame_name"] = "PRIORITY"
	case 3: // RST_STREAM frame
		eventData.Custom["http2_frame_name"] = "RST_STREAM"
		*severity = domain.EventSeverityWarning
	case 4: // SETTINGS frame
		eventData.Custom["http2_frame_name"] = "SETTINGS"
	case 5: // PUSH_PROMISE frame
		eventData.Custom["http2_frame_name"] = "PUSH_PROMISE"
	case 6: // PING frame
		eventData.Custom["http2_frame_name"] = "PING"
	case 7: // GOAWAY frame
		eventData.Custom["http2_frame_name"] = "GOAWAY"
		*severity = domain.EventSeverityWarning
	case 8: // WINDOW_UPDATE frame
		eventData.Custom["http2_frame_name"] = "WINDOW_UPDATE"
	default:
		eventData.Custom["http2_frame_name"] = "UNKNOWN"
	}
}

// parseGRPCPayload uses L7Parser to extract gRPC message information
func (c *Collector) parseGRPCPayload(parser *L7Parser, connectionID string, payload []byte, event *NetworkEvent, eventData *domain.EventDataContainer, severity *domain.EventSeverity) {
	message, err := parser.ParseGRPCMessage(payload)
	if err != nil {
		c.logger.Debug("Failed to parse gRPC message", zap.Error(err))
		return
	}

	// Create GRPC data structure
	eventData.GRPC = &domain.GRPCData{
		RequestSize:  int64(message.Length),
		ResponseSize: int64(message.Length),
	}

	// Add gRPC metadata
	if eventData.Custom == nil {
		eventData.Custom = make(map[string]string)
	}

	eventData.Custom["grpc_compressed"] = fmt.Sprintf("%v", message.Compressed)
	eventData.Custom["grpc_message_length"] = fmt.Sprintf("%d", message.Length)

	if message.Service != "" {
		eventData.GRPC.Service = message.Service
	}

	if message.Method != "" {
		eventData.GRPC.Method = message.Method
	}

	// Detect potential gRPC service and method from connection context
	// This would require more sophisticated parsing of HTTP/2 headers
	// For now, we'll set generic values
	if eventData.GRPC.Service == "" {
		eventData.GRPC.Service = "unknown"
	}

	if eventData.GRPC.Method == "" {
		eventData.GRPC.Method = "unknown"
	}
}

// parseDNSPayload uses L7Parser to extract detailed DNS information
func (c *Collector) parseDNSPayload(parser *L7Parser, payload []byte, event *NetworkEvent, eventData *domain.EventDataContainer, severity *domain.EventSeverity) {
	query, response, err := parser.ParseDNSPacket(payload)
	if err != nil {
		c.logger.Debug("Failed to parse DNS packet", zap.Error(err))
		return
	}

	eventData.DNS = &domain.DNSData{}

	if query != nil {
		eventData.DNS.QueryName = query.Name
		eventData.DNS.QueryType = query.Type
	}

	if response != nil {
		// Map string response code to int for domain compatibility
		if code, exists := getResponseCodeInt(response.ResponseCode); exists {
			eventData.DNS.ResponseCode = code
		}

		// Convert answers to domain format
		domainAnswers := make([]string, len(response.Answers))
		for i, answer := range response.Answers {
			domainAnswers[i] = answer.Data
		}
		eventData.DNS.Answers = domainAnswers

		// Add answer details
		if len(response.Answers) > 0 {
			eventData.Custom = make(map[string]string)
			for i, answer := range response.Answers {
				eventData.Custom[fmt.Sprintf("dns_answer_%d_name", i)] = answer.Name
				eventData.Custom[fmt.Sprintf("dns_answer_%d_type", i)] = answer.Type
				eventData.Custom[fmt.Sprintf("dns_answer_%d_data", i)] = answer.Data
				eventData.Custom[fmt.Sprintf("dns_answer_%d_ttl", i)] = fmt.Sprintf("%d", answer.TTL)
			}
		}

		// Determine severity based on response code
		switch response.ResponseCode {
		case "NXDOMAIN", "SERVFAIL", "REFUSED":
			*severity = domain.EventSeverityWarning
		case "FORMERR":
			*severity = domain.EventSeverityError
		}
	}
}

// parseBasicL7Data provides fallback parsing using basic fields from eBPF
func (c *Collector) parseBasicL7Data(event *NetworkEvent, eventData *domain.EventDataContainer, severity *domain.EventSeverity) {
	// Basic HTTP parsing from eBPF fields
	if event.HTTPStatus > 0 {
		eventData.HTTP = &domain.HTTPData{
			Method:     c.extractString(event.HTTPMethod[:]),
			URL:        c.extractString(event.HTTPPath[:]),
			StatusCode: int32(event.HTTPStatus),
		}

		if event.HTTPStatus >= 400 && event.HTTPStatus < 500 {
			*severity = domain.EventSeverityWarning
		} else if event.HTTPStatus >= 500 {
			*severity = domain.EventSeverityError
		}
	}

	// Basic DNS parsing from eBPF fields
	if len(c.extractString(event.DNSName[:])) > 0 {
		eventData.DNS = &domain.DNSData{
			QueryName:    c.extractString(event.DNSName[:]),
			ResponseCode: c.getDNSResponseCodeName(int(event.DNSResponseCode)),
		}

		if event.DNSResponseCode != 0 {
			*severity = domain.EventSeverityWarning
		}
	}
}

// getResponseCodeInt converts DNS response code string to int for domain compatibility
func getResponseCodeInt(responseCode string) (int, bool) {
	for code, name := range DNSResponseCodes {
		if name == responseCode {
			return code, true
		}
	}
	return 0, false
}
