package exports

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Job represents a unit of work for the worker pool
type Job interface {
	Execute(ctx context.Context) (interface{}, error)
}

// WorkerPool manages a pool of workers for async processing
type WorkerPool struct {
	// Configuration
	workerCount int
	queueSize   int

	// Channels
	jobQueue    chan Job
	stopChan    chan struct{}
	
	// Worker management
	workers     []*Worker
	wg          sync.WaitGroup
	
	// State
	running     bool
	mutex       sync.RWMutex
	
	// Callbacks
	resultCallback func(job interface{}, result interface{}, err error)
	
	// Metrics
	processed   uint64
	failed      uint64
	queueDepth  int
}

// Worker represents a single worker in the pool
type Worker struct {
	id          int
	pool        *WorkerPool
	stopChan    chan struct{}
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(workerCount, queueSize int) *WorkerPool {
	return &WorkerPool{
		workerCount: workerCount,
		queueSize:   queueSize,
		jobQueue:    make(chan Job, queueSize),
		stopChan:    make(chan struct{}),
		workers:     make([]*Worker, 0, workerCount),
	}
}

// SetResultCallback sets the callback for job results
func (wp *WorkerPool) SetResultCallback(callback func(job interface{}, result interface{}, err error)) {
	wp.mutex.Lock()
	defer wp.mutex.Unlock()
	wp.resultCallback = callback
}

// Start starts the worker pool
func (wp *WorkerPool) Start(ctx context.Context) error {
	wp.mutex.Lock()
	defer wp.mutex.Unlock()

	if wp.running {
		return fmt.Errorf("worker pool already running")
	}

	// Create and start workers
	for i := 0; i < wp.workerCount; i++ {
		worker := &Worker{
			id:       i,
			pool:     wp,
			stopChan: make(chan struct{}),
		}
		wp.workers = append(wp.workers, worker)
		wp.wg.Add(1)
		go worker.run(ctx)
	}

	wp.running = true
	return nil
}

// Stop stops the worker pool and waits for all workers to finish
func (wp *WorkerPool) Stop() {
	wp.mutex.Lock()
	if !wp.running {
		wp.mutex.Unlock()
		return
	}
	wp.running = false
	wp.mutex.Unlock()

	// Signal workers to stop
	close(wp.stopChan)
	
	// Stop all workers
	for _, worker := range wp.workers {
		close(worker.stopChan)
	}
	
	// Wait for all workers to finish
	wp.wg.Wait()
	
	// Close job queue
	close(wp.jobQueue)
}

// Submit submits a job to the worker pool
func (wp *WorkerPool) Submit(job Job) error {
	wp.mutex.RLock()
	if !wp.running {
		wp.mutex.RUnlock()
		return fmt.Errorf("worker pool not running")
	}
	wp.mutex.RUnlock()

	select {
	case wp.jobQueue <- job:
		return nil
	default:
		return fmt.Errorf("job queue full")
	}
}

// SubmitWithTimeout submits a job with a timeout
func (wp *WorkerPool) SubmitWithTimeout(job Job, timeout time.Duration) error {
	wp.mutex.RLock()
	if !wp.running {
		wp.mutex.RUnlock()
		return fmt.Errorf("worker pool not running")
	}
	wp.mutex.RUnlock()

	select {
	case wp.jobQueue <- job:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout submitting job")
	}
}

// QueueDepth returns the current queue depth
func (wp *WorkerPool) QueueDepth() int {
	return len(wp.jobQueue)
}

// Metrics returns worker pool metrics
func (wp *WorkerPool) Metrics() map[string]interface{} {
	wp.mutex.RLock()
	defer wp.mutex.RUnlock()

	return map[string]interface{}{
		"worker_count": wp.workerCount,
		"queue_size":   wp.queueSize,
		"queue_depth":  len(wp.jobQueue),
		"processed":    wp.processed,
		"failed":       wp.failed,
		"running":      wp.running,
	}
}

// run is the main worker loop
func (w *Worker) run(ctx context.Context) {
	defer w.pool.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopChan:
			return
		case <-w.pool.stopChan:
			return
		case job, ok := <-w.pool.jobQueue:
			if !ok {
				return
			}
			w.processJob(ctx, job)
		}
	}
}

