package internal

import (
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

func TestRateLimiter(t *testing.T) {
	// Test with 10 events per second
	rl := newRateLimiter(10)

	// Should allow first 10 events immediately
	allowed := 0
	for i := 0; i < 15; i++ {
		if rl.Allow() {
			allowed++
		}
	}

	if allowed != 10 {
		t.Errorf("RateLimiter allowed %d events, want 10", allowed)
	}

	// Wait for refill
	time.Sleep(100 * time.Millisecond)

	// Should allow more events after refill
	if !rl.Allow() {
		t.Error("RateLimiter should allow events after refill")
	}
}

func TestGenerateSubscriptionID(t *testing.T) {
	// Generate multiple IDs and ensure they're unique
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := generateSubscriptionID()
		if id == "" {
			t.Error("generateSubscriptionID() returned empty string")
		}
		if ids[id] {
			t.Errorf("generateSubscriptionID() returned duplicate ID: %s", id)
		}
		ids[id] = true
	}
}

func TestIsSystemProcess(t *testing.T) {
	tests := []struct {
		name     string
		info     core.ProcessInfo
		expected bool
	}{
		{
			name: "kernel_thread_by_pid",
			info: core.ProcessInfo{
				PID:  10,
				Name: "ksoftirqd/0",
			},
			expected: true,
		},
		{
			name: "kernel_thread_by_ppid",
			info: core.ProcessInfo{
				PID:  1234,
				PPID: 2,
				Name: "some_kernel_thread",
			},
			expected: true,
		},
		{
			name: "systemd_process",
			info: core.ProcessInfo{
				PID:  1,
				Name: "systemd",
			},
			expected: true,
		},
		{
			name: "kworker_process",
			info: core.ProcessInfo{
				PID:  5000,
				Name: "kworker/0:1",
			},
			expected: true,
		},
		{
			name: "regular_user_process",
			info: core.ProcessInfo{
				PID:  10000,
				Name: "firefox",
			},
			expected: false,
		},
		{
			name: "systemd_prefixed_service",
			info: core.ProcessInfo{
				PID:  2000,
				Name: "systemd-resolved",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSystemProcess(tt.info)
			if result != tt.expected {
				t.Errorf("isSystemProcess(%+v) = %v, want %v", tt.info, result, tt.expected)
			}
		})
	}
}

func TestMetricCollector(t *testing.T) {
	mc := newMetricCollector()

	// Test increment
	mc.increment("test_counter")
	mc.increment("test_counter")
	
	if got := mc.get("test_counter"); got != 2 {
		t.Errorf("MetricCollector.get() = %d, want 2", got)
	}

	// Test add
	mc.add("test_gauge", 100)
	mc.add("test_gauge", 50)
	
	if got := mc.get("test_gauge"); got != 150 {
		t.Errorf("MetricCollector.get() = %d, want 150", got)
	}

	// Test non-existent metric
	if got := mc.get("non_existent"); got != 0 {
		t.Errorf("MetricCollector.get() for non-existent metric = %d, want 0", got)
	}

	// Test concurrent access
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				mc.increment("concurrent_counter")
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	if got := mc.get("concurrent_counter"); got != 1000 {
		t.Errorf("MetricCollector concurrent increment = %d, want 1000", got)
	}
}

func TestCircularBuffer(t *testing.T) {
	cb := newCircularBuffer(5)

	// Test adding events
	events := []domain.Event{
		{ID: "1"},
		{ID: "2"},
		{ID: "3"},
	}

	for _, event := range events {
		cb.Add(event)
	}

	// Test GetAll
	all := cb.GetAll()
	if len(all) != 3 {
		t.Errorf("CircularBuffer.GetAll() returned %d events, want 3", len(all))
	}

	// Test GetRecent
	recent := cb.GetRecent(2)
	if len(recent) != 2 {
		t.Errorf("CircularBuffer.GetRecent(2) returned %d events, want 2", len(recent))
	}
	if recent[1].ID != "3" {
		t.Errorf("CircularBuffer.GetRecent(2) last event ID = %s, want 3", recent[1].ID)
	}

	// Test buffer overflow
	for i := 4; i <= 7; i++ {
		cb.Add(domain.Event{ID: string(rune('0' + i))})
	}

	all = cb.GetAll()
	if len(all) != 5 {
		t.Errorf("CircularBuffer.GetAll() after overflow returned %d events, want 5", len(all))
	}

	// First two events should have been overwritten
	if all[0].ID != "3" {
		t.Errorf("CircularBuffer first event after overflow = %s, want 3", all[0].ID)
	}

	// Test empty buffer
	emptyBuffer := newCircularBuffer(10)
	if got := emptyBuffer.GetAll(); got != nil {
		t.Errorf("CircularBuffer.GetAll() on empty buffer = %v, want nil", got)
	}
	if got := emptyBuffer.GetRecent(5); got != nil {
		t.Errorf("CircularBuffer.GetRecent() on empty buffer = %v, want nil", got)
	}
}

