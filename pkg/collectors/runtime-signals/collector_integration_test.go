package runtimesignals

import (
	"context"
	"testing"
	"time"
)

func TestDeathIntelligenceIntegration(t *testing.T) {
	// Create collector
	collector, err := NewCollector("test-runtime-signals")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Test scenario: Process receives SIGKILL and exits
	mockEvent := &runtimeEvent{
		Timestamp:  uint64(time.Now().UnixNano()),
		PID:        12345,
		TGID:       12345,
		PPID:       1,
		EventType:  EventTypeSignalGenerate,
		Signal:     uint32(SIGKILL),
		SenderPID:  1,
		Comm:       [16]byte{'t', 'e', 's', 't', '-', 'p', 'r', 'o', 'c'},
		ParentComm: [16]byte{'i', 'n', 'i', 't'},
	}

	ctx := context.Background()

	// Process signal generation event
	collector.processRuntimeEvent(ctx, mockEvent)

	// Small delay to ensure signal is tracked
	time.Sleep(10 * time.Millisecond)

	// Process death event
	exitEvent := &runtimeEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       12345,
		TGID:      12345,
		PPID:      1,
		EventType: EventTypeProcessExit,
		ExitCode:  137, // SIGKILL exit code
		Comm:      [16]byte{'t', 'e', 's', 't', '-', 'p', 'r', 'o', 'c'},
	}

	collector.processRuntimeEvent(ctx, exitEvent)

	// Verify death cause was recorded
	deathCause := collector.signalTracker.GetDeathCause(12345)
	if deathCause == nil {
		t.Fatal("Death cause should be recorded")
	}

	if deathCause.Reason != DeathReasonSignal {
		t.Errorf("Expected death reason %s, got %s", DeathReasonSignal, deathCause.Reason)
	}

	if deathCause.KillerPID != 1 {
		t.Errorf("Expected killer PID 1, got %d", deathCause.KillerPID)
	}

	if deathCause.SignalName != "SIGKILL" {
		t.Errorf("Expected signal name SIGKILL, got %s", deathCause.SignalName)
	}

	t.Logf("Death intelligence successfully correlated: PID %d killed by PID %d (%s) with %s - reason: %s",
		deathCause.PID, deathCause.KillerPID, deathCause.KillerComm, deathCause.SignalName, deathCause.Reason)
}

func TestOOMKillScenario(t *testing.T) {
	collector, err := NewCollector("test-oom-killer")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	ctx := context.Background()

	// OOM kill event
	oomEvent := &runtimeEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       9999,
		TGID:      9999,
		PPID:      1,
		EventType: EventTypeOOMKill,
		Signal:    uint32(SIGKILL),
		SenderPID: 0, // OOM killer
		Comm:      [16]byte{'m', 'e', 'm', 'o', 'r', 'y', '-', 'h', 'o', 'g'},
	}

	collector.processRuntimeEvent(ctx, oomEvent)

	// Process death
	exitEvent := &runtimeEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       9999,
		TGID:      9999,
		PPID:      1,
		EventType: EventTypeProcessExit,
		ExitCode:  137,
		Comm:      [16]byte{'m', 'e', 'm', 'o', 'r', 'y', '-', 'h', 'o', 'g'},
	}

	collector.processRuntimeEvent(ctx, exitEvent)

	// Verify OOM correlation
	deathCause := collector.signalTracker.GetDeathCause(9999)
	if deathCause == nil {
		t.Fatal("OOM death cause should be recorded")
	}

	if deathCause.KillerComm != "oom_killer" {
		t.Errorf("Expected OOM killer, got %s", deathCause.KillerComm)
	}

	t.Logf("OOM kill successfully detected: PID %d killed by %s - reason: %s",
		deathCause.PID, deathCause.KillerComm, deathCause.Reason)
}

func TestNormalExitNoCorrelation(t *testing.T) {
	collector, err := NewCollector("test-normal-exit")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	ctx := context.Background()

	// Normal process exit (code 0)
	exitEvent := &runtimeEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       7777,
		TGID:      7777,
		PPID:      1,
		EventType: EventTypeProcessExit,
		ExitCode:  0, // Success
		Comm:      [16]byte{'n', 'o', 'r', 'm', 'a', 'l', '-', 'a', 'p', 'p'},
	}

	collector.processRuntimeEvent(ctx, exitEvent)

	// Verify normal exit classification
	deathCause := collector.signalTracker.GetDeathCause(7777)
	if deathCause == nil {
		t.Fatal("Death cause should be recorded even for normal exits")
	}

	if deathCause.Reason != DeathReasonExit {
		t.Errorf("Expected death reason %s for normal exit, got %s", DeathReasonExit, deathCause.Reason)
	}

	if deathCause.KillerPID != 0 {
		t.Errorf("Expected no killer for normal exit, got PID %d", deathCause.KillerPID)
	}

	t.Logf("Normal exit properly classified: PID %d exited normally - reason: %s",
		deathCause.PID, deathCause.Reason)
}