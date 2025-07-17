package l7

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// Kafka API keys
const (
	KafkaAPIKeyProduce         = 0
	KafkaAPIKeyFetch           = 1
	KafkaAPIKeyListOffsets     = 2
	KafkaAPIKeyMetadata        = 3
	KafkaAPIKeyOffsetCommit    = 8
	KafkaAPIKeyOffsetFetch     = 9
	KafkaAPIKeyFindCoordinator = 10
	KafkaAPIKeyJoinGroup       = 11
	KafkaAPIKeyHeartbeat       = 12
	KafkaAPIKeyLeaveGroup      = 13
	KafkaAPIKeySyncGroup       = 14
	KafkaAPIKeyDescribeGroups  = 15
	KafkaAPIKeyListGroups      = 16
	KafkaAPIKeyCreateTopics    = 19
	KafkaAPIKeyDeleteTopics    = 20
)

// KafkaMessage represents a parsed Kafka message
type KafkaMessage struct {
	Type          string `json:"type"` // request/response
	APIKey        int16  `json:"api_key"`
	APIVersion    int16  `json:"api_version"`
	CorrelationID int32  `json:"correlation_id"`
	ClientID      string `json:"client_id,omitempty"`

	// Request specific
	Topics     []string `json:"topics,omitempty"`
	Partitions []int32  `json:"partitions,omitempty"`

	// Producer specific
	RequiredAcks int16          `json:"required_acks,omitempty"`
	Timeout      int32          `json:"timeout,omitempty"`
	Records      []*KafkaRecord `json:"records,omitempty"`

	// Consumer specific
	MaxWaitTime int32 `json:"max_wait_time,omitempty"`
	MinBytes    int32 `json:"min_bytes,omitempty"`
	MaxBytes    int32 `json:"max_bytes,omitempty"`

	// Response specific
	ErrorCode    int16  `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`

	// Metadata
	MessageSize int32     `json:"message_size"`
	Timestamp   time.Time `json:"timestamp"`
}

