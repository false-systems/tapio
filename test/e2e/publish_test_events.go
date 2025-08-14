package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/yairfalse/tapio/pkg/collectors"
)

func main() {
	// Connect to NATS
	nc, err := nats.Connect("nats://localhost:4222")
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	// Get JetStream context
	js, err := nc.JetStream()
	if err != nil {
		log.Fatalf("Failed to get JetStream: %v", err)
	}

	// Ensure stream exists
	_, err = js.AddStream(&nats.StreamConfig{
		Name:     "OBSERVATIONS",
		Subjects: []string{"observations.>"},
		MaxAge:   24 * time.Hour,
	})
	if err != nil {
		// Stream might already exist, that's ok
		fmt.Printf("Stream might already exist: %v\n", err)
	}

	// Create test events with proper correlation keys
	events := []struct {
		event collectors.RawEvent
		desc  string
	}{
		{
			event: collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "kernel",
				Data:      json.RawMessage(`{"PID": 1234, "TID": 1234, "Comm": "nginx", "Syscall": "open", "EventType": 1, "ContainerID": "docker://abc123def456", "PodUID": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"}`),
				Metadata: map[string]string{
					"node":      "worker-1",
					"collector": "kernel",
				},
			},
			desc: "Kernel syscall event",
		},
		{
			event: collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "kubeapi",
				Data: json.RawMessage(`{
					"EventType": "ADDED",
					"Kind": "Pod",
					"Name": "nginx-deployment-5d59d67564-8g7nm",
					"Namespace": "default",
					"Labels": {"app": "nginx", "version": "v1"},
					"OwnerReferences": [{"Kind": "ReplicaSet", "Name": "nginx-deployment-5d59d67564"}],
					"UID": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
				}`),
				Metadata: map[string]string{
					"cluster":   "production",
					"collector": "kubeapi",
				},
			},
			desc: "Kubernetes Pod creation",
		},
		{
			event: collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "dns",
				Data: json.RawMessage(`{
					"QueryName": "nginx-service.default.svc.cluster.local",
					"QueryType": "A",
					"ClientIP": "10.244.1.5",
					"ServerIP": "10.96.0.10",
					"ResponseCode": 0,
					"Latency": 5,
					"PodName": "coredns-5dd5756b68-xyzab",
					"Namespace": "kube-system"
				}`),
				Metadata: map[string]string{
					"node":      "worker-1",
					"collector": "dns",
				},
			},
			desc: "DNS query for service",
		},
		{
			event: collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "kernel",
				Data:      json.RawMessage(`{"PID": 5678, "TID": 5678, "Comm": "kubelet", "Syscall": "connect", "EventType": 2, "ContainerID": "containerd://xyz789ghi012"}`),
				Metadata: map[string]string{
					"node":      "worker-1",
					"collector": "kernel",
				},
			},
			desc: "Kernel network event",
		},
		{
			event: collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "etcd",
				Data: json.RawMessage(`{
					"Operation": "PUT",
					"Key": "/registry/services/endpoints/default/nginx-service",
					"Value": "{\"endpoints\":[\"10.244.1.5:80\"]}",
					"ServiceName": "nginx-service",
					"Namespace": "default"
				}`),
				Metadata: map[string]string{
					"node":      "control-plane",
					"collector": "etcd",
				},
			},
			desc: "etcd service endpoint update",
		},
	}

	// Publish events
	fmt.Println("📤 Publishing test events to NATS...")
	for _, e := range events {
		data, err := json.Marshal(e.event)
		if err != nil {
			log.Printf("Failed to marshal event: %v", err)
			continue
		}

		subject := fmt.Sprintf("observations.%s", e.event.Type)
		ack, err := js.Publish(subject, data)
		if err != nil {
			log.Printf("❌ Failed to publish %s: %v", e.desc, err)
		} else {
			fmt.Printf("✅ Published: %s (seq: %d)\n", e.desc, ack.Sequence)
		}

		// Small delay between events
		time.Sleep(100 * time.Millisecond)
	}

	fmt.Printf("\n✨ Published %d test events to NATS\n", len(events))
	fmt.Println("   Stream: OBSERVATIONS")
	fmt.Println("   Subjects: observations.kernel, observations.kubeapi, observations.dns, observations.etcd")
}
