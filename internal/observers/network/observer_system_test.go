//go:build linux

package network

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
)

func TestNetworkObserver_TCPCapture(t *testing.T) {
	setupOTEL(t)

	config := Config{
		Output: base.OutputConfig{Stdout: true},
	}

	observer, err := NewNetworkObserver("test-tcp", config)
	require.NoError(t, err)

	// Load eBPF program
	ctx := context.Background()
	err = observer.loadeBPF(ctx)
	require.NoError(t, err)
	defer observer.ebpfManager.Close()

	// Attach TCP kprobe
	err = observer.attachTCPProbe()
	require.NoError(t, err, "TCP kprobe should attach successfully")

	// Start observer
	go func() {
		_ = observer.Start(ctx)
	}()
	time.Sleep(10 * time.Millisecond)

	// Make a TCP connection to trigger event
	conn, err := net.Dial("tcp", "google.com:80")
	if err == nil {
		conn.Close()
	}

	// Give eBPF time to capture event
	time.Sleep(100 * time.Millisecond)

	// Verify observer is healthy
	assert.True(t, observer.IsHealthy())
}
