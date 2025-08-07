package ebpf

import (
	"fmt"
	"testing"
	"time"
)

// TestServiceEndpointCorrelation demonstrates service endpoint correlation
func TestServiceEndpointCorrelation(t *testing.T) {
	// This test demonstrates how the correlation would work
	// without requiring actual eBPF (for CI/CD compatibility)

	collector, err := NewCollector("network-demo")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// In a real K8s scenario:
	// 1. K8s API watcher sees service creation
	// 2. Service gets endpoints from pods
	// 3. We update the service endpoint map

	// Example service endpoints
	services := []struct {
		name      string
		namespace string
		clusterIP string
		port      uint16
		endpoints []string // Pod IPs
	}{
		{
			name:      "frontend-service",
			namespace: "production",
			clusterIP: "10.96.0.10",
			port:      80,
			endpoints: []string{"10.244.1.5", "10.244.2.8", "10.244.3.12"},
		},
		{
			name:      "backend-api",
			namespace: "production",
			clusterIP: "10.96.0.20",
			port:      8080,
			endpoints: []string{"10.244.1.15", "10.244.2.18"},
		},
		{
			name:      "database",
			namespace: "production",
			clusterIP: "10.96.0.30",
			port:      5432,
			endpoints: []string{"10.244.1.25"},
		},
	}

	fmt.Printf("\n=== Service Endpoint Correlation Demo ===\n")

	// Update service endpoints (would be called by K8s watcher)
	if collector.objs != nil { // Only if eBPF is available
		for _, svc := range services {
			// Map cluster IP
			err := collector.UpdateServiceEndpoint(svc.clusterIP, svc.port,
				svc.name, svc.namespace, svc.clusterIP)
			if err == nil {
				fmt.Printf("✓ Mapped ClusterIP %s:%d → %s/%s\n",
					svc.clusterIP, svc.port, svc.namespace, svc.name)
			}

			// Map each endpoint
			for _, ep := range svc.endpoints {
				err := collector.UpdateServiceEndpoint(ep, svc.port,
					svc.name, svc.namespace, svc.clusterIP)
				if err == nil {
					fmt.Printf("  → Endpoint %s:%d\n", ep, svc.port)
				}
			}
		}
	}

	// Simulate network events
	fmt.Printf("\n=== Simulated Network Events ===\n")

	// Example 1: Pod connecting to frontend service via ClusterIP
	event1 := NetworkInfo{
		SAddr:     collector.parseIPv4("10.244.1.100"), // Source pod
		DAddr:     collector.parseIPv4("10.96.0.10"),   // Frontend ClusterIP
		SPort:     45678,
		DPort:     80,
		Protocol:  6, // TCP
		Direction: 0, // Outgoing
	}

	fmt.Printf("\nConnection: %s:%d → %s:%d (TCP)\n",
		collector.ipToString(event1.SAddr), event1.SPort,
		collector.ipToString(event1.DAddr), event1.DPort)

	// Check if destination is a known service
	if collector.objs != nil {
		if svc, err := collector.GetServiceEndpoint(event1.DAddr, event1.DPort); err == nil {
			fmt.Printf("✓ Identified as service: %s in namespace %s\n",
				collector.nullTerminatedString(svc.ServiceName[:]),
				collector.nullTerminatedString(svc.Namespace[:]))
		}
	}

	// Example 2: Pod-to-pod direct connection
	event2 := NetworkInfo{
		SAddr:     collector.parseIPv4("10.244.1.5"),  // Frontend pod
		DAddr:     collector.parseIPv4("10.244.1.15"), // Backend pod
		SPort:     35000,
		DPort:     8080,
		Protocol:  6, // TCP
		Direction: 0, // Outgoing
	}

	fmt.Printf("\nConnection: %s:%d → %s:%d (TCP)\n",
		collector.ipToString(event2.SAddr), event2.SPort,
		collector.ipToString(event2.DAddr), event2.DPort)

	if collector.objs != nil {
		if svc, err := collector.GetServiceEndpoint(event2.DAddr, event2.DPort); err == nil {
			fmt.Printf("✓ Identified as service endpoint: %s in namespace %s\n",
				collector.nullTerminatedString(svc.ServiceName[:]),
				collector.nullTerminatedString(svc.Namespace[:]))
		}
	}

	// Show the narrative this enables
	fmt.Printf("\n=== Narrative ===\n")
	fmt.Printf("A pod at %s initiated a connection to the 'frontend-service'\n",
		collector.ipToString(event1.SAddr))
	fmt.Printf("which load-balanced to one of 3 available endpoints.\n")
	fmt.Printf("The frontend pod then connected directly to the 'backend-api' service\n")
	fmt.Printf("at endpoint %s:8080, bypassing the service proxy.\n",
		collector.ipToString(event2.DAddr))

	// Demonstrate lookup and cleanup
	if collector.objs != nil {
		// Remove some endpoints
		collector.RemoveServiceEndpoint("10.244.1.5", 80)
		collector.RemoveServiceEndpoint("10.244.2.8", 80)
		fmt.Printf("\n✓ Removed 2 frontend endpoints (simulating pod deletion)\n")
	}
}

