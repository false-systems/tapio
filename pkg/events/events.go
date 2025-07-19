// Package events provides event types and gRPC service definitions
// This is a placeholder until proto generation is set up
package events

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// UnifiedEvent represents a unified event structure
type UnifiedEvent struct {
	Id          string
	Timestamp   *timestamppb.Timestamp
	Metadata    *EventMetadata
	Source      *EventSource
	Entity      *EntityContext
	Attributes  map[string]*AttributeValue
	Labels      map[string]string
	Correlation *CorrelationContext
	Quality     *QualityMetadata
}

// EventMetadata contains event classification info
type EventMetadata struct {
	Type        string
	Category    EventCategory
	Severity    EventSeverity
	Priority    int32
	Persistent  bool
	TtlSeconds  int64
	RoutingKeys []string
}

// EventSource identifies event origin
type EventSource struct {
	Type      string
	Collector string
	Node      string
	Cluster   string
	Version   string
	Metadata  map[string]string
}

// EntityContext identifies associated entity
type EntityContext struct {
	Type      EntityType
	Id        string
	Name      string
	Namespace string
	Parent    *EntityReference
	Process   *ProcessInfo
	Container *ContainerInfo
	Pod       *PodInfo
	Node      *NodeInfo
}

// EntityReference is a lightweight entity reference
type EntityReference struct {
	Type EntityType
	Id   string
	Name string
}

// ProcessInfo contains process information
type ProcessInfo struct {
	Pid    uint32
	Ppid   uint32
	Comm   string
	Exe    string
	Cwd    string
	Args   []string
	Env    map[string]string
	Uid    uint32
	Gid    uint32
	Cgroup string
}

// ContainerInfo contains container information
type ContainerInfo struct {
	Id      string
	Name    string
	Image   string
	Runtime string
	Labels  map[string]string
}

// PodInfo contains Kubernetes pod information
type PodInfo struct {
	Uid         string
	Name        string
	Namespace   string
	Labels      map[string]string
	Annotations map[string]string
	Containers  []*ContainerInfo
}

// NodeInfo contains node information
type NodeInfo struct {
	Name          string
	Uid           string
	Labels        map[string]string
	KernelVersion string
	OsImage       string
}

// CorrelationContext links related events
type CorrelationContext struct {
	CorrelationId string
	TraceId       string
	SpanId        string
	ParentSpanId  string
	CausedBy      []string
	RelatedEvents []string
	SessionId     string
	UserId        string
}

// QualityMetadata provides confidence indicators
type QualityMetadata struct {
	Confidence          float32
	SamplingRate        float32
	Partial             bool
	QualityFlags        []string
	ProcessingLatencyUs int64
}

// AttributeValue holds typed values
type AttributeValue struct {
	Value isAttributeValue_Value
}

type isAttributeValue_Value interface {
	isAttributeValue_Value()
}

type AttributeValue_StringValue struct {
	StringValue string
}

type AttributeValue_IntValue struct {
	IntValue int64
}

type AttributeValue_DoubleValue struct {
	DoubleValue float64
}

type AttributeValue_BoolValue struct {
	BoolValue bool
}

type AttributeValue_BytesValue struct {
	BytesValue []byte
}

func (*AttributeValue_StringValue) isAttributeValue_Value() {}
func (*AttributeValue_IntValue) isAttributeValue_Value()    {}
func (*AttributeValue_DoubleValue) isAttributeValue_Value() {}
func (*AttributeValue_BoolValue) isAttributeValue_Value()   {}
func (*AttributeValue_BytesValue) isAttributeValue_Value()  {}

// EventCategory enumerates event categories
type EventCategory int32

const (
	EventCategory_CATEGORY_UNKNOWN        EventCategory = 0
	EventCategory_CATEGORY_NETWORK        EventCategory = 1
	EventCategory_CATEGORY_MEMORY         EventCategory = 2
	EventCategory_CATEGORY_CPU            EventCategory = 3
	EventCategory_CATEGORY_IO             EventCategory = 4
	EventCategory_CATEGORY_SYSTEM         EventCategory = 5
	EventCategory_CATEGORY_SECURITY       EventCategory = 6
	EventCategory_CATEGORY_APPLICATION    EventCategory = 7
	EventCategory_CATEGORY_INFRASTRUCTURE EventCategory = 8
	EventCategory_CATEGORY_OBSERVABILITY  EventCategory = 9
)

// EventSeverity enumerates severity levels
type EventSeverity int32

const (
	EventSeverity_SEVERITY_UNKNOWN  EventSeverity = 0
	EventSeverity_SEVERITY_DEBUG    EventSeverity = 1
	EventSeverity_SEVERITY_INFO     EventSeverity = 2
	EventSeverity_SEVERITY_WARNING  EventSeverity = 3
	EventSeverity_SEVERITY_ERROR    EventSeverity = 4
	EventSeverity_SEVERITY_CRITICAL EventSeverity = 5
)

// EntityType enumerates entity types
type EntityType int32

