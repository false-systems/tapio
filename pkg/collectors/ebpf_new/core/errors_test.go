package core

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestValidationError(t *testing.T) {
	tests := []struct {
		name     string
		err      ValidationError
		wantMsg  string
	}{
		{
			name: "with_value",
			err: ValidationError{
				Field:   "timeout",
				Value:   5,
				Message: "must be greater than 10",
			},
			wantMsg: "validation failed for field timeout (value: 5): must be greater than 10",
		},
		{
			name: "without_value",
			err: ValidationError{
				Field:   "name",
				Message: "is required",
			},
			wantMsg: "validation failed for field name: is required",
		},
		{
			name: "with_nil_value",
			err: ValidationError{
				Field:   "data",
				Value:   nil,
				Message: "cannot be nil",
			},
			wantMsg: "validation failed for field data: cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.wantMsg {
				t.Errorf("ValidationError.Error() = %v, want %v", got, tt.wantMsg)
			}
		})
	}
}

func TestProgramLoadError(t *testing.T) {
	baseErr := errors.New("kernel version too old")
	err := ProgramLoadError{
		ProgramName: "syscall_monitor",
		ProgramType: ProgramTypeKprobe,
		Cause:       baseErr,
	}

	expectedMsg := "failed to load eBPF program syscall_monitor (type: kprobe): kernel version too old"
	if err.Error() != expectedMsg {
		t.Errorf("ProgramLoadError.Error() = %v, want %v", err.Error(), expectedMsg)
	}

	// Test unwrap
	if errors.Unwrap(err) != baseErr {
		t.Errorf("ProgramLoadError.Unwrap() = %v, want %v", errors.Unwrap(err), baseErr)
	}
}

func TestAttachError(t *testing.T) {
	baseErr := errors.New("function not found")
	err := AttachError{
		ProgramName:  "network_monitor",
		AttachTarget: "tcp_v4_connect",
		Cause:        baseErr,
	}

	expectedMsg := "failed to attach eBPF program network_monitor to tcp_v4_connect: function not found"
	if err.Error() != expectedMsg {
		t.Errorf("AttachError.Error() = %v, want %v", err.Error(), expectedMsg)
	}

	// Test unwrap
	if errors.Unwrap(err) != baseErr {
		t.Errorf("AttachError.Unwrap() = %v, want %v", errors.Unwrap(err), baseErr)
	}
}

func TestMapError(t *testing.T) {
	baseErr := errors.New("out of memory")
	err := MapError{
		MapName:   "event_buffer",
		Operation: "create",
		Cause:     baseErr,
	}

	expectedMsg := "eBPF map error on event_buffer (operation: create): out of memory"
	if err.Error() != expectedMsg {
		t.Errorf("MapError.Error() = %v, want %v", err.Error(), expectedMsg)
	}

	// Test unwrap
	if errors.Unwrap(err) != baseErr {
		t.Errorf("MapError.Unwrap() = %v, want %v", errors.Unwrap(err), baseErr)
	}
}

func TestRingBufferError(t *testing.T) {
	tests := []struct {
		name     string
		err      RingBufferError
		wantMsg  string
	}{
		{
			name: "with_lost_events",
			err: RingBufferError{
				Operation: "read",
				Lost:      42,
				Cause:     errors.New("buffer full"),
			},
			wantMsg: "ring buffer error during read (lost 42 events): buffer full",
		},
		{
			name: "without_lost_events",
			err: RingBufferError{
				Operation: "create",
				Lost:      0,
				Cause:     errors.New("invalid size"),
			},
			wantMsg: "ring buffer error during create: invalid size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.wantMsg {
				t.Errorf("RingBufferError.Error() = %v, want %v", got, tt.wantMsg)
			}

			// Test unwrap
			if errors.Unwrap(tt.err) != tt.err.Cause {
				t.Errorf("RingBufferError.Unwrap() = %v, want %v", errors.Unwrap(tt.err), tt.err.Cause)
			}
		})
	}
}

func TestParseError(t *testing.T) {
	err := ParseError{
		EventType: EventTypeSyscall,
		DataSize:  128,
		Cause:     errors.New("invalid format"),
	}

	expectedMsg := "failed to parse eBPF event (type: syscall, size: 128 bytes): invalid format"
	if err.Error() != expectedMsg {
		t.Errorf("ParseError.Error() = %v, want %v", err.Error(), expectedMsg)
	}

	// Test unwrap
	if errors.Unwrap(err) != err.Cause {
		t.Errorf("ParseError.Unwrap() = %v, want %v", errors.Unwrap(err), err.Cause)
	}
}

