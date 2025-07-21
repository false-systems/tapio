package pipeline

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// JobHandler defines the function signature for job processing
type JobHandler func(ctx context.Context, job *Job) error

// Job represents a unit of work to be processed
type Job struct {
	ID       string
	Type     string
	Payload  interface{}
	Handler  JobHandler
	Priority int
	
	// Timing information
	SubmittedAt time.Time
	StartedAt   time.Time
	CompletedAt time.Time
	
	// Result tracking
	Error    error
	Duration time.Duration
}

// Worker represents a single worker in the pool
type Worker struct {
	ID       int
	jobChan  chan *Job
	quitChan chan struct{}
	wg       *sync.WaitGroup
	
	// Worker metrics
	jobsProcessed int64
	totalDuration time.Duration
	lastActivity  time.Time
}

// WorkerPool manages a pool of workers for parallel processing
type WorkerPool struct {
	workers    []*Worker
	jobQueue   chan *Job
	quitChan   chan struct{}
	wg         sync.WaitGroup
	
	// Configuration
	workerCount int
	bufferSize  int
	
	// State management
	running    int32
	stopped    int32
	
	// Metrics
	metricsLock       sync.RWMutex
	totalJobsSubmitted int64
	totalJobsCompleted int64
	totalJobsFailed    int64
	averageLatency     time.Duration
	throughputPerSec   float64
	
	// Performance tracking
	lastMetricsUpdate time.Time
	completedSnapshot int64
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(workerCount, bufferSize int) *WorkerPool {
	if workerCount <= 0 {
		workerCount = 1
	}
	if bufferSize <= 0 {
		bufferSize = 1000
	}

	return &WorkerPool{
		workers:           make([]*Worker, workerCount),
		jobQueue:          make(chan *Job, bufferSize),
		quitChan:          make(chan struct{}),
		workerCount:       workerCount,
		bufferSize:        bufferSize,
		lastMetricsUpdate: time.Now(),
	}
}

// Start initializes and starts all workers
func (wp *WorkerPool) Start(ctx context.Context) error {
	if !atomic.CompareAndSwapInt32(&wp.running, 0, 1) {
		return fmt.Errorf("worker pool is already running")
	}

	// Create and start workers
	for i := 0; i < wp.workerCount; i++ {
		worker := &Worker{
			ID:       i,
			jobChan:  make(chan *Job, 1),
			quitChan: make(chan struct{}),
			wg:       &wp.wg,
		}
		
		wp.workers[i] = worker
		
		// Start worker goroutine
		wp.wg.Add(1)
		go wp.runWorker(ctx, worker)
	}

	// Start job dispatcher
	wp.wg.Add(1)
	go wp.dispatch(ctx)

	// Start metrics collector
	wp.wg.Add(1)
	go wp.collectMetrics(ctx)

	return nil
}

// Stop gracefully shuts down the worker pool
func (wp *WorkerPool) Stop() error {
	if !atomic.CompareAndSwapInt32(&wp.running, 1, 0) {
		return fmt.Errorf("worker pool is not running")
	}

	// Signal all workers to quit
	close(wp.quitChan)
	
	// Wait for all workers to finish
	wp.wg.Wait()
	
	// Close job queue
	close(wp.jobQueue)
	
	atomic.StoreInt32(&wp.stopped, 1)
	return nil
}

// Submit adds a job to the processing queue
func (wp *WorkerPool) Submit(job *Job) error {
	if atomic.LoadInt32(&wp.running) == 0 {
		return fmt.Errorf("worker pool is not running")
	}
	
	if job == nil {
		return fmt.Errorf("job cannot be nil")
	}
	
	if job.Handler == nil {
		return fmt.Errorf("job must have a handler")
	}
	
	// Set submission time
	job.SubmittedAt = time.Now()
	
	// Try to submit job
	select {
	case wp.jobQueue <- job:
		atomic.AddInt64(&wp.totalJobsSubmitted, 1)
		return nil
	default:
		return fmt.Errorf("job queue is full")
	}
}

// SubmitWithTimeout submits a job with a timeout
func (wp *WorkerPool) SubmitWithTimeout(job *Job, timeout time.Duration) error {
	if atomic.LoadInt32(&wp.running) == 0 {
		return fmt.Errorf("worker pool is not running")
	}
	
	if job == nil {
		return fmt.Errorf("job cannot be nil")
	}
	
	job.SubmittedAt = time.Now()
	
	select {
	case wp.jobQueue <- job:
		atomic.AddInt64(&wp.totalJobsSubmitted, 1)
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting to submit job")
	}
}

// GetMetrics returns current worker pool metrics
func (wp *WorkerPool) GetMetrics() WorkerPoolMetrics {
	wp.metricsLock.RLock()
	defer wp.metricsLock.RUnlock()
	
	return WorkerPoolMetrics{
		WorkerCount:        wp.workerCount,
		JobsSubmitted:      atomic.LoadInt64(&wp.totalJobsSubmitted),
		JobsCompleted:      atomic.LoadInt64(&wp.totalJobsCompleted),
		JobsFailed:         atomic.LoadInt64(&wp.totalJobsFailed),
		AverageLatency:     wp.averageLatency,
		ThroughputPerSec:   wp.throughputPerSec,
		QueueSize:          len(wp.jobQueue),
		QueueCapacity:      wp.bufferSize,
		IsRunning:          atomic.LoadInt32(&wp.running) == 1,
	}
}

// WorkerPoolMetrics contains performance metrics for the worker pool
type WorkerPoolMetrics struct {
	WorkerCount      int           `json:"worker_count"`
	JobsSubmitted    int64         `json:"jobs_submitted"`
	JobsCompleted    int64         `json:"jobs_completed"`
	JobsFailed       int64         `json:"jobs_failed"`
	AverageLatency   time.Duration `json:"average_latency"`
	ThroughputPerSec float64       `json:"throughput_per_sec"`
	QueueSize        int           `json:"queue_size"`
	QueueCapacity    int           `json:"queue_capacity"`
	IsRunning        bool          `json:"is_running"`
}

// GetWorkerMetrics returns metrics for individual workers
func (wp *WorkerPool) GetWorkerMetrics() []WorkerMetrics {
	metrics := make([]WorkerMetrics, len(wp.workers))
	
	for i, worker := range wp.workers {
		if worker != nil {
			metrics[i] = WorkerMetrics{
				ID:            worker.ID,
				JobsProcessed: atomic.LoadInt64(&worker.jobsProcessed),
				TotalDuration: worker.totalDuration,
				LastActivity:  worker.lastActivity,
				QueueSize:     len(worker.jobChan),
			}
		}
	}
	
	return metrics
}

// WorkerMetrics contains performance metrics for individual workers
type WorkerMetrics struct {
	ID            int           `json:"id"`
	JobsProcessed int64         `json:"jobs_processed"`
	TotalDuration time.Duration `json:"total_duration"`
	LastActivity  time.Time     `json:"last_activity"`
	QueueSize     int           `json:"queue_size"`
}

// IsRunning returns whether the worker pool is currently running
func (wp *WorkerPool) IsRunning() bool {
	return atomic.LoadInt32(&wp.running) == 1
}

// GetQueueSize returns current number of jobs in queue
func (wp *WorkerPool) GetQueueSize() int {
	return len(wp.jobQueue)
}

// GetCapacity returns the maximum queue capacity
func (wp *WorkerPool) GetCapacity() int {
	return wp.bufferSize
}

// dispatch distributes jobs from the main queue to worker queues
func (wp *WorkerPool) dispatch(ctx context.Context) {
	defer wp.wg.Done()
	
	workerIndex := 0
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-wp.quitChan:
			return
		case job := <-wp.jobQueue:
			if job != nil {
				// Round-robin job distribution
				worker := wp.workers[workerIndex]
				
				select {
				case worker.jobChan <- job:
					workerIndex = (workerIndex + 1) % wp.workerCount
				case <-time.After(100 * time.Millisecond):
					// Worker queue full, try next worker
					workerIndex = (workerIndex + 1) % wp.workerCount
					
					// Try to dispatch to next available worker
					for i := 0; i < wp.workerCount; i++ {
						nextWorker := wp.workers[(workerIndex+i)%wp.workerCount]
						select {
						case nextWorker.jobChan <- job:
							workerIndex = (workerIndex + i + 1) % wp.workerCount
							goto dispatched
						default:
							continue
						}
					}
					
					// All workers busy, increment failed jobs
					atomic.AddInt64(&wp.totalJobsFailed, 1)
					dispatched:
				}
			}
		}
	}
}

