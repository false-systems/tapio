package discovery

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// BoundedWorkerPool implements WorkerPool with bounded concurrency and graceful scaling
type BoundedWorkerPool struct {
	// Configuration
	minWorkers  int
	maxWorkers  int
	idleTimeout time.Duration

	// State
	workers        int64
	queuedTasks    int64
	completedTasks int64
	failedTasks    int64

	// Channels
	taskQueue    chan workItem
	resultQueue  chan WorkResult
	shutdown     chan struct{}
	workerAdd    chan struct{}
	workerRemove chan struct{}

	// Synchronization
	mu     sync.RWMutex
	wg     sync.WaitGroup
	once   sync.Once
	closed int32

	// Metrics
	startTime     time.Time
	totalTaskTime int64 // nanoseconds

	// Object pools for efficiency
	workItemPool sync.Pool
	resultPool   sync.Pool
}

// workItem represents a unit of work in the pool
type workItem struct {
	id             uint64
	work           WorkFunc
	workWithResult WorkFuncWithResult
	ctx            context.Context
	resultCh       chan<- WorkResult
	submitted      time.Time
}

// NewBoundedWorkerPool creates a new worker pool with dynamic scaling
func NewBoundedWorkerPool(minWorkers, maxWorkers int, idleTimeout time.Duration) *BoundedWorkerPool {
	if minWorkers <= 0 {
		minWorkers = 1
	}
	if maxWorkers <= 0 {
		maxWorkers = runtime.GOMAXPROCS(0)
	}
	if minWorkers > maxWorkers {
		minWorkers = maxWorkers
	}
	if idleTimeout <= 0 {
		idleTimeout = 30 * time.Second
	}

	pool := &BoundedWorkerPool{
		minWorkers:   minWorkers,
		maxWorkers:   maxWorkers,
		idleTimeout:  idleTimeout,
		taskQueue:    make(chan workItem, maxWorkers*2), // Buffered for better throughput
		shutdown:     make(chan struct{}),
		workerAdd:    make(chan struct{}, maxWorkers),
		workerRemove: make(chan struct{}, maxWorkers),
		startTime:    time.Now(),
	}

	// Initialize object pools
	pool.workItemPool.New = func() interface{} {
		return &workItem{}
	}
	pool.resultPool.New = func() interface{} {
		return &WorkResult{}
	}

	// Start initial workers
	for i := 0; i < minWorkers; i++ {
		pool.startWorker()
	}

	// Start management goroutine
	go pool.manage()

	return pool
}

// Submit submits work to the pool
func (p *BoundedWorkerPool) Submit(ctx context.Context, work WorkFunc) error {
	if atomic.LoadInt32(&p.closed) == 1 {
		return ErrPoolClosed
	}

	item := p.getWorkItem()
	item.id = uint64(atomic.AddInt64(&p.queuedTasks, 1))
	item.work = work
	item.ctx = ctx
	item.submitted = time.Now()

	select {
	case p.taskQueue <- *item:
		p.putWorkItem(item)
		return nil
	case <-ctx.Done():
		p.putWorkItem(item)
		return ctx.Err()
	case <-p.shutdown:
		p.putWorkItem(item)
		return ErrPoolClosed
	}
}

// SubmitWithResult submits work and returns a result channel
func (p *BoundedWorkerPool) SubmitWithResult(ctx context.Context, work WorkFuncWithResult) <-chan WorkResult {
	resultCh := make(chan WorkResult, 1)

	if atomic.LoadInt32(&p.closed) == 1 {
		result := p.getResult()
		result.Error = ErrPoolClosed
		resultCh <- *result
		close(resultCh)
		p.putResult(result)
		return resultCh
	}

	item := p.getWorkItem()
	item.id = uint64(atomic.AddInt64(&p.queuedTasks, 1))
	item.workWithResult = work
	item.ctx = ctx
	item.resultCh = resultCh
	item.submitted = time.Now()

	select {
	case p.taskQueue <- *item:
		p.putWorkItem(item)
		return resultCh
	case <-ctx.Done():
		result := p.getResult()
		result.Error = ctx.Err()
		resultCh <- *result
		close(resultCh)
		p.putResult(result)
		p.putWorkItem(item)
		return resultCh
	case <-p.shutdown:
		result := p.getResult()
		result.Error = ErrPoolClosed
		resultCh <- *result
		close(resultCh)
		p.putResult(result)
		p.putWorkItem(item)
		return resultCh
	}
}

// Resize dynamically adjusts pool size
func (p *BoundedWorkerPool) Resize(size int) error {
	if atomic.LoadInt32(&p.closed) == 1 {
		return ErrPoolClosed
	}

	if size < p.minWorkers {
		size = p.minWorkers
	}
	if size > p.maxWorkers {
		size = p.maxWorkers
	}

	currentWorkers := int(atomic.LoadInt64(&p.workers))

	if size > currentWorkers {
		// Add workers
		for i := currentWorkers; i < size; i++ {
			select {
			case p.workerAdd <- struct{}{}:
			default:
				break
			}
		}
	} else if size < currentWorkers {
		// Remove workers
		for i := size; i < currentWorkers; i++ {
			select {
			case p.workerRemove <- struct{}{}:
			default:
				break
			}
		}
	}

	return nil
}

