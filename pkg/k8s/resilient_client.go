package k8s

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/client-go/util/retry"
)

type ResilientClient struct {
	*Client
	rateLimiter    *rate.Limiter
	requestTracker *RequestTracker
	mu             sync.RWMutex
	closed         bool
}

type RequestTracker struct {
	mu          sync.Mutex
	inFlight    map[string]*InFlightRequest
	dedupWindow time.Duration
}

type InFlightRequest struct {
	mu         sync.Mutex
	key        string
	result     interface{}
	err        error
	done       chan struct{}
	expiry     time.Time
	references int
}

type ResilientConfig struct {
	RateLimit       int
	RateBurst       int
	MaxRetries      int
	BackoffDuration time.Duration
	BackoffFactor   float64
	BackoffJitter   float64
	DedupWindow     time.Duration
	Timeout         time.Duration
}

func DefaultResilientConfig() *ResilientConfig {
	return &ResilientConfig{
		RateLimit:       50,
		RateBurst:       100,
		MaxRetries:      5,
		BackoffDuration: 100 * time.Millisecond,
		BackoffFactor:   2.0,
		BackoffJitter:   0.1,
		DedupWindow:     1 * time.Second,
		Timeout:         30 * time.Second,
	}
}

func NewResilientClient(kubeconfigPath string, config *ResilientConfig) (*ResilientClient, error) {
	if config == nil {
		config = DefaultResilientConfig()
	}

	baseClient, err := NewClient(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create base client: %w", err)
	}

	baseClient.Config.RateLimiter = flowcontrol.NewTokenBucketRateLimiter(
		float32(config.RateLimit),
		config.RateBurst,
	)
	// Configure connection pooling and timeouts
	baseClient.Config.Timeout = config.Timeout
	baseClient.Config.QPS = float32(config.RateLimit)
	baseClient.Config.Burst = config.RateBurst

	clientset, err := kubernetes.NewForConfig(baseClient.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client with resilient config: %w", err)
	}
	baseClient.Clientset = clientset

	rc := &ResilientClient{
		Client:      baseClient,
		rateLimiter: rate.NewLimiter(rate.Limit(config.RateLimit), config.RateBurst),
		requestTracker: &RequestTracker{
			inFlight:    make(map[string]*InFlightRequest),
			dedupWindow: config.DedupWindow,
		},
	}

	go rc.requestTracker.cleanupLoop()

	return rc, nil
}

func (rc *ResilientClient) WithRetry(ctx context.Context, fn func() error) error {
	rc.mu.RLock()
	if rc.closed {
		rc.mu.RUnlock()
		return fmt.Errorf("client is closed")
	}
	rc.mu.RUnlock()

	if err := rc.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit exceeded: %w", err)
	}

	backoff := wait.Backoff{
		Duration: 100 * time.Millisecond,
		Factor:   2.0,
		Jitter:   0.1,
		Steps:    5,
		Cap:      30 * time.Second,
	}

	return retry.OnError(backoff, isRetryableError, fn)
}

func (rc *ResilientClient) ExecuteWithDedup(ctx context.Context, key string, fn func() (interface{}, error)) (interface{}, error) {
	rc.mu.RLock()
	if rc.closed {
		rc.mu.RUnlock()
		return nil, fmt.Errorf("client is closed")
	}
	rc.mu.RUnlock()

	req := rc.requestTracker.getOrCreate(key)
	if req == nil {
		req = &InFlightRequest{
			key:    key,
			done:   make(chan struct{}),
			expiry: time.Now().Add(rc.requestTracker.dedupWindow),
		}
		rc.requestTracker.mu.Lock()
		rc.requestTracker.inFlight[key] = req
		rc.requestTracker.mu.Unlock()

		go func() {
			result, err := fn()
			req.mu.Lock()
			req.result = result
			req.err = err
			close(req.done)
			req.mu.Unlock()
		}()
	}

	select {
	case <-ctx.Done():
		rc.requestTracker.release(key)
		return nil, ctx.Err()
	case <-req.done:
		result, err := req.result, req.err
		rc.requestTracker.release(key)
		return result, err
	}
}

func (rc *ResilientClient) Close() error {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if rc.closed {
		return nil
	}

	rc.closed = true
	return nil
}

func (rt *RequestTracker) getOrCreate(key string) *InFlightRequest {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if req, exists := rt.inFlight[key]; exists && time.Now().Before(req.expiry) {
		req.references++
		return req
	}
	return nil
}

func (rt *RequestTracker) release(key string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if req, exists := rt.inFlight[key]; exists {
		req.references--
		if req.references <= 0 {
			delete(rt.inFlight, key)
		}
	}
}

func (rt *RequestTracker) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		rt.cleanup()
	}
}

func (rt *RequestTracker) cleanup() {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	now := time.Now()
	for key, req := range rt.inFlight {
		if now.After(req.expiry) && req.references == 0 {
			delete(rt.inFlight, key)
		}
	}
}

func isRetryableError(err error) bool {
	if errors.IsTimeout(err) {
		return true
	}
	if errors.IsServerTimeout(err) {
		return true
	}
	if errors.IsServiceUnavailable(err) {
		return true
	}
	if errors.IsTooManyRequests(err) {
		return true
	}
	if errors.IsInternalError(err) {
		return true
	}
	return false
}
