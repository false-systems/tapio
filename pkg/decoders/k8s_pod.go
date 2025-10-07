package decoders

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
)

// K8sPod decoder transforms IP address to pod name via NATS KV lookup
type K8sPod struct {
	kv nats.KeyValue
}

// PodInfo matches Context Service output (from CONTEXT_SERVICE.md)
type PodInfo struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	PodIP     string            `json:"pod_ip"`
	HostIP    string            `json:"host_ip"`
	Labels    map[string]string `json:"labels"`
}

// NewK8sPod creates a K8s pod decoder with NATS KV
func NewK8sPod(kv nats.KeyValue) *K8sPod {
	return &K8sPod{kv: kv}
}

// Decode transforms IP address (bytes) to pod name
// Input: ctx (context for cancellation), IP address bytes (e.g., from inet_ip decoder output: "10.244.1.5")
// Output: Pod name (e.g., "nginx-abc123")
func (k *K8sPod) Decode(ctx context.Context, in []byte, conf Decoder) ([]byte, error) {
	ip := string(in)

	// Lookup in NATS KV: pod.ip.<ip> → PodInfo
	key := fmt.Sprintf("pod.ip.%s", ip)
	entry, err := k.kv.Get(ctx, key)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			// IP not found in K8s metadata
			// If AllowUnknown, return original IP
			if conf.AllowUnknown {
				return in, nil
			}
			return nil, fmt.Errorf("pod not found for IP %s", ip)
		}
		return nil, fmt.Errorf("NATS KV lookup failed for %s: %w", key, err)
	}

	// Parse PodInfo JSON
	var podInfo PodInfo
	if err := json.Unmarshal(entry.Value(), &podInfo); err != nil {
		return nil, fmt.Errorf("failed to parse PodInfo for %s: %w", ip, err)
	}

	// Return pod name
	return []byte(podInfo.Name), nil
}