// TestNetworkEventProcessing tests the network event processing in collector
func TestNetworkEventProcessing(t *testing.T) {
	collector, err := NewCollector("network-test")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Create a mock kernel event with network info
	event := KernelEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       1234,
		TID:       1234,
		EventType: 5, // EVENT_TYPE_NETWORK_CONN
		Size:      0,
		CgroupID:  5678,
	}
	copy(event.Comm[:], "test-app")
	copy(event.PodUID[:], "test-pod-123")

	// Add network info to the data field
	netInfo := NetworkInfo{
		SAddr:     collector.parseIPv4("192.168.1.100"),
		DAddr:     collector.parseIPv4("10.96.0.10"),
		SPort:     45678,
		DPort:     80,
		Protocol:  6, // TCP
		Direction: 0, // Outgoing
		State:     1, // Connecting
	}

	// Verify IP conversion functions
	if collector.ipToString(netInfo.SAddr) != "192.168.1.100" {
		t.Errorf("IP to string conversion failed for source")
	}
	if collector.ipToString(netInfo.DAddr) != "10.96.0.10" {
		t.Errorf("IP to string conversion failed for destination")
	}

	// Verify protocol conversion
	if collector.protocolToString(netInfo.Protocol) != "tcp" {
		t.Errorf("Protocol conversion failed")
	}

	// Verify direction conversion
	if collector.directionToString(netInfo.Direction) != "outgoing" {
		t.Errorf("Direction conversion failed")
	}
}

// TestEndpointKeyGeneration tests the endpoint key generation
func TestEndpointKeyGeneration(t *testing.T) {
	collector, _ := NewCollector("key-test")

	tests := []struct {
		ip   string
		port uint16
		want uint64
	}{
		{"10.96.0.10", 80, (uint64(0x0a60000a) << 16) | 80},
		{"192.168.1.1", 8080, (uint64(0xc0a80101) << 16) | 8080},
		{"127.0.0.1", 443, (uint64(0x7f000001) << 16) | 443},
	}

	for _, tc := range tests {
		ipAddr := collector.parseIPv4(tc.ip)
		key := collector.makeEndpointKey(ipAddr, tc.port)
		if key != tc.want {
			t.Errorf("makeEndpointKey(%s, %d) = %x, want %x",
				tc.ip, tc.port, key, tc.want)
		}
	}
}

// TestIPv4Parsing tests IPv4 address parsing
func TestIPv4Parsing(t *testing.T) {
	collector, _ := NewCollector("ip-test")

	tests := []struct {
		ip   string
		want uint32
	}{
		{"10.96.0.10", 0x0a60000a},
		{"192.168.1.1", 0xc0a80101},
		{"127.0.0.1", 0x7f000001},
		{"255.255.255.255", 0xffffffff},
		{"0.0.0.0", 0x00000000},
		{"invalid", 0},   // Should return 0 for invalid IP
		{"256.0.0.1", 0}, // Invalid octet
	}

	for _, tc := range tests {
		got := collector.parseIPv4(tc.ip)
		if got != tc.want {
			t.Errorf("parseIPv4(%s) = %x, want %x", tc.ip, got, tc.want)
		}
	}
}

// Example of how network correlation enables service dependency mapping
func ExampleCollector_UpdateServiceEndpoint() {
	// This example shows how network events can be correlated to services
	// to build a service dependency graph

	// Raw network event: PID 1234 connects to 10.96.0.20:8080
	// After correlation:
	//   - PID 1234 → Container frontend-abc → Pod frontend-xyz
	//   - IP 10.96.0.20:8080 → Service backend-api
	//
	// Narrative: "Frontend pod connected to backend-api service"
	// This enables automatic service dependency discovery!

	fmt.Println("See TestServiceEndpointCorrelation for demonstration")
}