func TestProgramMetadata(t *testing.T) {
	pm := newProgramMetadata()

	// Test Add and Get
	meta := &programMeta{
		loadedAt:    time.Now(),
		attachedTo:  "sys_open",
		programType: core.ProgramTypeKprobe,
		bytecode:    []byte{0x01, 0x02},
	}

	pm.Add("test_prog", meta)

	retrieved, ok := pm.Get("test_prog")
	if !ok {
		t.Error("ProgramMetadata.Get() returned false for existing program")
	}

	if retrieved.attachedTo != meta.attachedTo {
		t.Errorf("ProgramMetadata.Get() attachedTo = %s, want %s", retrieved.attachedTo, meta.attachedTo)
	}

	// Test Get non-existent
	_, ok = pm.Get("non_existent")
	if ok {
		t.Error("ProgramMetadata.Get() returned true for non-existent program")
	}

	// Test Remove
	pm.Remove("test_prog")
	_, ok = pm.Get("test_prog")
	if ok {
		t.Error("ProgramMetadata.Get() returned true after Remove()")
	}
}

func TestBytesPool(t *testing.T) {
	pool := newBytesPool(1024)

	// Get buffer from pool
	buf := pool.Get()
	if buf == nil || cap(*buf) != 1024 {
		t.Errorf("BytesPool.Get() returned buffer with cap %d, want 1024", cap(*buf))
	}

	// Use the buffer
	*buf = append(*buf, []byte("test data")...)

	// Return to pool
	pool.Put(buf)

	// Get again - should be reset
	buf2 := pool.Get()
	if len(*buf2) != 0 {
		t.Errorf("BytesPool.Get() returned buffer with len %d, want 0", len(*buf2))
	}

	// Test nil safety
	pool.Put(nil)
	
	// Test concurrent access
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				b := pool.Get()
				*b = append(*b, byte(j))
				pool.Put(b)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestSubscription(t *testing.T) {
	// Test subscription struct
	sub := subscription{
		id: generateSubscriptionID(),
		criteria: domain.QueryCriteria{
			TimeWindow: domain.TimeWindow{
				Start: time.Now().Add(-time.Hour),
				End:   time.Now(),
			},
		},
		options: domain.SubscriptionOptions{
			BufferSize: 100,
		},
		ch:      make(chan domain.Event, 100),
		created: time.Now(),
	}

	if sub.id == "" {
		t.Error("Subscription ID should not be empty")
	}

	if sub.ch == nil {
		t.Error("Subscription channel should not be nil")
	}

	if sub.options.BufferSize != 100 {
		t.Errorf("Subscription buffer size = %d, want 100", sub.options.BufferSize)
	}
}

// Benchmark tests

func BenchmarkRateLimiter(b *testing.B) {
	rl := newRateLimiter(10000)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.Allow()
	}
}

func BenchmarkGenerateSubscriptionID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		generateSubscriptionID()
	}
}

func BenchmarkCircularBufferAdd(b *testing.B) {
	cb := newCircularBuffer(1000)
	event := domain.Event{ID: "test"}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cb.Add(event)
	}
}

func BenchmarkBytesPool(b *testing.B) {
	pool := newBytesPool(4096)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := pool.Get()
		*buf = append(*buf, make([]byte, 1000)...)
		pool.Put(buf)
	}
}