const (
	EntityType_ENTITY_UNKNOWN     EntityType = 0
	EntityType_ENTITY_PROCESS     EntityType = 1
	EntityType_ENTITY_THREAD      EntityType = 2
	EntityType_ENTITY_CONTAINER   EntityType = 3
	EntityType_ENTITY_POD         EntityType = 4
	EntityType_ENTITY_SERVICE     EntityType = 5
	EntityType_ENTITY_NODE        EntityType = 6
	EntityType_ENTITY_CLUSTER     EntityType = 7
	EntityType_ENTITY_NAMESPACE   EntityType = 8
	EntityType_ENTITY_DEPLOYMENT  EntityType = 9
	EntityType_ENTITY_DAEMONSET   EntityType = 10
	EntityType_ENTITY_STATEFULSET EntityType = 11
	EntityType_ENTITY_JOB         EntityType = 12
	EntityType_ENTITY_CRONJOB     EntityType = 13
)

// EventBatch for bulk operations
type EventBatch struct {
	Events        []*UnifiedEvent
	BatchId       string
	CreatedAt     *timestamppb.Timestamp
	Source        string
	DroppedEvents uint32
	Compression   CompressionType
}

// CompressionType enumerates compression types
type CompressionType int32

const (
	CompressionType_COMPRESSION_NONE   CompressionType = 0
	CompressionType_COMPRESSION_GZIP   CompressionType = 1
	CompressionType_COMPRESSION_ZSTD   CompressionType = 2
	CompressionType_COMPRESSION_LZ4    CompressionType = 3
	CompressionType_COMPRESSION_SNAPPY CompressionType = 4
)

// EventResponse is the response for event operations
type EventResponse struct {
	Success  bool
	Message  string
	EventId  string
	Errors   []string
	Metadata map[string]string
}

// EventServiceClient is the client API for EventService
type EventServiceClient interface {
	SendEvent(ctx context.Context, in *UnifiedEvent, opts ...grpc.CallOption) (*EventResponse, error)
	SendEventBatch(ctx context.Context, in *EventBatch, opts ...grpc.CallOption) (*EventResponse, error)
	StreamEvents(ctx context.Context, opts ...grpc.CallOption) (EventService_StreamEventsClient, error)
	ProcessEventStream(ctx context.Context, opts ...grpc.CallOption) (EventService_ProcessEventStreamClient, error)
}

// EventService_StreamEventsClient is the client streaming interface
type EventService_StreamEventsClient interface {
	Send(*UnifiedEvent) error
	CloseAndRecv() (*EventResponse, error)
	grpc.ClientStream
}

// EventService_ProcessEventStreamClient is the bidirectional streaming interface
type EventService_ProcessEventStreamClient interface {
	Send(*UnifiedEvent) error
	Recv() (*EventResponse, error)
	grpc.ClientStream
}

// eventServiceClient implements EventServiceClient
type eventServiceClient struct {
	cc grpc.ClientConnInterface
}

// NewEventServiceClient creates a new event service client
func NewEventServiceClient(cc grpc.ClientConnInterface) EventServiceClient {
	return &eventServiceClient{cc}
}

func (c *eventServiceClient) SendEvent(ctx context.Context, in *UnifiedEvent, opts ...grpc.CallOption) (*EventResponse, error) {
	out := new(EventResponse)
	err := c.cc.Invoke(ctx, "/tapio.events.v1.EventService/SendEvent", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *eventServiceClient) SendEventBatch(ctx context.Context, in *EventBatch, opts ...grpc.CallOption) (*EventResponse, error) {
	out := new(EventResponse)
	err := c.cc.Invoke(ctx, "/tapio.events.v1.EventService/SendEventBatch", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *eventServiceClient) StreamEvents(ctx context.Context, opts ...grpc.CallOption) (EventService_StreamEventsClient, error) {
	stream, err := c.cc.NewStream(ctx, &eventService_StreamEventsDesc, "/tapio.events.v1.EventService/StreamEvents", opts...)
	if err != nil {
		return nil, err
	}
	x := &eventServiceStreamEventsClient{stream}
	return x, nil
}

func (c *eventServiceClient) ProcessEventStream(ctx context.Context, opts ...grpc.CallOption) (EventService_ProcessEventStreamClient, error) {
	stream, err := c.cc.NewStream(ctx, &eventService_ProcessEventStreamDesc, "/tapio.events.v1.EventService/ProcessEventStream", opts...)
	if err != nil {
		return nil, err
	}
	x := &eventServiceProcessEventStreamClient{stream}
	return x, nil
}

// Stream client implementations
type eventServiceStreamEventsClient struct {
	grpc.ClientStream
}

func (x *eventServiceStreamEventsClient) Send(m *UnifiedEvent) error {
	return x.ClientStream.SendMsg(m)
}

func (x *eventServiceStreamEventsClient) CloseAndRecv() (*EventResponse, error) {
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	m := new(EventResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

type eventServiceProcessEventStreamClient struct {
	grpc.ClientStream
}

func (x *eventServiceProcessEventStreamClient) Send(m *UnifiedEvent) error {
	return x.ClientStream.SendMsg(m)
}

func (x *eventServiceProcessEventStreamClient) Recv() (*EventResponse, error) {
	m := new(EventResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// Stream descriptors
var eventService_StreamEventsDesc = grpc.StreamDesc{
	StreamName:    "StreamEvents",
	ClientStreams: true,
}

var eventService_ProcessEventStreamDesc = grpc.StreamDesc{
	StreamName:    "ProcessEventStream",
	ClientStreams: true,
	ServerStreams: true,
}