// Stats returns pool performance metrics
func (p *BoundedWorkerPool) Stats() PoolStats {
	queuedTasks := atomic.LoadInt64(&p.queuedTasks)
	completedTasks := atomic.LoadInt64(&p.completedTasks)
	failedTasks := atomic.LoadInt64(&p.failedTasks)
	workers := atomic.LoadInt64(&p.workers)
	totalTaskTime := atomic.LoadInt64(&p.totalTaskTime)

	var avgTaskTime time.Duration
	if completedTasks > 0 {
		avgTaskTime = time.Duration(totalTaskTime / completedTasks)
	}

	elapsed := time.Since(p.startTime).Seconds()
	var throughput float64
	if elapsed > 0 {
		throughput = float64(completedTasks) / elapsed
	}

	return PoolStats{
		ActiveWorkers:    int(workers),
		QueuedTasks:      int(queuedTasks - completedTasks - failedTasks),
		CompletedTasks:   completedTasks,
		FailedTasks:      failedTasks,
		AvgTaskTime:      avgTaskTime,
		ThroughputPerSec: throughput,
	}
}

// Shutdown gracefully shuts down the worker pool
func (p *BoundedWorkerPool) Shutdown(ctx context.Context) error {
	// Mark as closed
	if !atomic.CompareAndSwapInt32(&p.closed, 0, 1) {
		return nil // Already closed
	}

	// Signal shutdown
	p.once.Do(func() {
		close(p.shutdown)
	})

	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// manage handles dynamic worker scaling and lifecycle
func (p *BoundedWorkerPool) manage() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.shutdown:
			return
		case <-p.workerAdd:
			if int(atomic.LoadInt64(&p.workers)) < p.maxWorkers {
				p.startWorker()
			}
		case <-p.workerRemove:
			// Workers will automatically exit when idle
		case <-ticker.C:
			p.autoScale()
		}
	}
}

// autoScale automatically adjusts worker count based on load
func (p *BoundedWorkerPool) autoScale() {
	currentWorkers := int(atomic.LoadInt64(&p.workers))
	queueLen := len(p.taskQueue)

	// Scale up if queue is building up
	if queueLen > currentWorkers && currentWorkers < p.maxWorkers {
		p.startWorker()
	}

	// Don't scale down below minimum workers
	// Workers will exit naturally when idle
}

// startWorker starts a new worker goroutine
func (p *BoundedWorkerPool) startWorker() {
	atomic.AddInt64(&p.workers, 1)
	p.wg.Add(1)

	go func() {
		defer func() {
			atomic.AddInt64(&p.workers, -1)
			p.wg.Done()
		}()

		idleTimer := time.NewTimer(p.idleTimeout)
		defer idleTimer.Stop()

		for {
			// Reset idle timer
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(p.idleTimeout)

			select {
			case <-p.shutdown:
				return
			case <-p.workerRemove:
				// Exit this worker
				if int(atomic.LoadInt64(&p.workers)) > p.minWorkers {
					return
				}
			case <-idleTimer.C:
				// Exit if we have more than minimum workers
				if int(atomic.LoadInt64(&p.workers)) > p.minWorkers {
					return
				}
			case item := <-p.taskQueue:
				p.executeWork(item)
			}
		}
	}()
}

// executeWork executes a work item
func (p *BoundedWorkerPool) executeWork(item workItem) {
	start := time.Now()

	defer func() {
		duration := time.Since(start)
		atomic.AddInt64(&p.totalTaskTime, int64(duration))

		// Handle panic recovery
		if r := recover(); r != nil {
			atomic.AddInt64(&p.failedTasks, 1)

			if item.resultCh != nil {
				result := p.getResult()
				result.Error = &PanicError{Panic: r}
				result.Duration = duration

				select {
				case item.resultCh <- *result:
				default:
				}
				close(item.resultCh)
				p.putResult(result)
			}
		}
	}()

	if item.work != nil {
		// Execute work without result
		err := item.work(item.ctx)
		duration := time.Since(start)

		if err != nil {
			atomic.AddInt64(&p.failedTasks, 1)
		} else {
			atomic.AddInt64(&p.completedTasks, 1)
		}

		// Send result if channel provided
		if item.resultCh != nil {
			result := p.getResult()
			result.Error = err
			result.Duration = duration

			select {
			case item.resultCh <- *result:
			default:
			}
			close(item.resultCh)
			p.putResult(result)
		}
	} else if item.workWithResult != nil {
		// Execute work with result
		var (
			res interface{}
			err error
		)

		func() {
			defer func() {
				if r := recover(); r != nil {
					err = &PanicError{Panic: r}
				}
			}()
			res = item.workWithResult(item.ctx)
		}()

		duration := time.Since(start)

		if err != nil {
			atomic.AddInt64(&p.failedTasks, 1)
		} else {
			atomic.AddInt64(&p.completedTasks, 1)
		}

		// Send result
		if item.resultCh != nil {
			result := p.getResult()
			result.Result = res
			result.Error = err
			result.Duration = duration

			select {
			case item.resultCh <- *result:
			default:
			}
			close(item.resultCh)
			p.putResult(result)
		}
	}
}

// Object pool helpers for memory efficiency
func (p *BoundedWorkerPool) getWorkItem() *workItem {
	return p.workItemPool.Get().(*workItem)
}

func (p *BoundedWorkerPool) putWorkItem(item *workItem) {
	// Reset fields
	*item = workItem{}
	p.workItemPool.Put(item)
}

func (p *BoundedWorkerPool) getResult() *WorkResult {
	return p.resultPool.Get().(*WorkResult)
}

func (p *BoundedWorkerPool) putResult(result *WorkResult) {
	// Reset fields
	*result = WorkResult{}
	p.resultPool.Put(result)
}

// Custom error types
type PanicError struct {
	Panic interface{}
}

func (e *PanicError) Error() string {
	return "worker panic: " + toString(e.Panic)
}

func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	if err, ok := v.(error); ok {
		return err.Error()
	}
	return "unknown panic"
}

// Pool errors
var (
	ErrPoolClosed = &PoolError{Message: "worker pool is closed"}
)

type PoolError struct {
	Message string
}

func (e *PoolError) Error() string {
	return e.Message
}