// processJob processes a single job
func (w *Worker) processJob(ctx context.Context, job Job) {
	// Execute the job
	result, err := job.Execute(ctx)
	
	// Update metrics
	w.pool.mutex.Lock()
	if err != nil {
		w.pool.failed++
	} else {
		w.pool.processed++
	}
	callback := w.pool.resultCallback
	w.pool.mutex.Unlock()
	
	// Call result callback if set
	if callback != nil {
		callback(job, result, err)
	}
}

// RetryableJob wraps a job with retry logic
type RetryableJob struct {
	Job         Job
	MaxRetries  int
	RetryDelay  time.Duration
	BackoffRate float64
	
	currentRetry int
}

// Execute implements the Job interface with retry logic
func (rj *RetryableJob) Execute(ctx context.Context) (interface{}, error) {
	var lastErr error
	delay := rj.RetryDelay

	for attempt := 0; attempt <= rj.MaxRetries; attempt++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Execute the job
		result, err := rj.Job.Execute(ctx)
		if err == nil {
			return result, nil
		}

		lastErr = err
		rj.currentRetry = attempt

		// Check if this is a retryable error
		if exportErr, ok := err.(*ExportError); ok && !exportErr.Retryable {
			return nil, err
		}

		// Don't sleep after the last attempt
		if attempt < rj.MaxRetries {
			select {
			case <-time.After(delay):
				// Apply exponential backoff
				delay = time.Duration(float64(delay) * rj.BackoffRate)
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}

	return nil, fmt.Errorf("job failed after %d retries: %w", rj.MaxRetries, lastErr)
}

// BatchJob represents a batch of jobs to be executed together
type BatchJob struct {
	Jobs []Job
}

// Execute implements the Job interface for batch execution
func (bj *BatchJob) Execute(ctx context.Context) (interface{}, error) {
	results := make([]interface{}, len(bj.Jobs))
	errors := make([]error, len(bj.Jobs))
	
	// Execute all jobs
	for i, job := range bj.Jobs {
		result, err := job.Execute(ctx)
		results[i] = result
		errors[i] = err
	}
	
	// Check if any job failed
	var failedCount int
	for _, err := range errors {
		if err != nil {
			failedCount++
		}
	}
	
	if failedCount > 0 {
		return results, fmt.Errorf("%d out of %d jobs failed", failedCount, len(bj.Jobs))
	}
	
	return results, nil
}

// PriorityJob represents a job with priority
type PriorityJob struct {
	Job      Job
	Priority int
}

// PriorityQueue implements a priority queue for jobs
type PriorityQueue struct {
	jobs   []*PriorityJob
	mutex  sync.Mutex
	notify chan struct{}
}

// NewPriorityQueue creates a new priority queue
func NewPriorityQueue() *PriorityQueue {
	return &PriorityQueue{
		jobs:   make([]*PriorityJob, 0),
		notify: make(chan struct{}, 1),
	}
}

// Push adds a job to the priority queue
func (pq *PriorityQueue) Push(job *PriorityJob) {
	pq.mutex.Lock()
	defer pq.mutex.Unlock()
	
	// Insert job in priority order
	inserted := false
	for i, existingJob := range pq.jobs {
		if job.Priority > existingJob.Priority {
			pq.jobs = append(pq.jobs[:i], append([]*PriorityJob{job}, pq.jobs[i:]...)...)
			inserted = true
			break
		}
	}
	
	if !inserted {
		pq.jobs = append(pq.jobs, job)
	}
	
	// Notify waiters
	select {
	case pq.notify <- struct{}{}:
	default:
	}
}

// Pop removes and returns the highest priority job
func (pq *PriorityQueue) Pop() *PriorityJob {
	pq.mutex.Lock()
	defer pq.mutex.Unlock()
	
	if len(pq.jobs) == 0 {
		return nil
	}
	
	job := pq.jobs[0]
	pq.jobs = pq.jobs[1:]
	return job
}

// PopWait removes and returns the highest priority job, waiting if empty
func (pq *PriorityQueue) PopWait(ctx context.Context) (*PriorityJob, error) {
	for {
		job := pq.Pop()
		if job != nil {
			return job, nil
		}
		
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-pq.notify:
			// Try again
		}
	}
}

// Size returns the number of jobs in the queue
func (pq *PriorityQueue) Size() int {
	pq.mutex.Lock()
	defer pq.mutex.Unlock()
	return len(pq.jobs)
}