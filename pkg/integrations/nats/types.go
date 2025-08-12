package nats

import "time"

// SubscriberMetrics contains strongly-typed metrics from NATS subscriber
type SubscriberMetrics struct {
	MessagesReceived int64     `json:"messages_received"`
	MessagesAcked    int64     `json:"messages_acked"`
	MessagesNacked   int64     `json:"messages_nacked"`
	ProcessingErrors int64     `json:"processing_errors"`
	PendingMessages  int       `json:"pending_messages"`
	Connected        bool      `json:"connected"`
	LastActivity     time.Time `json:"last_activity"`
	ConsumerInfo     string    `json:"consumer_info"`
}
