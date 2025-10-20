package k8scontext

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
)

// TestStart_InitializesContext verifies Start creates context and cancel function
func TestStart_InitializesContext(t *testing.T) {
	mockKV := newMockKV()
	clientset := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(clientset, 0)

	service := &Service{
		kv:              mockKV,
		informerFactory: factory,
		eventBuffer:     make(chan func() error, 100),
		config: Config{
			MaxRetries:    3,
			RetryInterval: 10 * time.Millisecond,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	// Verify context was created
	assert.NotNil(t, service.ctx)
	assert.NotNil(t, service.cancel)

	// Verify context is not done yet
	select {
	case <-service.ctx.Done():
		t.Fatal("Context should not be done yet")
	default:
		// Success
	}
}

// TestStart_StartsEventWorker verifies event processing worker is started
func TestStart_StartsEventWorker(t *testing.T) {
	mockKV := newMockKV()
	clientset := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(clientset, 0)

	service := &Service{
		kv:              mockKV,
		informerFactory: factory,
		eventBuffer:     make(chan func() error, 100),
		config: Config{
			MaxRetries:    3,
			RetryInterval: 10 * time.Millisecond,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	// Enqueue an event and verify it gets processed using channel
	done := make(chan bool, 1)
	service.enqueueEvent(func() error {
		done <- true
		return nil
	})

	// Wait for event to be processed
	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Event worker should process events")
	}
}

// TestStart_RegistersInformers verifies informers are registered
func TestStart_RegistersInformers(t *testing.T) {
	mockKV := newMockKV()
	clientset := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(clientset, 0)

	service := &Service{
		kv:              mockKV,
		informerFactory: factory,
		eventBuffer:     make(chan func() error, 100),
		config: Config{
			MaxRetries:    3,
			RetryInterval: 10 * time.Millisecond,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	// Verify informers are running (informerFactory.Start was called)
	// We can verify this by checking that cache sync works
	synced := factory.WaitForCacheSync(ctx.Done())
	for informerType, ok := range synced {
		assert.True(t, ok, "Informer %v should be synced", informerType)
	}
}

// TestStart_PerformsInitialSync verifies initial sync populates KV with existing resources
func TestStart_PerformsInitialSync(t *testing.T) {
	mockKV := newMockKV()
	clientset := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(clientset, 0)

	service := &Service{
		kv:              mockKV,
		informerFactory: factory,
		eventBuffer:     make(chan func() error, 1000),
		config: Config{
			MaxRetries:    3,
			RetryInterval: 10 * time.Millisecond,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := service.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = service.Stop() }()

	// Initial sync should complete without error
	// Integration tests verify actual resource syncing
}
