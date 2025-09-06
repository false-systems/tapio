package processsignals

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// SignalTracker tracks signals and correlates them with process deaths
type SignalTracker struct {
	mu     sync.RWMutex
	logger *zap.Logger

	// Track recent signals by PID
	// pid -> list of recent signals
	recentSignals map[uint32][]*TrackedSignal

	// Track process deaths and their causes
	// pid -> death info
	deathCauses map[uint32]*DeathCause

	// Configuration
	signalWindow  time.Duration // How long to keep signals
	cleanupPeriod time.Duration // How often to clean old entries
}

// TrackedSignal represents a signal we're tracking
type TrackedSignal struct {
	Timestamp  time.Time
	Signal     int
	SignalName string
	SenderPID  uint32
	SenderComm string
	IsFatal    bool
}

// DeathCause represents why a process died
type DeathCause struct {
	Timestamp  time.Time
	PID        uint32
	ExitCode   int
	Signal     int
	SignalName string
	KillerPID  uint32
	KillerComm string
	Reason     DeathReason
	OOMKill    bool
	CoreDumped bool
}

// DeathReason categorizes why a process died
type DeathReason string

const (
	DeathReasonSignal   DeathReason = "signal"
	DeathReasonOOM      DeathReason = "oom_kill"
	DeathReasonExit     DeathReason = "normal_exit"
	DeathReasonCrash    DeathReason = "crash"
	DeathReasonSegfault DeathReason = "segmentation_fault"
	DeathReasonAbort    DeathReason = "abort"
	DeathReasonUnknown  DeathReason = "unknown"
)

// NewSignalTracker creates a new signal tracker
func NewSignalTracker(logger *zap.Logger) *SignalTracker {
	return &SignalTracker{
		logger:        logger,
		recentSignals: make(map[uint32][]*TrackedSignal),
		deathCauses:   make(map[uint32]*DeathCause),
		signalWindow:  30 * time.Second, // Track signals for 30 seconds
		cleanupPeriod: 60 * time.Second, // Clean up every minute
	}
}

// TrackSignal records a signal being sent to a process
func (st *SignalTracker) TrackSignal(pid uint32, signal *TrackedSignal) {
	st.mu.Lock()
	defer st.mu.Unlock()

	// Initialize slice if needed
	if st.recentSignals[pid] == nil {
		st.recentSignals[pid] = make([]*TrackedSignal, 0, 10)
	}

	// Add signal to tracking
	st.recentSignals[pid] = append(st.recentSignals[pid], signal)

	// Keep only last 10 signals per process
	if len(st.recentSignals[pid]) > 10 {
		st.recentSignals[pid] = st.recentSignals[pid][1:]
	}

	st.logger.Debug("Tracked signal",
		zap.Uint32("pid", pid),
		zap.String("signal", signal.SignalName),
		zap.Uint32("sender", signal.SenderPID),
		zap.Bool("fatal", signal.IsFatal))
}

// CorrelateProcessDeath correlates a process exit with recent signals
func (st *SignalTracker) CorrelateProcessDeath(pid uint32, exitCode int, exitInfo *ExitInfo) *DeathCause {
	st.mu.Lock()
	defer st.mu.Unlock()

	death := &DeathCause{
		Timestamp:  time.Now(),
		PID:        pid,
		ExitCode:   exitCode,
		Signal:     exitInfo.Signal,
		SignalName: GetSignalName(exitInfo.Signal),
		Reason:     st.determineDeathReason(exitInfo),
		CoreDumped: exitInfo.CoreDumped,
	}

	// Check for recent signals to this process
	if signals, ok := st.recentSignals[pid]; ok && len(signals) > 0 {
		// Find the most recent fatal signal
		for i := len(signals) - 1; i >= 0; i-- {
			sig := signals[i]
			if sig.IsFatal && time.Since(sig.Timestamp) < st.signalWindow {
				death.Signal = sig.Signal
				death.SignalName = sig.SignalName
				death.KillerPID = sig.SenderPID
				death.KillerComm = sig.SenderComm
				death.Reason = st.getSignalDeathReason(sig.Signal)

				st.logger.Info("Correlated process death with signal",
					zap.Uint32("pid", pid),
					zap.String("signal", sig.SignalName),
					zap.Uint32("killer_pid", sig.SenderPID),
					zap.String("killer_comm", sig.SenderComm),
					zap.String("reason", string(death.Reason)))
				break
			}
		}
	}

	// Store death cause for potential queries
	st.deathCauses[pid] = death

	// Clean up signal tracking for this PID
	delete(st.recentSignals, pid)

	return death
}

