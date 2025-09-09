package status

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestStatusObserver(t *testing.T) {
	logger := zap.NewNop()

	config := &Config{
		Enabled:         true,
		SampleRate:      1.0,
		MaxEventsPerSec: 1000,
		MaxMemoryMB:     10,
		FlushInterval:   100 * time.Millisecond,
		RedactHeaders:   []string{"Authorization"},
	}

	observer, err := NewObserver(config, logger)
	require.NoError(t, err)
	assert.NotNil(t, observer)

	t.Run("HashDecoder", func(t *testing.T) {
		decoder := NewHashDecoder()

		decoder.AddService(12345, "api-gateway")
		decoder.AddEndpoint(67890, "/api/users")

		assert.Equal(t, "api-gateway", decoder.GetService(12345))
		assert.Equal(t, "/api/users", decoder.GetEndpoint(67890))
		assert.Equal(t, "", decoder.GetService(99999))
	})

	t.Run("StatusAggregator", func(t *testing.T) {
		aggregator := NewStatusAggregator(100 * time.Millisecond)

		event1 := &StatusEvent{
			ServiceHash:  12345,
			EndpointHash: 67890,
			StatusCode:   500,
			ErrorType:    Error5XX,
			Latency:      1000,
		}

		event2 := &StatusEvent{
			ServiceHash:  12345,
			EndpointHash: 67890,
			StatusCode:   200,
			ErrorType:    ErrorNone,
			Latency:      500,
		}

		aggregator.Add(event1)
		aggregator.Add(event2)

		aggregates := aggregator.Flush()
		assert.Len(t, aggregates, 1)

		agg := aggregates[12345]
		assert.NotNil(t, agg)
		assert.Equal(t, uint64(2), agg.TotalCount)
		assert.Equal(t, uint64(1), agg.ErrorCount)
		assert.Equal(t, 750.0, agg.AvgLatency())
		assert.Equal(t, 0.5, agg.ErrorRate())
	})

	t.Run("PatternDetection", func(t *testing.T) {
		events := make([]*StatusEvent, 0)

		for i := 0; i < 10; i++ {
			events = append(events, &StatusEvent{
				ServiceHash: 12345,
				ErrorType:   ErrorTimeout,
			})
		}

		cascadingPattern := KnownPatterns[0]
		assert.True(t, cascadingPattern.Detector(events))

		events = events[:2]
		assert.False(t, cascadingPattern.Detector(events))
	})

	t.Run("ErrorTypes", func(t *testing.T) {
		assert.Equal(t, ErrorType(0), ErrorNone)
		assert.Equal(t, ErrorType(1), ErrorTimeout)
		assert.Equal(t, ErrorType(2), ErrorRefused)
		assert.Equal(t, ErrorType(3), ErrorReset)
		assert.Equal(t, ErrorType(4), Error5XX)
		assert.Equal(t, ErrorType(5), Error4XX)
	})
}

func TestHTTPFailureScenarios(t *testing.T) {
	t.Run("5XXErrors", func(t *testing.T) {
		var requestCount atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := requestCount.Add(1)
			if count%3 == 0 {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			} else {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("OK"))
			}
		}))
		defer server.Close()

		client := &http.Client{Timeout: 1 * time.Second}

		errors := 0
		for i := 0; i < 10; i++ {
			resp, err := client.Get(server.URL)
			if err == nil {
				if resp.StatusCode >= 500 {
					errors++
				}
				resp.Body.Close()
			}
		}

		assert.Greater(t, errors, 0)
		assert.Less(t, errors, 10)
	})

	t.Run("TimeoutScenario", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * time.Second)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := &http.Client{Timeout: 100 * time.Millisecond}

		_, err := client.Get(server.URL)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "deadline exceeded")
	})

	t.Run("RetryStorm", func(t *testing.T) {
		var requestCount atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount.Add(1)
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		}))
		defer server.Close()

		client := &http.Client{Timeout: 1 * time.Second}

		for retry := 0; retry < 3; retry++ {
			for i := 0; i < 5; i++ {
				resp, err := client.Get(server.URL)
				if err == nil {
					resp.Body.Close()
				}
			}
			time.Sleep(10 * time.Millisecond)
		}

		assert.Equal(t, int32(15), requestCount.Load())
	})
}

func TestObserverLifecycle(t *testing.T) {
	logger := zap.NewNop()
	config := &Config{
		Enabled:       true,
		FlushInterval: 100 * time.Millisecond,
	}

	observer, err := NewObserver(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err = observer.Start(ctx)
	if err != nil {
		t.Logf("Start failed (expected on non-Linux): %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	err = observer.Stop()
	assert.NoError(t, err)
}

func BenchmarkAggregator(b *testing.B) {
	aggregator := NewStatusAggregator(100 * time.Millisecond)

	event := &StatusEvent{
		ServiceHash:  12345,
		EndpointHash: 67890,
		StatusCode:   500,
		ErrorType:    Error5XX,
		Latency:      1000,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			aggregator.Add(event)
		}
	})
}

func BenchmarkHashDecoder(b *testing.B) {
	decoder := NewHashDecoder()

	for i := 0; i < 1000; i++ {
		decoder.AddService(uint32(i), "service-"+string(rune(i)))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			decoder.GetService(uint32(i % 1000))
			i++
		}
	})
}
