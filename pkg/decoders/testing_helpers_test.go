package decoders

import (
	"time"

	"github.com/nats-io/nats.go"
)

// mockKVEntry implements nats.KeyValueEntry for testing
type mockKVEntry struct {
	value []byte
}

func (m *mockKVEntry) Key() string                { return "" }
func (m *mockKVEntry) Value() []byte              { return m.value }
func (m *mockKVEntry) Bucket() string             { return "" }
func (m *mockKVEntry) Revision() uint64           { return 0 }
func (m *mockKVEntry) Created() time.Time         { return time.Time{} }
func (m *mockKVEntry) Delta() uint64              { return 0 }
func (m *mockKVEntry) Operation() nats.KeyValueOp { return nats.KeyValuePut }

// mockKeyValue implements nats.KeyValue for testing
type mockKeyValue struct {
	data map[string][]byte
	err  error
}

func (m *mockKeyValue) Get(key string) (nats.KeyValueEntry, error) {
	if m.err != nil {
		return nil, m.err
	}
	if val, ok := m.data[key]; ok {
		return &mockKVEntry{value: val}, nil
	}
	return nil, nats.ErrKeyNotFound
}

func (m *mockKeyValue) GetRevision(key string, revision uint64) (nats.KeyValueEntry, error) {
	return m.Get(key)
}

// Unused methods for interface compliance
func (m *mockKeyValue) Put(key string, value []byte) (uint64, error)       { return 0, nil }
func (m *mockKeyValue) PutString(key string, value string) (uint64, error) { return 0, nil }
func (m *mockKeyValue) Create(key string, value []byte) (uint64, error)    { return 0, nil }
func (m *mockKeyValue) Update(key string, value []byte, last uint64) (uint64, error) {
	return 0, nil
}
func (m *mockKeyValue) Delete(key string, opts ...nats.DeleteOpt) error { return nil }
func (m *mockKeyValue) Purge(key string, opts ...nats.DeleteOpt) error  { return nil }
func (m *mockKeyValue) Watch(keys string, opts ...nats.WatchOpt) (nats.KeyWatcher, error) {
	return nil, nil
}
func (m *mockKeyValue) WatchAll(opts ...nats.WatchOpt) (nats.KeyWatcher, error) { return nil, nil }
func (m *mockKeyValue) WatchFiltered(keys []string, opts ...nats.WatchOpt) (nats.KeyWatcher, error) {
	return nil, nil
}
func (m *mockKeyValue) Keys(opts ...nats.WatchOpt) ([]string, error)           { return nil, nil }
func (m *mockKeyValue) ListKeys(opts ...nats.WatchOpt) (nats.KeyLister, error) { return nil, nil }
func (m *mockKeyValue) History(key string, opts ...nats.WatchOpt) ([]nats.KeyValueEntry, error) {
	return nil, nil
}
func (m *mockKeyValue) Bucket() string                           { return "" }
func (m *mockKeyValue) PurgeDeletes(opts ...nats.PurgeOpt) error { return nil }
func (m *mockKeyValue) Status() (nats.KeyValueStatus, error)     { return nil, nil }
