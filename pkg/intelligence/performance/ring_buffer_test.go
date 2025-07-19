package performance

import (
	"runtime"
	"sync"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRingBuffer_BasicOperations(t *testing.T) {
	buffer, err := NewRingBuffer(8)
	require.NoError(t, err)

	// Test empty buffer
	assert.Equal(t, uint64(0), buffer.Size())

	// Test single put/get
	data := "test"
	err = buffer.Put(unsafe.Pointer(&data))
	require.NoError(t, err)
	assert.Equal(t, uint64(1), buffer.Size())

	result, err := buffer.Get()
	require.NoError(t, err)
	assert.Equal(t, data, *(*string)(result))
	assert.Equal(t, uint64(0), buffer.Size())
}

func TestRingBuffer_FullBuffer(t *testing.T) {
	buffer, err := NewRingBuffer(4)
	require.NoError(t, err)

	// Fill buffer to capacity
	for i := 0; i < 4; i++ {
		data := i
		err = buffer.Put(unsafe.Pointer(&data))
		require.NoError(t, err)
	}

	// Buffer should be full
	data := 999
	err = buffer.Put(unsafe.Pointer(&data))
	assert.Error(t, err)
	assert.Equal(t, uint64(4), buffer.Size())

	// Drain and verify order
	for i := 0; i < 4; i++ {
		result, err := buffer.Get()
		require.NoError(t, err)
		assert.Equal(t, i, *(*int)(result))
	}

	assert.Equal(t, uint64(0), buffer.Size())
}

func TestRingBuffer_EmptyGet(t *testing.T) {
	buffer, err := NewRingBuffer(4)
	require.NoError(t, err)

	_, err = buffer.Get()
	assert.Error(t, err)
}

func TestRingBuffer_TryOperations(t *testing.T) {
	buffer, err := NewRingBuffer(2)
	require.NoError(t, err)

	// Test TryPut
	data1 := "test1"
	assert.True(t, buffer.TryPut(unsafe.Pointer(&data1)))

	data2 := "test2"
	assert.True(t, buffer.TryPut(unsafe.Pointer(&data2)))

	// Buffer full
	data3 := "test3"
	assert.False(t, buffer.TryPut(unsafe.Pointer(&data3)))

	// Test TryGet
	result, ok := buffer.TryGet()
	assert.True(t, ok)
	assert.Equal(t, "test1", *(*string)(result))

	result, ok = buffer.TryGet()
	assert.True(t, ok)
	assert.Equal(t, "test2", *(*string)(result))

	// Buffer empty
	_, ok = buffer.TryGet()
	assert.False(t, ok)
}

func TestRingBuffer_InvalidSize(t *testing.T) {
	// Test non-power-of-2 size
	_, err := NewRingBuffer(3)
	assert.Error(t, err)

	// Test zero size
	_, err = NewRingBuffer(0)
	assert.Error(t, err)
}

// Concurrent tests
func TestRingBuffer_ConcurrentAccess(t *testing.T) {
	buffer, err := NewRingBuffer(1024)
	require.NoError(t, err)

	const numGoroutines = 10
	const itemsPerGoroutine = 100

	var wg sync.WaitGroup

	// Producers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < itemsPerGoroutine; j++ {
				value := id*itemsPerGoroutine + j
				for {
					if buffer.TryPut(unsafe.Pointer(&value)) {
						break
					}
					runtime.Gosched()
				}
			}
		}(i)
	}

	// Consumer
	var consumed []int
	var mu sync.Mutex
	wg.Add(1)
	go func() {
		defer wg.Done()
		for len(consumed) < numGoroutines*itemsPerGoroutine {
			if result, ok := buffer.TryGet(); ok {
				value := *(*int)(result)
				mu.Lock()
				consumed = append(consumed, value)
				mu.Unlock()
			} else {
				runtime.Gosched()
			}
		}
	}()

	wg.Wait()

	assert.Equal(t, numGoroutines*itemsPerGoroutine, len(consumed))
	assert.Equal(t, uint64(0), buffer.Size())
}

// Benchmark tests
func BenchmarkRingBuffer_Put(b *testing.B) {
	buffer, err := NewRingBuffer(uint64(b.N))
	if err != nil {
		b.Fatal(err)
	}

	data := "benchmark"
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := buffer.Put(unsafe.Pointer(&data))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRingBuffer_Get(b *testing.B) {
	buffer, err := NewRingBuffer(uint64(b.N))
	if err != nil {
		b.Fatal(err)
	}

	// Pre-fill buffer
	data := "benchmark"
	for i := 0; i < b.N; i++ {
		buffer.Put(unsafe.Pointer(&data))
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := buffer.Get()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRingBuffer_TryPut(b *testing.B) {
	buffer, err := NewRingBuffer(uint64(b.N))
	if err != nil {
		b.Fatal(err)
	}

	data := "benchmark"
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if !buffer.TryPut(unsafe.Pointer(&data)) {
			b.Fatal("TryPut failed")
		}
	}
}

func BenchmarkRingBuffer_TryGet(b *testing.B) {
	buffer, err := NewRingBuffer(uint64(b.N))
	if err != nil {
		b.Fatal(err)
	}

	// Pre-fill buffer
	data := "benchmark"
	for i := 0; i < b.N; i++ {
		buffer.Put(unsafe.Pointer(&data))
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, ok := buffer.TryGet(); !ok {
			b.Fatal("TryGet failed")
		}
	}
}

func BenchmarkRingBuffer_Concurrent(b *testing.B) {
	buffer, err := NewRingBuffer(1024)
	if err != nil {
		b.Fatal(err)
	}

	b.RunParallel(func(pb *testing.PB) {
		data := "benchmark"
		for pb.Next() {
			// Try to put, then try to get
			if buffer.TryPut(unsafe.Pointer(&data)) {
				buffer.TryGet()
			}
		}
	})
}

func BenchmarkRingBuffer_HighThroughput(b *testing.B) {
	buffer, err := NewRingBuffer(8192)
	if err != nil {
		b.Fatal(err)
	}

	const numProducers = 4
	const numConsumers = 2

	b.ResetTimer()

	var wg sync.WaitGroup
	start := make(chan struct{})

	// Producers
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			data := "high-throughput"
			for j := 0; j < b.N/numProducers; j++ {
				for !buffer.TryPut(unsafe.Pointer(&data)) {
					runtime.Gosched()
				}
			}
		}()
	}

	// Consumers
	for i := 0; i < numConsumers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			consumed := 0
			target := b.N / numConsumers
			for consumed < target {
				if _, ok := buffer.TryGet(); ok {
					consumed++
				} else {
					runtime.Gosched()
				}
			}
		}()
	}

	close(start)
	wg.Wait()
}
