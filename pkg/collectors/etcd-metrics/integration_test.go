//go:build integration
// +build integration

package etcdmetrics

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/tests/v3/integration"
)

func TestIntegration_FullLifecycle(t *testing.T) {
	integration.BeforeTestExternal(t)
	cluster := integration.NewClusterV3(t, &integration.ClusterConfig{
		Size: 3,
	})
	defer cluster.Terminate(t)

	cfg := Config{
		Name:                  "integration-etcd",
		BufferSize:            1000,
		Endpoints:             getClusterEndpoints(cluster),
		DialTimeout:           5 * time.Second,
		RequestTimeout:        2 * time.Second,
		HealthCheckInterval:   100 * time.Millisecond,
		ResponseTimeThreshold: 50 * time.Millisecond,
		DbSizeThreshold:       100 * 1024 * 1024, // 100MB
	}

	collector, err := NewCollector("integration-etcd", cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Create client for operations
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: cfg.DialTimeout,
	})
	require.NoError(t, err)
	defer client.Close()

	// Perform various operations to generate events
	t.Run("Normal_Operations", func(t *testing.T) {
		// Put some keys
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("test-key-%d", i)
			value := fmt.Sprintf("test-value-%d", i)
			_, err := client.Put(ctx, key, value)
			require.NoError(t, err)
		}

		// Read keys
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("test-key-%d", i)
			resp, err := client.Get(ctx, key)
			require.NoError(t, err)
			assert.Equal(t, 1, len(resp.Kvs))
		}

		// Delete keys
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("test-key-%d", i)
			_, err := client.Delete(ctx, key)
			require.NoError(t, err)
		}
	})

	t.Run("Watch_Operations", func(t *testing.T) {
		watchCh := client.Watch(ctx, "watch-", clientv3.WithPrefix())

		// Trigger watch events
		for i := 0; i < 5; i++ {
			key := fmt.Sprintf("watch-key-%d", i)
			_, err := client.Put(ctx, key, "value")
			require.NoError(t, err)
		}

		// Consume watch events
		eventCount := 0
		done := time.After(2 * time.Second)
	watchLoop:
		for {
			select {
			case resp := <-watchCh:
				eventCount += len(resp.Events)
				if eventCount >= 5 {
					break watchLoop
				}
			case <-done:
				break watchLoop
			}
		}
		assert.GreaterOrEqual(t, eventCount, 5)
	})

	t.Run("Transaction_Operations", func(t *testing.T) {
		// Successful transaction
		_, err := client.Txn(ctx).
			If(clientv3.Compare(clientv3.Version("txn-key"), "=", 0)).
			Then(clientv3.OpPut("txn-key", "initial")).
			Else(clientv3.OpPut("txn-key", "updated")).
			Commit()
		require.NoError(t, err)

		// Failed transaction condition
		_, err = client.Txn(ctx).
			If(clientv3.Compare(clientv3.Value("txn-key"), "=", "wrong")).
			Then(clientv3.OpPut("txn-key", "should-not-happen")).
			Else(clientv3.OpPut("txn-key", "fallback")).
			Commit()
		require.NoError(t, err)
	})

	// Wait for health checks to process
	time.Sleep(500 * time.Millisecond)

	// Verify collector is healthy
	assert.True(t, collector.IsHealthy())

	// Check for events
	eventReceived := false
	timeout := time.After(2 * time.Second)
