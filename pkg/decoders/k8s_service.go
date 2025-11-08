package decoders

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
)

// K8sService decoder transforms IP address to service name via NATS KV lookup
type K8sService struct {
	kv nats.KeyValue
}

// ServiceInfo matches Context Service output (from CONTEXT_SERVICE.md)
type ServiceInfo struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	ClusterIP string            `json:"cluster_ip"`
	Type      string            `json:"type"`
	Labels    map[string]string `json:"labels"`
}

// NewK8sService creates a K8s service decoder with NATS KV
func NewK8sService(kv nats.KeyValue) *K8sService {
	return &K8sService{kv: kv}
}

// Decode transforms IP address (bytes) to service name
// Input: IP address bytes (e.g., from inet_ip decoder output: "10.96.0.1")
// Output: Service name (e.g., "kubernetes")
func (k *K8sService) Decode(ctx context.Context, in []byte, conf Decoder) ([]byte, error) {
	if k.kv == nil {
		return nil, fmt.Errorf("K8sService decoder: NATS KV not initialized")
	}

	ip := string(in)

	// Lookup in NATS KV: service.ip.<ip> → ServiceInfo
	key := fmt.Sprintf("service.ip.%s", ip)
	entry, err := k.kv.Get(key)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			// IP not found in K8s metadata
			// If AllowUnknown, return original IP
			if conf.AllowUnknown {
				return in, nil
			}
			return nil, fmt.Errorf("service not found for IP %s", ip)
		}
		return nil, fmt.Errorf("NATS KV lookup failed for %s: %w", key, err)
	}

	// Parse ServiceInfo JSON
	var serviceInfo ServiceInfo
	if err := json.Unmarshal(entry.Value(), &serviceInfo); err != nil {
		return nil, fmt.Errorf("failed to parse ServiceInfo for %s: %w", ip, err)
	}

	// Return service name
	return []byte(serviceInfo.Name), nil
}
