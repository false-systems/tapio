package runtime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/cache"
)

// Mock informer for testing
type mockInformer struct {
	started      bool
	stopped      bool
	synced       bool
	addHandler   cache.ResourceEventHandler
	mu           sync.Mutex
	stopCh       chan struct{}
	hasSyncedVal bool
}

func newMockInformer() *mockInformer {
	return &mockInformer{
		stopCh:       make(chan struct{}),
		hasSyncedVal: true, // Default to synced
	}
}

func (m *mockInformer) AddEventHandler(handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addHandler = handler
	return nil, nil
}

func (m *mockInformer) AddEventHandlerWithResyncPeriod(handler cache.ResourceEventHandler, resyncPeriod time.Duration) (cache.ResourceEventHandlerRegistration, error) {
	return m.AddEventHandler(handler)
}

func (m *mockInformer) AddEventHandlerWithOptions(handler cache.ResourceEventHandler, options cache.HandlerOptions) (cache.ResourceEventHandlerRegistration, error) {
	return m.AddEventHandler(handler)
}

func (m *mockInformer) RemoveEventHandler(handle cache.ResourceEventHandlerRegistration) error {
	return nil
}

func (m *mockInformer) AddIndexers(indexers cache.Indexers) error {
	return nil
}

func (m *mockInformer) GetStore() cache.Store {
	return nil
}

func (m *mockInformer) GetIndexer() cache.Indexer {
	return nil
}

func (m *mockInformer) GetController() cache.Controller {
	return nil
}

func (m *mockInformer) Run(stopCh <-chan struct{}) {
	m.mu.Lock()
	m.started = true
	m.mu.Unlock()

	<-stopCh

	m.mu.Lock()
	m.stopped = true
	m.mu.Unlock()
}

func (m *mockInformer) RunWithContext(ctx context.Context) {
	m.Run(ctx.Done())
}

func (m *mockInformer) HasSynced() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hasSyncedVal
}

func (m *mockInformer) LastSyncResourceVersion() string {
	return ""
}

func (m *mockInformer) SetWatchErrorHandler(handler cache.WatchErrorHandler) error {
	return nil
}

func (m *mockInformer) SetWatchErrorHandlerWithContext(handler cache.WatchErrorHandlerWithContext) error {
	return nil
}

func (m *mockInformer) SetTransform(handler cache.TransformFunc) error {
	return nil
}

func (m *mockInformer) IsStopped() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopped
}

func (m *mockInformer) IsStarted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

// RED: Test K8sRuntime creation
func TestNewK8sRuntime(t *testing.T) {
	runtime := NewK8sRuntime()
	require.NotNil(t, runtime)
}

// RED: Test adding informer
func TestK8sRuntime_AddInformer(t *testing.T) {
	runtime := NewK8sRuntime()
	informer := newMockInformer()

	var addCalled, updateCalled, deleteCalled bool

	handlers := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			addCalled = true
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			updateCalled = true
		},
		DeleteFunc: func(obj interface{}) {
			deleteCalled = true
		},
	}

	err := runtime.AddInformer(informer, handlers)
	require.NoError(t, err)

	// Verify handler was registered
	require.NotNil(t, informer.addHandler)

	// Trigger handlers
	informer.addHandler.OnAdd(&struct{}{}, false)
	assert.True(t, addCalled)

	informer.addHandler.OnUpdate(&struct{}{}, &struct{}{})
	assert.True(t, updateCalled)

	informer.addHandler.OnDelete(&struct{}{})
	assert.True(t, deleteCalled)
}

// RED: Test starting runtime starts informers
func TestK8sRuntime_Start(t *testing.T) {
	runtime := NewK8sRuntime()
	informer := newMockInformer()

	err := runtime.AddInformer(informer, cache.ResourceEventHandlerFuncs{})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Start runtime
	go func() {
		_ = runtime.Start(ctx) // Ignore: Test goroutine, error checked elsewhere
	}()

	// Give it time to start
	time.Sleep(100 * time.Millisecond)

	// Verify informer was started
	assert.True(t, informer.IsStarted())

	// Cancel and verify stop
	cancel()
	time.Sleep(100 * time.Millisecond)
}

// RED: Test WaitForCacheSync
func TestK8sRuntime_WaitForCacheSync(t *testing.T) {
	runtime := NewK8sRuntime()
	informer := newMockInformer()

	err := runtime.AddInformer(informer, cache.ResourceEventHandlerFuncs{})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Start runtime
	go func() {
		_ = runtime.Start(ctx) // Ignore: Test goroutine, error checked elsewhere
	}()

	time.Sleep(50 * time.Millisecond)

	// Wait for sync
	err = runtime.WaitForCacheSync(ctx)
	assert.NoError(t, err)
}

// RED: Test WaitForCacheSync timeout
func TestK8sRuntime_WaitForCacheSync_Timeout(t *testing.T) {
	runtime := NewK8sRuntime()
	informer := newMockInformer()
	informer.hasSyncedVal = false // Never syncs

	err := runtime.AddInformer(informer, cache.ResourceEventHandlerFuncs{})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	go func() {
		_ = runtime.Start(ctx) // Ignore: Test goroutine, error checked elsewhere
	}()

	time.Sleep(50 * time.Millisecond)

	// Wait for sync with short timeout
	syncCtx, syncCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer syncCancel()

	err = runtime.WaitForCacheSync(syncCtx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

// RED: Test Stop closes all informers
func TestK8sRuntime_Stop(t *testing.T) {
	runtime := NewK8sRuntime()
	informer1 := newMockInformer()
	informer2 := newMockInformer()

	err := runtime.AddInformer(informer1, cache.ResourceEventHandlerFuncs{})
	require.NoError(t, err)

	err = runtime.AddInformer(informer2, cache.ResourceEventHandlerFuncs{})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	go func() {
		_ = runtime.Start(ctx) // Ignore: Test goroutine, error checked elsewhere
	}()

	time.Sleep(100 * time.Millisecond)

	// Stop runtime
	runtime.Stop()

	time.Sleep(100 * time.Millisecond)

	// Verify both informers were stopped
	assert.True(t, informer1.IsStopped())
	assert.True(t, informer2.IsStopped())
}

// RED: Test adding informer after start fails
func TestK8sRuntime_AddInformer_AfterStart(t *testing.T) {
	runtime := NewK8sRuntime()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	go func() {
		_ = runtime.Start(ctx) // Ignore: Test goroutine, error checked elsewhere
	}()

	time.Sleep(50 * time.Millisecond)

	// Try to add informer after start
	informer := newMockInformer()
	err := runtime.AddInformer(informer, cache.ResourceEventHandlerFuncs{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")
}