func TestPermissionError(t *testing.T) {
	err := PermissionError{
		Operation:   "load BPF program",
		Requirement: "CAP_BPF capability or root privileges",
	}

	expectedMsg := "insufficient privileges for load BPF program: CAP_BPF capability or root privileges"
	if err.Error() != expectedMsg {
		t.Errorf("PermissionError.Error() = %v, want %v", err.Error(), expectedMsg)
	}
}

func TestRateLimitError(t *testing.T) {
	err := RateLimitError{
		Limit:      1000,
		Window:     time.Second,
		RetryAfter: 500 * time.Millisecond,
	}

	expectedMsg := "rate limit exceeded: 1000 events per 1s (retry after 500ms)"
	if err.Error() != expectedMsg {
		t.Errorf("RateLimitError.Error() = %v, want %v", err.Error(), expectedMsg)
	}
}

func TestTimeoutError(t *testing.T) {
	err := TimeoutError{
		Operation: "program load",
		Timeout:   30 * time.Second,
	}

	expectedMsg := "operation program load timed out after 30s"
	if err.Error() != expectedMsg {
		t.Errorf("TimeoutError.Error() = %v, want %v", err.Error(), expectedMsg)
	}
}

func TestNotSupportedError(t *testing.T) {
	tests := []struct {
		name     string
		err      NotSupportedError
		wantMsg  string
	}{
		{
			name: "with_reason",
			err: NotSupportedError{
				Feature:  "eBPF ring buffer",
				Platform: "darwin",
				Reason:   "eBPF is Linux-specific",
			},
			wantMsg: "eBPF ring buffer is not supported on darwin: eBPF is Linux-specific",
		},
		{
			name: "without_reason",
			err: NotSupportedError{
				Feature:  "BPF_PROG_TYPE_LSM",
				Platform: "linux-4.19",
			},
			wantMsg: "BPF_PROG_TYPE_LSM is not supported on linux-4.19",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.wantMsg {
				t.Errorf("NotSupportedError.Error() = %v, want %v", got, tt.wantMsg)
			}
		})
	}
}

func TestCollectorClosedError(t *testing.T) {
	err := CollectorClosedError{
		Operation: "subscribe",
	}

	expectedMsg := "cannot perform subscribe: collector is closed"
	if err.Error() != expectedMsg {
		t.Errorf("CollectorClosedError.Error() = %v, want %v", err.Error(), expectedMsg)
	}
}

func TestInvalidEventError(t *testing.T) {
	err := InvalidEventError{
		Reason: "missing timestamp",
	}

	expectedMsg := "invalid event: missing timestamp"
	if err.Error() != expectedMsg {
		t.Errorf("InvalidEventError.Error() = %v, want %v", err.Error(), expectedMsg)
	}
}

func TestErrorTypes(t *testing.T) {
	// Test that all error types implement the error interface
	var _ error = ValidationError{}
	var _ error = ProgramLoadError{}
	var _ error = AttachError{}
	var _ error = MapError{}
	var _ error = RingBufferError{}
	var _ error = ParseError{}
	var _ error = PermissionError{}
	var _ error = RateLimitError{}
	var _ error = TimeoutError{}
	var _ error = NotSupportedError{}
	var _ error = CollectorClosedError{}
	var _ error = InvalidEventError{}

	// Test that wrapped errors implement Unwrap
	var _ interface{ Unwrap() error } = ProgramLoadError{}
	var _ interface{ Unwrap() error } = AttachError{}
	var _ interface{ Unwrap() error } = MapError{}
	var _ interface{ Unwrap() error } = RingBufferError{}
	var _ interface{ Unwrap() error } = ParseError{}
}

func TestErrorFormatting(t *testing.T) {
	// Test that errors can be used with fmt.Errorf
	err := ValidationError{Field: "test", Message: "failed"}
	wrapped := fmt.Errorf("wrapped: %w", err)
	
	if !errors.Is(wrapped, err) {
		t.Errorf("errors.Is(wrapped, err) = false, want true")
	}
}