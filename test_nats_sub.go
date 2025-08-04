package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/yairfalse/tapio/pkg/domain"
)

func main() {
	// Connect to NATS
	nc, err := nats.Connect("nats://localhost:4222")
	if err != nil {
		log.Fatal(err)
	}
	defer nc.Close()

	// Get JetStream context
	js, err := nc.JetStream()
	if err != nil {
		log.Fatal(err)
	}

	// Subscribe to traces
	sub, err := js.PullSubscribe("traces.>", "test-consumer",
		nats.BindStream("TRACES"),
		nats.ManualAck(),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Waiting for events from NATS...")

	// Fetch messages
	for i := 0; i < 5; i++ {
		msgs, err := sub.Fetch(1, nats.MaxWait(5*time.Second))
		if err != nil {
			if err == nats.ErrTimeout {
				fmt.Println("No more messages")
				break
			}
			log.Printf("Error fetching: %v", err)
			continue
		}

		for _, msg := range msgs {
			var event domain.UnifiedEvent
			if err := json.Unmarshal(msg.Data, &event); err != nil {
				log.Printf("Failed to unmarshal: %v", err)
				msg.Ack()
				continue
			}

			fmt.Printf("\n=== Event %d ===\n", i+1)
			fmt.Printf("Subject: %s\n", msg.Subject)
			fmt.Printf("ID: %s\n", event.ID)
			fmt.Printf("Type: %s\n", event.Type)
			fmt.Printf("Source: %s\n", event.Source)
			fmt.Printf("TraceID: %s\n", event.TraceContext.TraceID)
			if event.K8sContext != nil {
				fmt.Printf("K8s: %s/%s (%s)\n", event.K8sContext.Namespace, event.K8sContext.Name, event.K8sContext.Kind)
			}

			msg.Ack()
		}
	}
}
