package intelligence

import (
	"context"
	"fmt"
	"sync"

	tapiopb "github.com/yairfalse/proto/gen/go/tapio/v1"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/publisher"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// PolkuConfig configures the POLKU intelligence service.
type PolkuConfig struct {
	Publisher publisher.Config
	Critical  bool
}

// Validate checks required fields.
func (c *PolkuConfig) Validate() error {
	return c.Publisher.Validate()
}

// Compile-time interface compliance check.
var _ domain.EventEmitter = (*polkuService)(nil)

// polkuService implements Service for POLKU tier.
type polkuService struct {
	pub       *publisher.Publisher
	clusterID string
	nodeName  string
	critical  bool

	mu     sync.Mutex
	closed bool
}

// NewPolkuService creates a POLKU intelligence service.
func NewPolkuService(cfg PolkuConfig) (Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	cfg.Publisher.ApplyDefaults()

	return &polkuService{
		pub:       publisher.New(cfg.Publisher),
		clusterID: cfg.Publisher.ClusterID,
		nodeName:  cfg.Publisher.NodeName,
		critical:  cfg.Critical,
	}, nil
}

// Connect establishes the connection to POLKU.
func (s *polkuService) Connect(ctx context.Context) error {
	return s.pub.Connect(ctx)
}

// Emit converts the event and publishes to POLKU.
func (s *polkuService) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	if event == nil {
		return fmt.Errorf("nil event")
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("service is closed")
	}
	s.mu.Unlock()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	raw := convertToRawEvent(event, s.clusterID, s.nodeName)
	return s.pub.Publish(raw)
}

// Name returns the service identifier.
func (s *polkuService) Name() string {
	return "intelligence-polku"
}

// IsCritical returns whether this service is critical.
func (s *polkuService) IsCritical() bool {
	return s.critical
}

// Close gracefully shuts down the service.
func (s *polkuService) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return s.pub.Close()
}

// IsConnected returns true if connected to POLKU.
func (s *polkuService) IsConnected() bool {
	return s.pub.IsConnected()
}

// convertToRawEvent converts domain.ObserverEvent to tapiopb.RawEbpfEvent.
func convertToRawEvent(event *domain.ObserverEvent, clusterID, nodeName string) *tapiopb.RawEbpfEvent {
	raw := &tapiopb.RawEbpfEvent{
		Id:        event.ID,
		Timestamp: timestamppb.New(event.Timestamp),
		Type:      convertEventType(event.Type),
		Subtype:   event.Subtype,
		ClusterId: clusterID,
		NodeName:  nodeName,
	}

	// Extract K8s context from typed data
	if event.NetworkData != nil {
		raw.Namespace = event.NetworkData.Namespace
		raw.PodName = event.NetworkData.PodName
		raw.ContainerId = event.NetworkData.ContainerID
		raw.Data = &tapiopb.RawEbpfEvent_Network{
			Network: convertNetworkData(event.NetworkData),
		}
	}

	if event.ContainerData != nil {
		raw.Namespace = event.ContainerData.PodNamespace
		raw.PodName = event.ContainerData.PodName
		raw.ContainerId = event.ContainerData.ContainerID
		raw.ContainerName = event.ContainerData.ContainerName
		raw.Data = &tapiopb.RawEbpfEvent_Container{
			Container: convertContainerData(event.ContainerData),
		}
	}

	if event.KernelData != nil {
		raw.Data = &tapiopb.RawEbpfEvent_Memory{
			Memory: convertMemoryData(event.KernelData),
		}
	}

	if event.StorageData != nil {
		raw.Namespace = event.StorageData.Namespace
		raw.PodName = event.StorageData.PodName
		raw.ContainerId = event.StorageData.ContainerID
		raw.Data = &tapiopb.RawEbpfEvent_Storage{
			Storage: convertStorageData(event.StorageData),
		}
	}

	return raw
}

func convertEventType(t string) tapiopb.EbpfType {
	switch t {
	case "network":
		return tapiopb.EbpfType_EBPF_TYPE_NETWORK
	case "container":
		return tapiopb.EbpfType_EBPF_TYPE_CONTAINER
	case "kernel":
		return tapiopb.EbpfType_EBPF_TYPE_MEMORY
	case "storage":
		return tapiopb.EbpfType_EBPF_TYPE_STORAGE
	default:
		return tapiopb.EbpfType_EBPF_TYPE_UNSPECIFIED
	}
}

func convertNetworkData(d *domain.NetworkEventData) *tapiopb.NetworkData {
	return &tapiopb.NetworkData{
		Protocol:    d.Protocol,
		SrcIp:       d.SrcIP,
		SrcPort:     uint32(d.SrcPort),
		DstIp:       d.DstIP,
		DstPort:     uint32(d.DstPort),
		BytesSent:   d.BytesSent,
		BytesRecv:   d.BytesReceived,
		RttUs:       uint64(d.RTTCurrent * 1000),
		Retransmits: uint64(d.RetransmitCount),
		DnsQuery:    d.DNSQuery,
	}
}

func convertContainerData(d *domain.ContainerEventData) *tapiopb.ContainerData {
	return &tapiopb.ContainerData{
		ContainerId:   d.ContainerID,
		ContainerName: d.ContainerName,
		Image:         d.Image,
		State:         d.State,
		ExitCode:      d.ExitCode,
		ExitReason:    d.Reason,
		Signal:        d.Signal,
		MemoryLimit:   uint64(d.MemoryLimit),
		MemoryUsage:   uint64(d.MemoryUsage),
		CgroupPath:    d.CgroupPath,
		Pid:           d.PID,
	}
}

func convertMemoryData(d *domain.KernelEventData) *tapiopb.MemoryData {
	return &tapiopb.MemoryData{
		UsageBytes: d.OOMMemoryUsage,
		LimitBytes: d.OOMMemoryLimit,
	}
}

func convertStorageData(d *domain.StorageEventData) *tapiopb.StorageData {
	device := ""
	if d.DeviceName != "" {
		device = d.DeviceName
	} else {
		device = fmt.Sprintf("%d:%d", d.DeviceMajor, d.DeviceMinor)
	}

	return &tapiopb.StorageData{
		Device:    device,
		Operation: d.OperationType,
		Bytes:     d.Bytes,
		LatencyUs: uint64(d.LatencyMs * 1000), // ms → µs
		ErrorCode: d.ErrorName,                // proto ErrorCode is string (e.g., "EIO")
	}
}
