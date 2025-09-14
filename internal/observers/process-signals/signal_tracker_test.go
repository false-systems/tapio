package processsignals

import (
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

func TestSignalCorrelation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewSignalTracker(logger)

	// Simulate a signal being sent to a process
	pid := uint32(12345)
	signal := &TrackedSignal{
		Timestamp:  time.Now(),
		Signal:     SIGKILL,
		SignalName: "SIGKILL",
		SenderPID:  uint32(1),
		SenderComm: "init",
		IsFatal:    true,
	}

	// Track the signal
	tracker.TrackSignal(pid, signal)

	// Simulate process exit with matching signal
	exitInfo := &ExitInfo{
		Signal:     SIGKILL,
		Code:       137, // 128 + 9 (SIGKILL)
		CoreDumped: false,
	}

	// Correlate the death
	deathCause := tracker.CorrelateProcessDeath(pid, 137, exitInfo)

	// Verify correlation worked
	if deathCause.Reason != DeathReasonSignal {
		t.Errorf("Expected death reason %s, got %s", DeathReasonSignal, deathCause.Reason)
	}

	if deathCause.KillerPID != 1 {
		t.Errorf("Expected killer PID 1, got %d", deathCause.KillerPID)
	}

	if deathCause.KillerComm != "init" {
		t.Errorf("Expected killer command 'init', got %s", deathCause.KillerComm)
	}

	if deathCause.SignalName != "SIGKILL" {
		t.Errorf("Expected signal name 'SIGKILL', got %s", deathCause.SignalName)
	}
}

func TestOOMKillCorrelation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewSignalTracker(logger)

	// Simulate OOM kill
	victimPID := uint32(5432)
	killerPID := uint32(0) // oom_killer

	tracker.TrackOOMKill(victimPID, killerPID)

	// Simulate process exit
	exitInfo := &ExitInfo{
		Signal:     SIGKILL,
		Code:       137,
		CoreDumped: false,
	}

	// Correlate the death
	deathCause := tracker.CorrelateProcessDeath(victimPID, 137, exitInfo)

	// Verify OOM correlation
	if deathCause.Reason != DeathReasonSignal {
		t.Errorf("Expected death reason %s for OOM kill, got %s", DeathReasonSignal, deathCause.Reason)
	}

	if deathCause.KillerPID != 0 {
		t.Errorf("Expected OOM killer PID 0, got %d", deathCause.KillerPID)
	}

	if deathCause.KillerComm != "oom_killer" {
		t.Errorf("Expected killer command 'oom_killer', got %s", deathCause.KillerComm)
	}
}

func TestSignalWindow(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewSignalTracker(logger)

	pid := uint32(9876)

	// Track a signal that's too old
	oldSignal := &TrackedSignal{
		Timestamp:  time.Now().Add(-60 * time.Second), // 60 seconds ago
		Signal:     SIGTERM,
		SignalName: "SIGTERM",
		SenderPID:  uint32(1),
		SenderComm: "init",
		IsFatal:    true,
	}
	tracker.TrackSignal(pid, oldSignal)

	// Process exit now
	exitInfo := &ExitInfo{
		Signal:     SIGTERM,
		Code:       143,
		CoreDumped: false,
	}

	deathCause := tracker.CorrelateProcessDeath(pid, 143, exitInfo)

	// Should not correlate with old signal (outside 30s window)
	if deathCause.KillerPID != 0 {
		t.Errorf("Expected no correlation for old signal, but got killer PID %d", deathCause.KillerPID)
	}
}

func TestTrackerStats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracker := NewSignalTracker(logger)

	// Add some signals
	pid1 := uint32(1111)
	pid2 := uint32(2222)

	signal1 := &TrackedSignal{
		Timestamp:  time.Now(),
		Signal:     SIGTERM,
		SignalName: "SIGTERM",
		SenderPID:  1,
		SenderComm: "init",
		IsFatal:    true,
	}

	signal2 := &TrackedSignal{
		Timestamp:  time.Now(),
		Signal:     SIGKILL,
		SignalName: "SIGKILL",
		SenderPID:  2,
		SenderComm: "killer",
		IsFatal:    true,
	}

	tracker.TrackSignal(pid1, signal1)
	tracker.TrackSignal(pid2, signal2)

	// Check stats
	trackedPIDs, totalSignals, deathsCached := tracker.GetStats()

	if trackedPIDs != 2 {
		t.Errorf("Expected 2 tracked PIDs, got %d", trackedPIDs)
	}

	if totalSignals != 2 {
		t.Errorf("Expected 2 total signals, got %d", totalSignals)
	}

	if deathsCached != 0 {
		t.Errorf("Expected 0 cached deaths, got %d", deathsCached)
	}
}