// KafkaRecord represents a Kafka record
type KafkaRecord struct {
	Topic     string    `json:"topic"`
	Partition int32     `json:"partition"`
	Offset    int64     `json:"offset"`
	Key       []byte    `json:"key,omitempty"`
	Value     []byte    `json:"value,omitempty"`
	Headers   []Header  `json:"headers,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Header represents a Kafka record header
type Header struct {
	Key   string `json:"key"`
	Value []byte `json:"value"`
}

// KafkaFlow represents a complete Kafka operation
type KafkaFlow struct {
	ID        string        `json:"id"`
	Operation string        `json:"operation"` // produce, fetch, metadata, etc.
	Request   *KafkaMessage `json:"request,omitempty"`
	Response  *KafkaMessage `json:"response,omitempty"`
	Error     string        `json:"error,omitempty"`

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

	// Kafka specific
	Topics   []string `json:"topics,omitempty"`
	ClientID string   `json:"client_id,omitempty"`
	GroupID  string   `json:"group_id,omitempty"`

	// Metrics
	Latency    time.Duration `json:"latency,omitempty"`
	RecordsIn  uint64        `json:"records_in"`
	RecordsOut uint64        `json:"records_out"`
	BytesIn    uint64        `json:"bytes_in"`
	BytesOut   uint64        `json:"bytes_out"`

	// Analysis
	Anomalies []string `json:"anomalies,omitempty"`
	Tags      []string `json:"tags,omitempty"`
}

// KafkaParser parses Kafka traffic from eBPF data
type KafkaParser struct {
	maxRecordSize  int
	parseRecords   bool
	maxRecordValue int
}

// NewKafkaParser creates a new Kafka parser
func NewKafkaParser(maxRecordSize, maxRecordValue int, parseRecords bool) *KafkaParser {
	return &KafkaParser{
		maxRecordSize:  maxRecordSize,
		maxRecordValue: maxRecordValue,
		parseRecords:   parseRecords,
	}
}

// ParseMessage parses Kafka message from raw bytes
func (p *KafkaParser) ParseMessage(data []byte, msgType string) (*KafkaMessage, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("Kafka message too short")
	}

	msg := &KafkaMessage{
		Type:      msgType,
		Timestamp: time.Now(),
	}

	offset := 0

	// Parse message size (4 bytes)
	if len(data) < offset+4 {
		return nil, fmt.Errorf("insufficient data for message size")
	}
	msg.MessageSize = int32(binary.BigEndian.Uint32(data[offset : offset+4]))
	offset += 4

	// Parse API key (2 bytes)
	if len(data) < offset+2 {
		return nil, fmt.Errorf("insufficient data for API key")
	}
	msg.APIKey = int16(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2

	// Parse API version (2 bytes)
	if len(data) < offset+2 {
		return nil, fmt.Errorf("insufficient data for API version")
	}
	msg.APIVersion = int16(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2

	// Parse correlation ID (4 bytes)
	if len(data) < offset+4 {
		return nil, fmt.Errorf("insufficient data for correlation ID")
	}
	msg.CorrelationID = int32(binary.BigEndian.Uint32(data[offset : offset+4]))
	offset += 4

	// Parse client ID length and value
	if msgType == "request" && len(data) >= offset+2 {
		clientIDLen := int16(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2

		if clientIDLen > 0 && len(data) >= offset+int(clientIDLen) {
			msg.ClientID = string(data[offset : offset+int(clientIDLen)])
			offset += int(clientIDLen)
		}
	}

	// Parse API-specific fields based on API key
	if len(data) > offset {
		p.parseAPISpecificFields(data[offset:], msg)
	}

	return msg, nil
}

// parseAPISpecificFields parses fields specific to each Kafka API
func (p *KafkaParser) parseAPISpecificFields(data []byte, msg *KafkaMessage) {
	switch msg.APIKey {
	case KafkaAPIKeyProduce:
		p.parseProduceRequest(data, msg)
	case KafkaAPIKeyFetch:
		p.parseFetchRequest(data, msg)
	case KafkaAPIKeyMetadata:
		p.parseMetadataRequest(data, msg)
	case KafkaAPIKeyHeartbeat:
		p.parseHeartbeatRequest(data, msg)
	}
}

// parseProduceRequest parses Kafka produce request
func (p *KafkaParser) parseProduceRequest(data []byte, msg *KafkaMessage) {
	offset := 0

	// Parse required acks (2 bytes)
	if len(data) >= offset+2 {
		msg.RequiredAcks = int16(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2
	}

	// Parse timeout (4 bytes)
	if len(data) >= offset+4 {
		msg.Timeout = int32(binary.BigEndian.Uint32(data[offset : offset+4]))
		offset += 4
	}

	// Parse topic data (simplified - just count topics)
	if len(data) >= offset+4 {
		topicCount := int32(binary.BigEndian.Uint32(data[offset : offset+4]))
		offset += 4

		for i := int32(0); i < topicCount && len(data) > offset+2; i++ {
			topicLen := int16(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2

			if topicLen > 0 && len(data) >= offset+int(topicLen) {
				topic := string(data[offset : offset+int(topicLen)])
				msg.Topics = append(msg.Topics, topic)
				offset += int(topicLen)

				// Skip partition data for now
				if len(data) >= offset+4 {
					partitionCount := int32(binary.BigEndian.Uint32(data[offset : offset+4]))
					offset += 4

					// Skip detailed parsing for now
					_ = partitionCount
					break
				}
			}
		}
	}
}

// parseFetchRequest parses Kafka fetch request
func (p *KafkaParser) parseFetchRequest(data []byte, msg *KafkaMessage) {
	offset := 0

	// Parse max wait time (4 bytes)
	if len(data) >= offset+4 {
		msg.MaxWaitTime = int32(binary.BigEndian.Uint32(data[offset : offset+4]))
		offset += 4
	}

	// Parse min bytes (4 bytes)
	if len(data) >= offset+4 {
		msg.MinBytes = int32(binary.BigEndian.Uint32(data[offset : offset+4]))
		offset += 4
	}

	// Parse max bytes (4 bytes) - if version supports it
	if msg.APIVersion >= 3 && len(data) >= offset+4 {
		msg.MaxBytes = int32(binary.BigEndian.Uint32(data[offset : offset+4]))
		offset += 4
	}
}

// parseMetadataRequest parses Kafka metadata request
func (p *KafkaParser) parseMetadataRequest(data []byte, msg *KafkaMessage) {
	offset := 0

	// Parse topic count (4 bytes)
	if len(data) >= offset+4 {
		topicCount := int32(binary.BigEndian.Uint32(data[offset : offset+4]))
		offset += 4

		for i := int32(0); i < topicCount && len(data) > offset+2; i++ {
			topicLen := int16(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2

			if topicLen > 0 && len(data) >= offset+int(topicLen) {
				topic := string(data[offset : offset+int(topicLen)])
				msg.Topics = append(msg.Topics, topic)
				offset += int(topicLen)
			}
		}
	}
}

// parseHeartbeatRequest parses Kafka heartbeat request
func (p *KafkaParser) parseHeartbeatRequest(data []byte, msg *KafkaMessage) {
	// Heartbeat requests are simple - just group ID, generation ID, member ID
	// For now, just mark as heartbeat
}

// AnalyzeFlow performs analysis on Kafka flow
func (p *KafkaParser) AnalyzeFlow(flow *KafkaFlow) {
	flow.Anomalies = []string{}
	flow.Tags = []string{}

	// Determine operation from API key
	if flow.Request != nil {
		switch flow.Request.APIKey {
		case KafkaAPIKeyProduce:
			flow.Operation = "produce"
			flow.Tags = append(flow.Tags, "operation:produce")
		case KafkaAPIKeyFetch:
			flow.Operation = "fetch"
			flow.Tags = append(flow.Tags, "operation:fetch")
		case KafkaAPIKeyMetadata:
			flow.Operation = "metadata"
			flow.Tags = append(flow.Tags, "operation:metadata")
		case KafkaAPIKeyHeartbeat:
			flow.Operation = "heartbeat"
			flow.Tags = append(flow.Tags, "operation:heartbeat")
		case KafkaAPIKeyOffsetCommit:
			flow.Operation = "offset_commit"
			flow.Tags = append(flow.Tags, "operation:offset_commit")
		case KafkaAPIKeyJoinGroup:
			flow.Operation = "join_group"
			flow.Tags = append(flow.Tags, "operation:join_group")
		default:
			flow.Operation = fmt.Sprintf("api_%d", flow.Request.APIKey)
		}

		// Tag by topics
		for _, topic := range flow.Request.Topics {
			flow.Tags = append(flow.Tags, "topic:"+topic)
		}

		// Tag by client
		if flow.Request.ClientID != "" {
			flow.Tags = append(flow.Tags, "client:"+flow.Request.ClientID)
		}
	}

	// Check for errors in response
	if flow.Response != nil && flow.Response.ErrorCode != 0 {
		flow.Error = p.getKafkaErrorMessage(flow.Response.ErrorCode)
		flow.Anomalies = append(flow.Anomalies, "kafka_error")
		flow.Tags = append(flow.Tags, "error")
	}

	// Check latency
	if flow.Latency > 10*time.Second {
		flow.Anomalies = append(flow.Anomalies, "high_latency")
	}

	// Check message sizes
	if flow.Request != nil && flow.Request.MessageSize > 100*1024*1024 { // 100MB
		flow.Anomalies = append(flow.Anomalies, "large_message")
	}

	// Check for potential issues
	if flow.Request != nil && flow.Response == nil && flow.Error == "" {
		flow.Anomalies = append(flow.Anomalies, "no_response")
	}

	// Detect common patterns
	if flow.Operation == "heartbeat" {
		flow.Tags = append(flow.Tags, "health_check")
	}

	if flow.Operation == "metadata" {
		flow.Tags = append(flow.Tags, "discovery")
	}
}

// getKafkaErrorMessage returns human-readable error message for Kafka error codes
func (p *KafkaParser) getKafkaErrorMessage(errorCode int16) string {
	switch errorCode {
	case 1:
		return "OFFSET_OUT_OF_RANGE"
	case 2:
		return "CORRUPT_MESSAGE"
	case 3:
		return "UNKNOWN_TOPIC_OR_PARTITION"
	case 4:
		return "INVALID_FETCH_SIZE"
	case 5:
		return "LEADER_NOT_AVAILABLE"
	case 6:
		return "NOT_LEADER_FOR_PARTITION"
	case 7:
		return "REQUEST_TIMED_OUT"
	case 8:
		return "BROKER_NOT_AVAILABLE"
	case 9:
		return "REPLICA_NOT_AVAILABLE"
	case 10:
		return "MESSAGE_TOO_LARGE"
	case 11:
		return "STALE_CONTROLLER_EPOCH"
	case 12:
		return "OFFSET_METADATA_TOO_LARGE"
	case 13:
		return "NETWORK_EXCEPTION"
	case 14:
		return "COORDINATOR_LOAD_IN_PROGRESS"
	case 15:
		return "COORDINATOR_NOT_AVAILABLE"
	case 16:
		return "NOT_COORDINATOR"
	case 17:
		return "INVALID_TOPIC_EXCEPTION"
	case 18:
		return "RECORD_LIST_TOO_LARGE"
	case 19:
		return "NOT_ENOUGH_REPLICAS"
	case 20:
		return "NOT_ENOUGH_REPLICAS_AFTER_APPEND"
	case 21:
		return "INVALID_REQUIRED_ACKS"
	case 22:
		return "ILLEGAL_GENERATION"
	case 23:
		return "INCONSISTENT_GROUP_PROTOCOL"
	case 24:
		return "INVALID_GROUP_ID"
	case 25:
		return "UNKNOWN_MEMBER_ID"
	case 26:
		return "INVALID_SESSION_TIMEOUT"
	case 27:
		return "REBALANCE_IN_PROGRESS"
	case 28:
		return "INVALID_COMMIT_OFFSET_SIZE"
	case 29:
		return "TOPIC_AUTHORIZATION_FAILED"
	case 30:
		return "GROUP_AUTHORIZATION_FAILED"
	case 31:
		return "CLUSTER_AUTHORIZATION_FAILED"
	default:
		return fmt.Sprintf("ERROR_%d", errorCode)
	}
}

// GetMetrics extracts metrics from Kafka flow
func (p *KafkaParser) GetMetrics(flow *KafkaFlow) map[string]interface{} {
	metrics := make(map[string]interface{})

	metrics["operation"] = flow.Operation
	metrics["topics"] = flow.Topics
	metrics["client_id"] = flow.ClientID
	metrics["latency_ms"] = flow.Latency.Milliseconds()
	metrics["records_in"] = flow.RecordsIn
	metrics["records_out"] = flow.RecordsOut
	metrics["bytes_in"] = flow.BytesIn
	metrics["bytes_out"] = flow.BytesOut

	if flow.Response != nil {
		metrics["error_code"] = flow.Response.ErrorCode
	}

	return metrics
}

// DetectKafkaTraffic determines if traffic is Kafka based on patterns
func DetectKafkaTraffic(data []byte, port uint16) bool {
	// Kafka commonly runs on ports 9092, 9093, etc.
	if port >= 9092 && port <= 9099 {
		// Check for Kafka message format
		if len(data) >= 8 {
			// Message size (4 bytes) + API key (2 bytes) + API version (2 bytes)
			messageSize := binary.BigEndian.Uint32(data[0:4])
			apiKey := binary.BigEndian.Uint16(data[4:6])
			apiVersion := binary.BigEndian.Uint16(data[6:8])

			// Validate message size is reasonable
			if messageSize > 0 && messageSize < 100*1024*1024 { // < 100MB
				// Validate API key is known
				if apiKey <= 50 { // Known API keys are typically < 50
					// Validate API version is reasonable
					if apiVersion <= 10 { // API versions are typically small
						return true
					}
				}
			}
		}
	}

	return false
}