// runWorker executes jobs in a worker goroutine
func (wp *WorkerPool) runWorker(ctx context.Context, worker *Worker) {
	defer wp.wg.Done()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-wp.quitChan:
			return
		case job := <-worker.jobChan:
			if job != nil {
				wp.processJob(ctx, worker, job)
			}
		}
	}
}

// processJob executes a single job
func (wp *WorkerPool) processJob(ctx context.Context, worker *Worker, job *Job) {
	// Update worker activity
	worker.lastActivity = time.Now()
	job.StartedAt = time.Now()
	
	// Create job-specific context with timeout
	jobCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	
	// Execute job handler
	err := job.Handler(jobCtx, job)
	
	// Record completion
	job.CompletedAt = time.Now()
	job.Duration = job.CompletedAt.Sub(job.StartedAt)
	job.Error = err
	
	// Update worker metrics
	atomic.AddInt64(&worker.jobsProcessed, 1)
	worker.totalDuration += job.Duration
	
	// Update pool metrics
	if err != nil {
		atomic.AddInt64(&wp.totalJobsFailed, 1)
	} else {
		atomic.AddInt64(&wp.totalJobsCompleted, 1)
	}
}

// collectMetrics periodically calculates performance metrics
func (wp *WorkerPool) collectMetrics(ctx context.Context) {
	defer wp.wg.Done()
	
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-wp.quitChan:
			return
		case <-ticker.C:
			wp.updateMetrics()
		}
	}
}

// updateMetrics calculates and updates performance metrics
func (wp *WorkerPool) updateMetrics() {
	wp.metricsLock.Lock()
	defer wp.metricsLock.Unlock()
	
	now := time.Now()
	duration := now.Sub(wp.lastMetricsUpdate)
	
	if duration > 0 {
		currentCompleted := atomic.LoadInt64(&wp.totalJobsCompleted)
		
		if wp.completedSnapshot > 0 {
			jobsDelta := currentCompleted - wp.completedSnapshot
			wp.throughputPerSec = float64(jobsDelta) / duration.Seconds()
		}
		
		wp.completedSnapshot = currentCompleted
		wp.lastMetricsUpdate = now
		
		// Calculate average latency from worker durations
		totalDuration := time.Duration(0)
		totalJobs := int64(0)
		
		for _, worker := range wp.workers {
			if worker != nil {
				totalDuration += worker.totalDuration
				totalJobs += atomic.LoadInt64(&worker.jobsProcessed)
			}
		}
		
		if totalJobs > 0 {
			wp.averageLatency = totalDuration / time.Duration(totalJobs)
		}
	}
}