eventLoop:
	for {
		select {
		case event := <-collector.Events():
			if event != nil && event.Type == domain.EventTypeETCD {
				eventReceived = true
				// Validate event structure
				assert.NotEmpty(t, event.EventID)
				assert.NotZero(t, event.Timestamp)
				assert.Equal(t, "integration-etcd", event.Source)
				assert.NotNil(t, event.EventData.ETCD)
			}
		case <-timeout:
			break eventLoop
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// We should have received at least one event (health check or monitoring)
	assert.True(t, eventReceived, "Should have received at least one event")
}

func TestIntegration_ClusterFailover(t *testing.T) {
	integration.BeforeTestExternal(t)
	cluster := integration.NewClusterV3(t, &integration.ClusterConfig{
		Size: 3,
	})
	defer cluster.Terminate(t)

	cfg := Config{
		Name:                  "failover-etcd",
		BufferSize:            1000,
		Endpoints:             getClusterEndpoints(cluster),
		HealthCheckInterval:   100 * time.Millisecond,
		ResponseTimeThreshold: 50 * time.Millisecond,
	}

	collector, err := NewCollector("failover-etcd", cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Get initial leader
	var initialLeader uint64
	for _, endpoint := range cfg.Endpoints {
		status, err := collector.client.Status(ctx, endpoint)
		if err == nil && status.Leader != 0 {
			initialLeader = status.Leader
			collector.lastLeaderID = initialLeader
			break
		}
	}
	require.NotZero(t, initialLeader, "Should have found initial leader")

	// Stop the leader member
	for _, member := range cluster.Members {
		if member.Server.ID() == initialLeader {
			member.Stop(t)
			break
		}
	}

	// Wait for new leader election
	time.Sleep(2 * time.Second)

	// Trigger health check
	collector.performHealthCheck()

	// Should receive leader change event
	leaderChangeDetected := false
	timeout := time.After(5 * time.Second)
leaderLoop:
	for {
		select {
		case event := <-collector.Events():
			if event != nil && event.EventData.ETCD != nil &&
				event.EventData.ETCD.Operation == "leader_change" {
				leaderChangeDetected = true
				assert.Equal(t, domain.EventSeverityCritical, event.Severity)
				assert.Contains(t, event.EventData.Custom["old_leader"], fmt.Sprintf("%x", initialLeader))
				break leaderLoop
			}
		case <-timeout:
			break leaderLoop
		}
	}

	assert.True(t, leaderChangeDetected, "Should have detected leader change")
}

func TestIntegration_HighLoad(t *testing.T) {
	integration.BeforeTestExternal(t)
	cluster := integration.NewClusterV3(t, &integration.ClusterConfig{
		Size: 1,
	})
	defer cluster.Terminate(t)

	cfg := Config{
		Name:                  "highload-etcd",
		BufferSize:            5000,
		Endpoints:             []string{cluster.Members[0].GRPCAddr()},
		HealthCheckInterval:   500 * time.Millisecond,
		ResponseTimeThreshold: 100 * time.Millisecond,
		DbSizeThreshold:       50 * 1024 * 1024, // 50MB
	}

	collector, err := NewCollector("highload-etcd", cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Create client for load generation
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	defer client.Close()

	// Generate high load
	const numGoroutines = 10
	const opsPerGoroutine = 100
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(workerID int) {
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("worker-%d-key-%d", workerID, j)
				value := fmt.Sprintf("value-%d-%d-%d", workerID, j, time.Now().UnixNano())

				// Mix of operations
				switch j % 3 {
				case 0:
					client.Put(ctx, key, value)
				case 1:
					client.Get(ctx, key)
				case 2:
					client.Delete(ctx, key)
				}
			}
			done <- true
		}(i)
	}

	// Wait for all workers
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Let health checks run
	time.Sleep(1 * time.Second)

	// Collector should still be healthy
	assert.True(t, collector.IsHealthy())

	// Check for slow response events under load
	slowResponseDetected := false
	timeout := time.After(2 * time.Second)
loadLoop:
	for {
		select {
		case event := <-collector.Events():
			if event != nil && event.EventData.ETCD != nil &&
				event.EventData.ETCD.Operation == "slow_response" {
				slowResponseDetected = true
				assert.Equal(t, domain.EventSeverityWarning, event.Severity)
			}
		case <-timeout:
			break loadLoop
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Under high load, we might detect slow responses
	t.Logf("Slow response detected under load: %v", slowResponseDetected)
}

func TestIntegration_Authentication(t *testing.T) {
	t.Skip("Requires etcd with auth enabled - manual test")

	// This test would require setting up etcd with authentication
	// which is complex in integration tests. For production:
	//
	// 1. Enable auth: etcdctl auth enable
	// 2. Create user: etcdctl user add test-user
	// 3. Grant role: etcdctl user grant-role test-user root
	//
	// Then test with:
	cfg := Config{
		Name:                "auth-etcd",
		BufferSize:          100,
		Endpoints:           []string{"localhost:2379"},
		Username:            "test-user",
		Password:            "test-password",
		HealthCheckInterval: 1 * time.Second,
	}

	collector, err := NewCollector("auth-etcd", cfg)
	if err == nil {
		ctx := context.Background()
		err = collector.Start(ctx)
		if err == nil {
			defer collector.Stop()
			assert.True(t, collector.IsHealthy())
		}
	}
}

func TestIntegration_NetworkPartition(t *testing.T) {
	integration.BeforeTestExternal(t)
	cluster := integration.NewClusterV3(t, &integration.ClusterConfig{
		Size: 3,
	})
	defer cluster.Terminate(t)

	// Use only first endpoint to simulate partial connectivity
	cfg := Config{
		Name:                "partition-etcd",
		BufferSize:          100,
		Endpoints:           []string{cluster.Members[0].GRPCAddr()},
		HealthCheckInterval: 100 * time.Millisecond,
	}

	collector, err := NewCollector("partition-etcd", cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Initially healthy
	assert.True(t, collector.IsHealthy())

	// Simulate network partition by stopping the member we're connected to
	cluster.Members[0].Stop(t)

	// Wait for health check to detect failure
	time.Sleep(500 * time.Millisecond)

	// Should be unhealthy now
	assert.False(t, collector.IsHealthy())

	// Should receive error events
	errorReceived := false
	timeout := time.After(2 * time.Second)
partitionLoop:
	for {
		select {
		case event := <-collector.Events():
			if event != nil && event.Severity == domain.EventSeverityError {
				errorReceived = true
				assert.Equal(t, domain.EventTypeETCD, event.Type)
				assert.Contains(t, event.EventData.ETCD.Value, "connectivity check failed")
				break partitionLoop
			}
		case <-timeout:
			break partitionLoop
		}
	}

	assert.True(t, errorReceived, "Should have received error event for network partition")
}

// Helper function to get all cluster endpoints
func getClusterEndpoints(cluster *integration.ClusterV3) []string {
	endpoints := make([]string, 0, len(cluster.Members))
	for _, member := range cluster.Members {
		endpoints = append(endpoints, member.GRPCAddr())
	}
	return endpoints
}
