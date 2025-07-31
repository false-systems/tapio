package systemd

import (
	"testing"

	"github.com/yairfalse/tapio/pkg/collectors"
)

func TestNewCollector(t *testing.T) {
	collector, err := NewCollector("test")
	if err != nil {
		t.Fatalf("NewCollector failed: %v", err)
	}
	if collector.Name() != "test" {
		t.Errorf("Expected name 'test', got %s", collector.Name())
	}
}

func TestCollectorInterface(t *testing.T) {
	collector, err := NewCollector("interface-test")
	if err != nil {
		t.Fatalf("NewCollector failed: %v", err)
	}

	// Verify it implements collectors.Collector
	var _ collectors.Collector = collector
}

func TestNullTerminatedString(t *testing.T) {
	collector, _ := NewCollector("test")

	tests := []struct {
		input    []byte
		expected string
	}{
		{[]byte("hello\x00world"), "hello"},
		{[]byte("systemd\x00\x00\x00"), "systemd"},
		{[]byte("test"), "test"},
		{[]byte("\x00"), ""},
	}

	for _, test := range tests {
		result := collector.nullTerminatedString(test.input)
		if result != test.expected {
			t.Errorf("nullTerminatedString(%v) = %s, want %s", test.input, result, test.expected)
		}
	}
}
