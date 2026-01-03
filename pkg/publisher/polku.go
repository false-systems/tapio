// Package publisher provides the PolkuPublisher for sending events to POLKU gateway.
package publisher

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	polkupb "github.com/yairfalse/proto/gen/go/polku/v1"
	tapiopb "github.com/yairfalse/proto/gen/go/tapio/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

var (
	flushErrors = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "tapio",
		Subsystem: "publisher",
		Name:      "flush_errors_total",
		Help:      "Total number of flush errors",
	})

	eventsSent = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "tapio",
		Subsystem: "publisher",
		Name:      "events_sent_total",
		Help:      "Total number of events sent to POLKU",
	})
)

// Config for the POLKU publisher.
type Config struct {
	// Required
	Address   string // POLKU gRPC address (e.g., "polku:50051")
	ClusterID string // Cluster identifier
	NodeName  string // Node name for source identification

	// Optional (defaults applied)
	BatchSize     int           // Events per batch (default: 100)
	FlushInterval time.Duration // Max time between flushes (default: 100ms)
	BufferSize    int           // Event buffer size (default: 1000)

	// Reconnection (optional)
	ReconnectInitial time.Duration // Initial backoff (default: 1s)
	ReconnectMax     time.Duration // Max backoff (default: 30s)

	// TLS (optional)
	TLSCert string // Path to client cert
	TLSKey  string // Path to client key
	TLSCA   string // Path to CA cert
}

// Validate checks required fields.
func (c *Config) Validate() error {
	if c.Address == "" {
		return errors.New("address is required")
	}
	if c.ClusterID == "" {
		return errors.New("cluster_id is required")
	}
	return nil
}

// ApplyDefaults sets default values for optional fields.
func (c *Config) ApplyDefaults() {
	if c.BatchSize == 0 {
		c.BatchSize = 100
	}
	if c.FlushInterval == 0 {
		c.FlushInterval = 100 * time.Millisecond
	}
	if c.BufferSize == 0 {
		c.BufferSize = 1000
	}
	if c.ReconnectInitial == 0 {
		c.ReconnectInitial = 1 * time.Second
	}
	if c.ReconnectMax == 0 {
		c.ReconnectMax = 30 * time.Second
	}
}

// Publisher sends events to POLKU gateway.
type Publisher struct {
	config    Config
	clusterID string
	nodeName  string

	conn     *grpc.ClientConn
	client   polkupb.GatewayClient
	stream   polkupb.Gateway_StreamEventsClient
	streamMu sync.RWMutex

	buffer     []*tapiopb.RawEbpfEvent
	bufferMu   sync.Mutex
	bufferSize int

	// Connection state
	connected   atomic.Bool
	reconnectCh chan struct{}

	// Backpressure
	throttle atomic.Int32 // 0-100, percentage of normal rate

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a new POLKU publisher.
func New(cfg Config) *Publisher {
	cfg.ApplyDefaults()

	ctx, cancel := context.WithCancel(context.Background())

	return &Publisher{
		config:      cfg,
		clusterID:   cfg.ClusterID,
		nodeName:    cfg.NodeName,
		buffer:      make([]*tapiopb.RawEbpfEvent, 0, cfg.BatchSize),
		bufferSize:  cfg.BufferSize,
		reconnectCh: make(chan struct{}, 1),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Connect establishes gRPC connection to POLKU.
func (p *Publisher) Connect(ctx context.Context) error {
	creds, err := p.buildTransportCredentials()
	if err != nil {
		return fmt.Errorf("build credentials: %w", err)
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
	}

	conn, err := grpc.NewClient(p.config.Address, opts...)
	if err != nil {
		return err
	}

	p.conn = conn
	p.client = polkupb.NewGatewayClient(conn)

	stream, err := p.client.StreamEvents(ctx)
	if err != nil {
		return errors.Join(err, conn.Close())
	}

	p.streamMu.Lock()
	p.stream = stream
	p.streamMu.Unlock()
	p.connected.Store(true)

	p.wg.Add(3)
	go p.flushLoop()
	go p.ackLoop()
	go p.reconnectLoop()

	return nil
}

// buildTransportCredentials creates TLS or insecure credentials based on config.
func (p *Publisher) buildTransportCredentials() (credentials.TransportCredentials, error) {
	if p.config.TLSCert == "" {
		return insecure.NewCredentials(), nil
	}

	cert, err := tls.LoadX509KeyPair(p.config.TLSCert, p.config.TLSKey)
	if err != nil {
		return nil, fmt.Errorf("load client cert: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	if p.config.TLSCA != "" {
		caCert, err := os.ReadFile(p.config.TLSCA)
		if err != nil {
			return nil, fmt.Errorf("read CA cert: %w", err)
		}
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			return nil, errors.New("failed to parse CA certificate")
		}
		tlsConfig.RootCAs = caPool
	}

	return credentials.NewTLS(tlsConfig), nil
}

// Publish queues an event for sending.
func (p *Publisher) Publish(event *tapiopb.RawEbpfEvent) error {
	p.bufferMu.Lock()
	defer p.bufferMu.Unlock()

	if len(p.buffer) >= p.bufferSize {
		return errors.New("buffer full, dropping event")
	}

	p.buffer = append(p.buffer, event)

	if len(p.buffer) >= p.config.BatchSize {
		return p.flushLocked()
	}

	return nil
}

// flushLoop periodically flushes the buffer.
func (p *Publisher) flushLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.bufferMu.Lock()
			if len(p.buffer) > 0 {
				if err := p.flushLocked(); err != nil {
					flushErrors.Inc()
				}
			}
			p.bufferMu.Unlock()
		}
	}
}

