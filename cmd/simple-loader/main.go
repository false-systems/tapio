package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// Simple, boring loader that works
type SimpleLoader struct {
	logger   *zap.Logger
	nc       *nats.Conn
	js       nats.JetStreamContext
	neo      neo4j.DriverWithContext
	parser   *domain.EventParser
	subCount int
}

func main() {
	// Logger
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// Connect to NATS
	nc, err := nats.Connect("nats://localhost:4222")
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()
	logger.Info("Connected to NATS")

	// Get JetStream
	js, err := nc.JetStream()
	if err != nil {
		log.Fatalf("Failed to get JetStream: %v", err)
	}

	// Connect to Neo4j
	driver, err := neo4j.NewDriverWithContext(
		"bolt://localhost:7687",
		neo4j.BasicAuth("neo4j", "password", ""),
	)
	if err != nil {
		log.Fatalf("Failed to connect to Neo4j: %v", err)
	}
	defer driver.Close(context.Background())
	logger.Info("Connected to Neo4j")

	// Create parser
	parser, err := domain.NewEventParser(logger)
	if err != nil {
		log.Fatalf("Failed to create parser: %v", err)
	}

	// Create loader
	loader := &SimpleLoader{
		logger: logger,
		nc:     nc,
		js:     js,
		neo:    driver,
		parser: parser,
	}

	// Setup context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("Shutting down...")
		cancel()
	}()

	// Start processing
	loader.Start(ctx)
}

func (l *SimpleLoader) Start(ctx context.Context) {
	// Subscribe to observation subjects
	subjects := []string{
		"observations.kernel",
		"observations.kubeapi",
		"observations.dns",
		"observations.etcd",
	}

	for _, subject := range subjects {
		sub, err := l.js.PullSubscribe(
			subject,
			"loader",
			nats.ManualAck(),
			nats.AckExplicit(),
		)
		if err != nil {
			l.logger.Error("Failed to subscribe", zap.String("subject", subject), zap.Error(err))
			continue
		}
		l.subCount++
		l.logger.Info("Subscribed to", zap.String("subject", subject))

		// Start worker for this subscription
		go l.processMessages(ctx, sub, subject)
	}

	l.logger.Info("Loader started", zap.Int("subscriptions", l.subCount))

	// Wait for shutdown
	<-ctx.Done()
	l.logger.Info("Loader stopped")
}

func (l *SimpleLoader) processMessages(ctx context.Context, sub *nats.Subscription, subject string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Fetch messages
		msgs, err := sub.Fetch(10, nats.MaxWait(time.Second))
		if err != nil {
			if err != nats.ErrTimeout {
				l.logger.Error("Failed to fetch messages", zap.Error(err))
			}
			continue
		}

		for _, msg := range msgs {
			if err := l.processMessage(ctx, msg); err != nil {
				l.logger.Error("Failed to process message",
					zap.String("subject", subject),
					zap.Error(err))
				// NAK the message for retry
				msg.Nak()
			} else {
				// ACK the message
				msg.Ack()
			}
		}
	}
}

func (l *SimpleLoader) processMessage(ctx context.Context, msg *nats.Msg) error {
	// Parse RawEvent from message
	var rawEvent collectors.RawEvent
	if err := json.Unmarshal(msg.Data, &rawEvent); err != nil {
		return fmt.Errorf("failed to unmarshal raw event: %w", err)
	}

	// Parse to ObservationEvent
	obsEvent, err := l.parser.ParseEvent(ctx, rawEvent)
	if err != nil {
		return fmt.Errorf("failed to parse event: %w", err)
	}

	// Store in Neo4j
	if err := l.storeObservation(ctx, obsEvent); err != nil {
		return fmt.Errorf("failed to store observation: %w", err)
	}

	l.logger.Debug("Processed observation",
		zap.String("id", obsEvent.ID),
		zap.String("source", obsEvent.Source),
		zap.String("type", obsEvent.Type))

	return nil
}

func (l *SimpleLoader) storeObservation(ctx context.Context, obs *domain.ObservationEvent) error {
	session := l.neo.NewSession(ctx, neo4j.SessionConfig{
		AccessMode: neo4j.AccessModeWrite,
	})
	defer session.Close(ctx)

	// Create observation node
	query := `
		CREATE (o:Observation {
			id: $id,
			timestamp: datetime($timestamp),
			source: $source,
			type: $type,
			action: $action,
			pid: $pid,
			container_id: $container_id,
			pod_name: $pod_name,
			namespace: $namespace,
			service_name: $service_name,
			node_name: $node_name,
			target: $target,
			result: $result
		})
		RETURN o.id as id
	`

	params := map[string]interface{}{
		"id":           obs.ID,
		"timestamp":    obs.Timestamp.Format(time.RFC3339),
		"source":       obs.Source,
		"type":         obs.Type,
		"action":       obs.Action,
		"pid":          obs.PID,
		"container_id": obs.ContainerID,
		"pod_name":     obs.PodName,
		"namespace":    obs.Namespace,
		"service_name": obs.ServiceName,
		"node_name":    obs.NodeName,
		"target":       obs.Target,
		"result":       obs.Result,
	}

	_, err := session.Run(ctx, query, params)
	if err != nil {
		return fmt.Errorf("failed to create observation node: %w", err)
	}

	// If it's a pod event, create/update Pod node
	if obs.PodName != nil && obs.Source == "kubeapi" {
		podQuery := `
			MERGE (p:Pod {name: $name, namespace: $namespace})
			WITH p
			MATCH (o:Observation {id: $obs_id})
			MERGE (o)-[:BELONGS_TO]->(p)
		`
		_, err = session.Run(ctx, podQuery, map[string]interface{}{
			"name":      *obs.PodName,
			"namespace": getStringValue(obs.Namespace, "default"),
			"obs_id":    obs.ID,
		})
		if err != nil {
			l.logger.Warn("Failed to create pod relationship", zap.Error(err))
		}
	}

	return nil
}

func getStringValue(ptr *string, defaultVal string) string {
	if ptr != nil {
		return *ptr
	}
	return defaultVal
}