// determineDeathReason figures out why a process died based on exit info
func (st *SignalTracker) determineDeathReason(exitInfo *ExitInfo) DeathReason {
	if exitInfo.Signal > 0 {
		return st.getSignalDeathReason(exitInfo.Signal)
	}
	if exitInfo.Code == 0 {
		return DeathReasonExit
	}
	// Non-zero exit without signal usually means error exit
	return DeathReasonUnknown
}

// getSignalDeathReason determines death reason from signal number
func (st *SignalTracker) getSignalDeathReason(signal int) DeathReason {
	switch signal {
	case SIGKILL:
		return DeathReasonSignal
	case SIGSEGV:
		return DeathReasonSegfault
	case SIGABRT:
		return DeathReasonAbort
	case SIGBUS:
		return DeathReasonCrash
	case SIGTERM, SIGINT, SIGQUIT:
		return DeathReasonSignal
	default:
		return DeathReasonSignal
	}
}

// TrackOOMKill records an OOM kill event
func (st *SignalTracker) TrackOOMKill(pid, killerPID uint32) {
	st.mu.Lock()
	defer st.mu.Unlock()

	// OOM kills are always SIGKILL
	signal := &TrackedSignal{
		Timestamp:  time.Now(),
		Signal:     SIGKILL,
		SignalName: "SIGKILL (OOM)",
		SenderPID:  killerPID,
		SenderComm: "oom_killer",
		IsFatal:    true,
	}

	// Track as regular signal
	if st.recentSignals[pid] == nil {
		st.recentSignals[pid] = make([]*TrackedSignal, 0, 10)
	}
	st.recentSignals[pid] = append(st.recentSignals[pid], signal)

	st.logger.Warn("OOM Kill tracked",
		zap.Uint32("victim_pid", pid),
		zap.Uint32("killer_pid", killerPID))
}

// GetDeathCause returns the death cause for a PID if known
func (st *SignalTracker) GetDeathCause(pid uint32) *DeathCause {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.deathCauses[pid]
}

// CleanupLoop periodically cleans old entries
func (st *SignalTracker) CleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(st.cleanupPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			st.cleanup()
		}
	}
}

// cleanup removes old signal and death entries
func (st *SignalTracker) cleanup() {
	st.mu.Lock()
	defer st.mu.Unlock()

	now := time.Now()
	cleanedSignals := 0
	cleanedDeaths := 0

	// Clean old signals
	for pid, signals := range st.recentSignals {
		// Keep only recent signals
		recent := make([]*TrackedSignal, 0, len(signals))
		for _, sig := range signals {
			if now.Sub(sig.Timestamp) < st.signalWindow {
				recent = append(recent, sig)
			}
		}
		if len(recent) == 0 {
			delete(st.recentSignals, pid)
			cleanedSignals++
		} else {
			st.recentSignals[pid] = recent
		}
	}

	// Clean old death causes (keep for 5 minutes)
	for pid, death := range st.deathCauses {
		if now.Sub(death.Timestamp) > 5*time.Minute {
			delete(st.deathCauses, pid)
			cleanedDeaths++
		}
	}

	if cleanedSignals > 0 || cleanedDeaths > 0 {
		st.logger.Debug("Cleaned signal tracker",
			zap.Int("signals_cleaned", cleanedSignals),
			zap.Int("deaths_cleaned", cleanedDeaths))
	}
}

// GetStats returns statistics about the tracker
func (st *SignalTracker) GetStats() (trackedPIDs int, totalSignals int, deathsCached int) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	trackedPIDs = len(st.recentSignals)
	for _, signals := range st.recentSignals {
		totalSignals += len(signals)
	}
	deathsCached = len(st.deathCauses)

	return
}
