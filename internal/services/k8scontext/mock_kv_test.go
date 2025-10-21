package k8scontext

import (
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// mockKV is a simple in-memory NATS KV for testing
type mockKV struct {
	mu    sync.RWMutex
	data  map[string][]byte
	fails bool // simulate failures
}

func newMockKV() *mockKV {
	return &mockKV{
		data: make(map[string][]byte),
	}
}

func (m *mockKV) Put(key string, value []byte) (uint64, error) {
	if m.fails {
		return 0, fmt.Errorf("mock KV failure")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return 1, nil
}

func (m *mockKV) Get(key string) (nats.KeyValueEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if value, ok := m.data[key]; ok {
		return &mockKVEntry{key: key, value: value}, nil
	}
	return nil, nats.ErrKeyNotFound
}

func (m *mockKV) Delete(key string, opts ...nats.DeleteOpt) error {
	if m.fails {
		return fmt.Errorf("mock KV failure")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *mockKV) Bucket() string { return "mock-bucket" }

func (m *mockKV) len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.data)
}

// Stub methods to satisfy nats.KeyValue interface
func (m *mockKV) Create(string, []byte) (uint64, error)                             { return 0, nil }
func (m *mockKV) Update(string, []byte, uint64) (uint64, error)                     { return 0, nil }
func (m *mockKV) PutString(string, string) (uint64, error)                          { return 0, nil }
func (m *mockKV) GetRevision(string, uint64) (nats.KeyValueEntry, error)            { return nil, nil }
func (m *mockKV) Purge(string, ...nats.DeleteOpt) error                             { return nil }
func (m *mockKV) Watch(string, ...nats.WatchOpt) (nats.KeyWatcher, error)           { return nil, nil }
func (m *mockKV) WatchAll(...nats.WatchOpt) (nats.KeyWatcher, error)                { return nil, nil }
func (m *mockKV) WatchFiltered([]string, ...nats.WatchOpt) (nats.KeyWatcher, error) { return nil, nil }
func (m *mockKV) Keys(...nats.WatchOpt) ([]string, error)                           { return nil, nil }
func (m *mockKV) ListKeys(...nats.WatchOpt) (nats.KeyLister, error)                 { return nil, nil }
func (m *mockKV) History(string, ...nats.WatchOpt) ([]nats.KeyValueEntry, error)    { return nil, nil }
func (m *mockKV) PurgeDeletes(...nats.PurgeOpt) error                               { return nil }
func (m *mockKV) Status() (nats.KeyValueStatus, error)                              { return nil, nil }

// mockKVEntry implements nats.KeyValueEntry
type mockKVEntry struct {
	key   string
	value []byte
}

func (e *mockKVEntry) Key() string                { return e.key }
func (e *mockKVEntry) Value() []byte              { return e.value }
func (e *mockKVEntry) Bucket() string             { return "mock-bucket" }
func (e *mockKVEntry) Created() time.Time         { return time.Now() }
func (e *mockKVEntry) Delta() uint64              { return 0 }
func (e *mockKVEntry) Operation() nats.KeyValueOp { return nats.KeyValuePut }
func (e *mockKVEntry) Revision() uint64           { return 1 }
