package grpc

import (
	"time"

	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
)

// CorrelationSubscription represents an active correlation subscription
type CorrelationSubscription struct {
	ID         string
	Filter     *pb.Filter
	CreatedAt  time.Time
	LastUpdate time.Time
	EventCount int64
}
