//go:build linux
// +build linux

package dns

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/dns/bpf"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 dnsMonitor ./bpf_src/dns_monitor.c -- -I../bpf_common

// eBPF components - Linux-specific
type ebpfState struct {
	objs   *bpf.DnsmonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// startEBPF initializes eBPF monitoring - Linux only
func (c *Collector) startEBPF() error {
	ctx, span := c.tracer.Start(context.Background(), "dns.ebpf.start",
		trace.WithAttributes(
			attribute.String("collector.name", c.name),
		),
	)
	defer span.End()

	// Check if eBPF is supported
	if !bpf.IsSupported() {
		c.logger.Warn("eBPF not supported on this platform")
		c.config.EnableEBPF = false
		return nil
	}

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Failed to remove memory limit")
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("error_type", "memlock_removal_failed"),
				),
			)
		}
		return fmt.Errorf("removing memory limit: %w", err)
	}

	// Load pre-compiled eBPF programs
	objs := bpf.DnsmonitorObjects{}
	if err := bpf.LoadDnsmonitorObjects(&objs, nil); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Failed to load eBPF objects")
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("error_type", "ebpf_load_failed"),
				),
			)
		}
		return fmt.Errorf("loading eBPF objects: %w", err)
	}

	c.ebpfState = &ebpfState{objs: &objs}

	// Attach eBPF programs to tracepoints
	queryLink, err := link.Tracepoint("syscalls", "sys_enter_sendto", objs.TraceDnsSendto, nil)
	if err != nil {
		objs.Close()
		return fmt.Errorf("attaching DNS sendto tracepoint: %w", err)
	}

	responseLink, err := link.Tracepoint("syscalls", "sys_exit_recvfrom", objs.TraceDnsRecvfrom, nil)
	if err != nil {
		queryLink.Close()
		objs.Close()
		return fmt.Errorf("attaching DNS recvfrom tracepoint: %w", err)
	}

	c.ebpfState.(*ebpfState).reader, err = ringbuf.NewReader(objs.DnsEvents)
	if err != nil {
		queryLink.Close()
		responseLink.Close()
		objs.Close()
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}

	c.ebpfState.(*ebpfState).links = []link.Link{queryLink, responseLink}

	span.SetAttributes(
		attribute.Bool("ebpf_enabled", c.config.EnableEBPF),
		attribute.Int("link_count", len(c.ebpfState.(*ebpfState).links)),
	)

	c.logger.Info("eBPF monitoring started successfully",
		zap.String("collector", c.name),
		zap.Int("links", len(c.ebpfState.(*ebpfState).links)),
	)

	return nil
}

// stopEBPF cleans up eBPF resources - Linux only
func (c *Collector) stopEBPF() {
	if c.ebpfState == nil {
		return
	}

	state := c.ebpfState.(*ebpfState)

	// Close reader
	if state.reader != nil {
		state.reader.Close()
	}

	// Close all links
	for _, link := range state.links {
		if err := link.Close(); err != nil {
			c.logger.Error("Failed to close eBPF link", zap.Error(err))
		}
	}

	// Close eBPF objects
	if state.objs != nil {
		state.objs.Close()
	}

	c.logger.Info("eBPF monitoring stopped", zap.String("collector", c.name))
}

// readEBPFEvents processes eBPF ring buffer events - Linux only
func (c *Collector) readEBPFEvents() {
	if c.ebpfState == nil {
		return
	}

	state := c.ebpfState.(*ebpfState)
	if state.reader == nil {
		return
	}

	ctx := c.ctx
	for {
		select {
		case <-ctx.Done():
			return
		default:
			record, err := state.reader.Read()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1,
						metric.WithAttributes(
							attribute.String("error_type", "ringbuf_read_failed"),
						),
					)
				}
				c.logger.Error("Failed to read from ring buffer", zap.Error(err))
				continue
			}

			// Parse the event
			if len(record.RawSample) < int(unsafe.Sizeof(EnhancedDNSEvent{})) {
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1,
						metric.WithAttributes(
							attribute.String("error_type", "invalid_event_size"),
						),
					)
				}
				continue
			}

			var event EnhancedDNSEvent
			if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1,
						metric.WithAttributes(
							attribute.String("error_type", "event_parse_failed"),
						),
					)
				}
				c.logger.Error("Failed to parse DNS event", zap.Error(err))
				continue
			}

			// Convert to unified event
			unifiedEvent := c.convertToUnifiedEvent(event)

			// Send to event channel
			select {
			case c.events <- unifiedEvent:
				if c.queriesTotal != nil {
					c.queriesTotal.Add(ctx, 1,
						metric.WithAttributes(
							attribute.String("protocol", fmt.Sprintf("%d", event.Protocol)),
						),
					)
				}
			case <-ctx.Done():
				return
			default:
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1,
						metric.WithAttributes(
							attribute.String("error_type", "channel_full"),
						),
					)
				}
			}
		}
	}
}

// convertToUnifiedEvent converts eBPF event to unified event format - Linux only
func (c *Collector) convertToUnifiedEvent(event EnhancedDNSEvent) collectors.RawEvent {
	// Convert timestamp to time.Time format expected by RawEvent
	timestamp := time.Unix(0, int64(event.Timestamp))
	
	// Create metadata map with proper string conversion
	metadata := map[string]string{
		"collector":  c.name,
		"pid":        fmt.Sprintf("%d", event.PID),
		"tid":        fmt.Sprintf("%d", event.TID),
		"uid":        fmt.Sprintf("%d", event.UID),
		"gid":        fmt.Sprintf("%d", event.GID),
		"cgroup_id":  fmt.Sprintf("%d", event.CgroupID),
		"protocol":   fmt.Sprintf("%d", event.Protocol),
		"ip_version": fmt.Sprintf("%d", event.IPVersion),
		"dns_id":     fmt.Sprintf("%d", event.DNSID),
		"dns_opcode": fmt.Sprintf("%d", event.DNSOpcode),
		"dns_rcode":  fmt.Sprintf("%d", event.DNSRcode),
		"dns_qtype":  fmt.Sprintf("%d", event.DNSQtype),
		"latency_ns": fmt.Sprintf("%d", event.LatencyNs),
		"src_port":   fmt.Sprintf("%d", event.SrcPort),
		"dst_port":   fmt.Sprintf("%d", event.DstPort),
	}
	
	// Convert query name from byte array to string
	queryName := ""
	for i, b := range event.QueryName {
		if b == 0 {
			queryName = string(event.QueryName[:i])
			break
		}
	}
	if queryName != "" {
		metadata["query_name"] = queryName
	}
	
	return collectors.RawEvent{
		Timestamp: timestamp,
		Type:      "dns_query",
		Data:      nil, // Raw event data would go here if needed
		Metadata:  metadata,
		TraceID:   collectors.GenerateTraceID(),
		SpanID:    collectors.GenerateSpanID(),
	}
}