// flushLocked sends buffered events. Caller must hold bufferMu.
func (p *Publisher) flushLocked() error {
	if len(p.buffer) == 0 || !p.connected.Load() {
		return nil
	}

	eventCount := len(p.buffer)

	batch := &tapiopb.EventBatch{
		Events:    p.buffer,
		Source:    "tapio",
		ClusterId: p.clusterID,
		NodeName:  p.nodeName,
	}

	data, err := proto.Marshal(batch)
	if err != nil {
		return err
	}

	p.streamMu.RLock()
	stream := p.stream
	p.streamMu.RUnlock()

	if stream == nil {
		return errors.New("stream not available")
	}

	err = stream.Send(&polkupb.IngestBatch{
		Source:  "tapio",
		Cluster: p.clusterID,
		Payload: &polkupb.IngestBatch_Raw{
			Raw: &polkupb.RawPayload{
				Data:   data,
				Format: "protobuf",
			},
		},
	})

	if err == nil {
		p.buffer = p.buffer[:0]
		eventsSent.Add(float64(eventCount))
	}

	return err
}

// ackLoop handles acknowledgments and backpressure.
// Runs for the lifetime of the publisher, survives reconnections.
func (p *Publisher) ackLoop() {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		p.streamMu.RLock()
		stream := p.stream
		p.streamMu.RUnlock()

		if stream == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		ack, err := stream.Recv()
		if err != nil {
			p.signalReconnect()
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if ack.BufferCapacity > 0 {
			fillPct := float64(ack.BufferSize) / float64(ack.BufferCapacity) * 100
			if fillPct > 80 {
				p.throttle.Store(50)
			} else if fillPct > 50 {
				p.throttle.Store(75)
			} else {
				p.throttle.Store(100)
			}
		}
	}
}

// signalReconnect triggers a reconnection attempt.
func (p *Publisher) signalReconnect() {
	p.connected.Store(false)
	select {
	case p.reconnectCh <- struct{}{}:
	default:
	}
}

// reconnectLoop handles reconnection with exponential backoff.
func (p *Publisher) reconnectLoop() {
	defer p.wg.Done()

	backoff := p.config.ReconnectInitial

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-p.reconnectCh:
			for {
				select {
				case <-p.ctx.Done():
					return
				default:
				}

				if err := p.tryReconnect(); err == nil {
					backoff = p.config.ReconnectInitial
					break
				}

				time.Sleep(backoff)
				backoff = min(backoff*2, p.config.ReconnectMax)
			}
		}
	}
}

// tryReconnect attempts to re-establish the stream.
func (p *Publisher) tryReconnect() error {
	stream, err := p.client.StreamEvents(p.ctx)
	if err != nil {
		return err
	}

	p.streamMu.Lock()
	p.stream = stream
	p.streamMu.Unlock()
	p.connected.Store(true)

	return nil
}

// Close shuts down the publisher.
func (p *Publisher) Close() error {
	p.cancel()
	p.connected.Store(false)

	var errs []error

	p.streamMu.Lock()
	if p.stream != nil {
		if err := p.stream.CloseSend(); err != nil {
			errs = append(errs, err)
		}
		p.stream = nil
	}
	p.streamMu.Unlock()

	p.wg.Wait()

	if p.conn != nil {
		if err := p.conn.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Throttle returns the current throttle percentage (0-100).
func (p *Publisher) Throttle() int {
	return int(p.throttle.Load())
}

// IsConnected returns true if the publisher is connected to POLKU.
func (p *Publisher) IsConnected() bool {
	return p.connected.Load()
}
