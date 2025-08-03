package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/integrations/transformer"
)

type TransformerService struct {
	nc          *nats.Conn
	js          jetstream.JetStream
	transformer *transformer.EventTransformer
	consumers   map[string]jetstream.Consumer
	mu          sync.RWMutex
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
}

func NewTransformerService() (*TransformerService, error) {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}

	nc, err := nats.Connect(natsURL,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
		nats.Timeout(10*time.Second),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			log.Printf("Disconnected from NATS: %v", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Printf("Reconnected to NATS")
		}),
		nats.ErrorHandler(func(nc *nats.Conn, sub *nats.Subscription, err error) {
			log.Printf("NATS error: %v", err)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	transformer := transformer.NewEventTransformer()
	ctx, cancel := context.WithCancel(context.Background())

	return &TransformerService{
		nc:          nc,
		js:          js,
		transformer: transformer,
		consumers:   make(map[string]jetstream.Consumer),
		ctx:         ctx,
		cancel:      cancel,
	}, nil
}

func (s *TransformerService) Start() error {
	log.Println("Starting Transformer Service...")

	stream, err := s.js.Stream(s.ctx, "RAW_EVENTS")
	if err != nil {
		return fmt.Errorf("failed to get RAW_EVENTS stream: %w", err)
	}

	consumer, err := stream.CreateOrUpdateConsumer(s.ctx, jetstream.ConsumerConfig{
		Name:          "transformer-consumer",
		Durable:       "transformer-consumer",
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: "raw.>",
		MaxDeliver:    3,
		AckWait:       30 * time.Second,
		MaxAckPending: 1000,
	})
	if err != nil {
		return fmt.Errorf("failed to create consumer: %w", err)
	}

	s.mu.Lock()
	s.consumers["transformer-consumer"] = consumer
	s.mu.Unlock()

	s.wg.Add(1)
	go s.consumeMessages(consumer)

	log.Println("Transformer Service started successfully")
	return nil
}

func (s *TransformerService) consumeMessages(consumer jetstream.Consumer) {
	defer s.wg.Done()

	cctx, err := consumer.Consume(func(msg jetstream.Msg) {
		if err := s.processMessage(msg); err != nil {
			log.Printf("Error processing message: %v", err)
			msg.Nak()
			return
		}
		msg.Ack()
	}, jetstream.ConsumeErrHandler(func(consumeCtx jetstream.ConsumeContext, err error) {
		log.Printf("Consume error: %v", err)
	}))

	if err != nil {
		log.Printf("Failed to start consuming: %v", err)
		return
	}

	<-s.ctx.Done()
	cctx.Stop()
}

func (s *TransformerService) processMessage(msg jetstream.Msg) error {
	metadata, err := msg.Metadata()
	if err != nil {
		return fmt.Errorf("failed to get message metadata: %w", err)
	}

	log.Printf("Processing message from subject: %s, stream: %s, consumer: %s",
		msg.Subject(), metadata.Stream, metadata.Consumer)

	var rawEvent collectors.RawEvent
	if err := json.Unmarshal(msg.Data(), &rawEvent); err != nil {
		return fmt.Errorf("failed to unmarshal raw event: %w", err)
	}

	ctx := context.Background()
	unifiedEvent, err := s.transformer.Transform(ctx, rawEvent)
	if err != nil {
		return fmt.Errorf("failed to transform event: %w", err)
	}

	data, err := json.Marshal(unifiedEvent)
	if err != nil {
		return fmt.Errorf("failed to marshal unified event: %w", err)
	}

	var entityType, namespace, name string
	if unifiedEvent.Entity != nil {
		entityType = unifiedEvent.Entity.Type
		namespace = unifiedEvent.Entity.Namespace
		name = unifiedEvent.Entity.Name
	}
	if entityType == "" {
		entityType = "unknown"
	}
	if namespace == "" {
		namespace = "default"
	}
	if name == "" {
		name = "unnamed"
	}

	subject := fmt.Sprintf("unified.%s.%s.%s", entityType, namespace, name)

	if err := s.nc.Publish(subject, data); err != nil {
		return fmt.Errorf("failed to publish unified event: %w", err)
	}

	log.Printf("Published unified event to subject: %s", subject)
	return nil
}

func (s *TransformerService) Stop() {
	log.Println("Stopping Transformer Service...")

	s.cancel()

	s.wg.Wait()

	s.mu.Lock()
	for name := range s.consumers {
		log.Printf("Stopping consumer: %s", name)
		// Consumer stops automatically when context is cancelled
	}
	s.mu.Unlock()

	s.nc.Close()
	log.Println("Transformer Service stopped")
}

func main() {
	service, err := NewTransformerService()
	if err != nil {
		log.Fatalf("Failed to create transformer service: %v", err)
	}

	if err := service.Start(); err != nil {
		log.Fatalf("Failed to start transformer service: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	log.Println("Received shutdown signal")

	service.Stop()
}
