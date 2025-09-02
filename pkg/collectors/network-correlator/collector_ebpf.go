//go:build linux
// +build linux

package networkcorrelator

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/vishvananda/netlink"
	"go.uber.org/zap"
)

// Platform-specific eBPF implementation for Linux

func (c *Collector) startPlatformSpecific(ctx context.Context) error {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	// Load eBPF programs
	spec, err := loadNetworkmonitor()
	if err != nil {
		return fmt.Errorf("failed to load eBPF spec: %w", err)
	}

	c.collection, err = ebpf.NewCollection(spec)
	if err != nil {
		return fmt.Errorf("failed to create eBPF collection: %w", err)
	}

	// Attach to TC on interfaces
	if err := c.attachToInterfaces(); err != nil {
		return fmt.Errorf("failed to attach to interfaces: %w", err)
	}

	// Open ring buffer
	c.perfReader, err = ringbuf.NewReader(c.collection.Maps["failure_events"])
	if err != nil {
		return fmt.Errorf("failed to create ring buffer reader: %w", err)
	}

	// Start reading events
	c.LifecycleManager.StartGoroutine("event-reader", func(ctx context.Context) error {
		return c.readEvents(ctx)
	})

	return nil
}

func (c *Collector) stopPlatformSpecific() {
	// Close ring buffer
	if c.perfReader != nil {
		c.perfReader.Close()
	}

	// Detach eBPF programs
	for _, link := range c.tcLinks {
		link.Close()
	}

	// Close eBPF collection
	if c.collection != nil {
		c.collection.Close()
	}
}

// Attach eBPF programs to network interfaces
func (c *Collector) attachToInterfaces() error {
	interfaces := c.config.Interfaces

	// If no interfaces specified, attach to all
	if len(interfaces) == 0 {
		links, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("failed to list interfaces: %w", err)
		}

		for _, link := range links {
			// Skip loopback and non-ethernet interfaces
			if link.Type() == "veth" || link.Type() == "bridge" {
				interfaces = append(interfaces, link.Attrs().Name)
			}
		}
	}

	// Attach TC programs to each interface
	for _, ifname := range interfaces {
		iface, err := netlink.LinkByName(ifname)
		if err != nil {
			c.logger.Warn("Failed to get interface",
				zap.String("interface", ifname),
				zap.Error(err))
			continue
		}

		// Attach ingress
		ingressLink, err := link.AttachTC(link.TCOptions{
			Program:   c.collection.Programs["track_tcp_syn"],
			Attach:    ebpf.AttachTCIngress,
			Interface: iface.Attrs().Index,
		})
		if err != nil {
			c.logger.Warn("Failed to attach TC ingress",
				zap.String("interface", ifname),
				zap.Error(err))
			continue
		}
		c.tcLinks = append(c.tcLinks, ingressLink)

		c.logger.Info("Attached to interface",
			zap.String("interface", ifname))
	}

	if len(c.tcLinks) == 0 {
		return fmt.Errorf("failed to attach to any interface")
	}

	return nil
}

// Read events from eBPF ring buffer
func (c *Collector) readEvents(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		record, err := c.perfReader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return err
			}
			c.BaseCollector.RecordError(err)
			continue
		}

		// Parse the raw event based on the C struct
		if err := c.parseAndProcessEvent(record.RawSample); err != nil {
			c.BaseCollector.RecordError(err)
			continue
		}

		c.BaseCollector.RecordEvent()
	}
}

// Parse raw eBPF event
func (c *Collector) parseAndProcessEvent(raw []byte) error {
	// Parse based on C struct network_event
	if len(raw) < 88 { // Minimum size of our struct
		return fmt.Errorf("event too small: %d bytes", len(raw))
	}

	reader := bytes.NewReader(raw)

	// Read fields matching C struct
	var timestamp uint64
	var eventType uint32
	var srcMAC, dstMAC [6]byte
	var srcIP, dstIP uint32
	var srcPort, dstPort uint16
	var protocol uint8
	var failureCode uint32
	var duration uint64
	var cgroupID uint64
	var netnsID uint32
	var comm [16]byte

	binary.Read(reader, binary.LittleEndian, &timestamp)
	binary.Read(reader, binary.LittleEndian, &eventType)
	reader.Read(srcMAC[:])
	reader.Read(dstMAC[:])
	binary.Read(reader, binary.LittleEndian, &srcIP)
	binary.Read(reader, binary.LittleEndian, &dstIP)
	binary.Read(reader, binary.LittleEndian, &srcPort)
	binary.Read(reader, binary.LittleEndian, &dstPort)
	binary.Read(reader, binary.LittleEndian, &protocol)
	binary.Read(reader, binary.LittleEndian, &failureCode)
	binary.Read(reader, binary.LittleEndian, &duration)
	binary.Read(reader, binary.LittleEndian, &cgroupID)
	binary.Read(reader, binary.LittleEndian, &netnsID)
	reader.Read(comm[:])

	// Convert to NetworkEvent
	event := &NetworkEvent{
		Timestamp:   time.Now(), // Convert kernel timestamp if needed
		EventType:   eventType,
		SrcMAC:      srcMAC,
		DstMAC:      dstMAC,
		SrcIP:       intToIP(srcIP),
		DstIP:       intToIP(dstIP),
		SrcPort:     srcPort,
		DstPort:     dstPort,
		Protocol:    protocol,
		FailureCode: failureCode,
		Duration:    time.Duration(duration),
		CgroupID:    cgroupID,
		NetnsID:     netnsID,
		Comm:        string(bytes.TrimRight(comm[:], "\x00")),
	}

	// Process based on event type
	c.handleEvent(event)

	return nil
}

// Handle different event types
func (c *Collector) handleEvent(event *NetworkEvent) {
	switch event.EventType {
	case EventTCPSYNTimeout:
		// This comes from timeout checker, not kernel

	case EventTCPReset:
		// Connection refused - send directly to correlator
		c.correlator.tcpEvents <- event

	case EventOrphanACK:
		// ACK without connection - interesting!
		c.correlator.tcpEvents <- event

	case EventDupSYN:
		// Duplicate SYN - retry storm
		c.correlator.tcpEvents <- event

	case EventARPTimeout:
		// ARP failure
		c.correlator.arpEvents <- event

	default:
		// Track SYN for timeout detection
		if event.EventType == 0 { // Raw SYN packet
			hash := ConnHash(event.SrcIP, event.DstIP, event.SrcPort, event.DstPort)
			c.pendingSYNs[hash] = &SYNAttempt{
				Timestamp: event.Timestamp,
				SrcIP:     event.SrcIP,
				DstIP:     event.DstIP,
				SrcPort:   event.SrcPort,
				DstPort:   event.DstPort,
				SrcMAC:    event.SrcMAC,
				DstMAC:    event.DstMAC,
				CgroupID:  event.CgroupID,
			}
		}
	}
}

// Helper to convert uint32 to IP
func intToIP(ip uint32) net.IP {
	result := make(net.IP, 4)
	binary.BigEndian.PutUint32(result, ip)
	return result
}
