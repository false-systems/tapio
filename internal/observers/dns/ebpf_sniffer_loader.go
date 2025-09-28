//go:build linux
// +build linux

package dns

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/vishvananda/netlink"
	"go.uber.org/zap"
)

// DNSSnifferProgram manages the packet sniffer
type DNSSnifferProgram struct {
	objs    *dnsSnifferObjects
	reader  *ringbuf.Reader
	logger  *zap.Logger
	iface   string
	tracker *DNSTracker
}

// NewDNSSnifferProgram creates a new DNS sniffer
func NewDNSSnifferProgram(logger *zap.Logger, iface string) *DNSSnifferProgram {
	if iface == "" {
		iface = "eth0"
	}
	return &DNSSnifferProgram{
		logger:  logger,
		iface:   iface,
		tracker: NewDNSTracker(),
	}
}

// Load loads the sniffer eBPF program
func (p *DNSSnifferProgram) Load() error {
	// Remove memory limit
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("removing memlock: %w", err)
	}

	// Load eBPF objects
	objs := &dnsSnifferObjects{}
	if err := loadDnsSnifferObjects(objs, nil); err != nil {
		return fmt.Errorf("loading sniffer objects: %w", err)
	}

	p.objs = objs

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(p.objs.DnsPackets)
	if err != nil {
		p.objs.Close()
		return fmt.Errorf("creating ringbuf reader: %w", err)
	}
	p.reader = reader

	p.logger.Info("DNS sniffer loaded")
	return nil
}

// Attach attaches to network interface
func (p *DNSSnifferProgram) Attach() error {
	// Get netlink interface
	nl, err := netlink.LinkByName(p.iface)
	if err != nil {
		return fmt.Errorf("getting interface %s: %w", p.iface, err)
	}

	// Create clsact qdisc
	qdisc := &netlink.GenericQdisc{
		QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: nl.Attrs().Index,
			Handle:    netlink.MakeHandle(0xffff, 0),
			Parent:    netlink.HANDLE_CLSACT,
		},
		QdiscType: "clsact",
	}

	_ = netlink.QdiscAdd(qdisc)

	// Attach to TC ingress
	filter := &netlink.BpfFilter{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: nl.Attrs().Index,
			Parent:    netlink.HANDLE_MIN_INGRESS,
			Protocol:  0x0003, // ETH_P_ALL
		},
		Fd:           p.objs.TcDnsSniffer.FD(),
		Name:         "dns_sniffer",
		DirectAction: true,
	}

	if err := netlink.FilterAdd(filter); err != nil {
		return fmt.Errorf("adding TC filter: %w", err)
	}

	p.logger.Info("DNS sniffer attached", zap.String("interface", p.iface))
	return nil
}

// DNSPacketEvent is the raw event from eBPF
type DNSPacketEvent struct {
	TimestampNs uint64
	Saddr       uint32
	Daddr       uint32
	Sport       uint16
	Dport       uint16
	DNSDataLen  uint16
	DNSData     [512]byte
}

// DNSProblem represents a detected DNS problem
type DNSProblem struct {
	Timestamp time.Time
	Name      string
	SourceIP  net.IP
	DestIP    net.IP
	Latency   time.Duration
	RCode     uint8
	IsSlow    bool
	Problem   string
}

// ReadProblems reads and processes DNS packets, returning only problems
func (p *DNSSnifferProgram) ReadProblems() (<-chan *DNSProblem, error) {
	problems := make(chan *DNSProblem, 100)

	go func() {
		defer close(problems)

		// Cleanup old queries periodically
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				p.tracker.CleanupOld(30 * time.Second)
			default:
				record, err := p.reader.Read()
				if err != nil {
					if err == ringbuf.ErrClosed {
						return
					}
					// Check if it's because we're closing
					if err.Error() == "epoll wait: file already closed" {
						return
					}
					p.logger.Error("Reading ringbuf", zap.Error(err))
					continue
				}

				if len(record.RawSample) < int(unsafe.Sizeof(DNSPacketEvent{})) {
					continue
				}

				// Parse event
				event := (*DNSPacketEvent)(unsafe.Pointer(&record.RawSample[0]))

				// Parse DNS packet
				dnsData := event.DNSData[:event.DNSDataLen]
				header, name, err := ParseDNSPacketData(dnsData)
				if err != nil {
					continue
				}

				// Convert network byte order
				saddr := binary.BigEndian.Uint32((*[4]byte)(unsafe.Pointer(&event.Saddr))[:])
				daddr := binary.BigEndian.Uint32((*[4]byte)(unsafe.Pointer(&event.Daddr))[:])
				sport := binary.BigEndian.Uint16((*[2]byte)(unsafe.Pointer(&event.Sport))[:])
				dport := binary.BigEndian.Uint16((*[2]byte)(unsafe.Pointer(&event.Dport))[:])

				if !header.IsResponse() {
					// Track query
					p.tracker.TrackQuery(saddr, daddr, sport, header, name)
					p.logger.Debug("DNS Query captured",
						zap.String("name", name),
						zap.Uint16("id", header.ID))
				} else {
					// Match response
					query, latency := p.tracker.MatchResponse(saddr, daddr, dport, header)
					if query == nil {
						p.logger.Debug("DNS Response without matching query",
							zap.Uint16("id", header.ID))
						continue
					}

					p.logger.Debug("DNS Response matched",
						zap.String("name", query.Name),
						zap.Uint16("id", header.ID),
						zap.Duration("latency", latency),
						zap.Uint8("rcode", header.ResponseCode()))

					// Check for problems
					rcode := header.ResponseCode()
					isSlow := latency > 100*time.Millisecond

					if isSlow || rcode != 0 {
						problem := &DNSProblem{
							Timestamp: time.Now(),
							Name:      query.Name,
							SourceIP:  query.SrcIP,
							DestIP:    query.DstIP,
							Latency:   latency,
							RCode:     rcode,
							IsSlow:    isSlow,
						}

						// Set problem description
						switch rcode {
						case 0:
							if isSlow {
								problem.Problem = fmt.Sprintf("Slow query: %dms", latency.Milliseconds())
							}
						case 2:
							problem.Problem = "SERVFAIL"
						case 3:
							problem.Problem = "NXDOMAIN"
						case 5:
							problem.Problem = "REFUSED"
						default:
							problem.Problem = fmt.Sprintf("Error code %d", rcode)
						}

						problems <- problem
					}
				}
			}
		}
	}()

	return problems, nil
}

// Close cleans up
func (p *DNSSnifferProgram) Close() {
	if p.reader != nil {
		p.reader.Close()
	}

	if p.objs != nil {
		p.objs.Close()
	}

	p.logger.Info("DNS sniffer closed")
}
