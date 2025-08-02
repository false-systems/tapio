package ebpf

import (
	"fmt"
	"testing"
	"time"
)

// TestMountInfoCorrelation demonstrates ConfigMap/Secret mount correlation
func TestMountInfoCorrelation(t *testing.T) {
	// This test demonstrates how the correlation would work
	// without requiring actual eBPF (for CI/CD compatibility)

	collector, err := NewCollector("mount-demo")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// In a real K8s scenario:
	// 1. K8s API watcher sees ConfigMap/Secret creation
	// 2. K8s mounts them into pods at specific paths
	// 3. We update the mount info map

	// Example mounts in a typical K8s pod
	mounts := []struct {
		mountPath string
		name      string
		namespace string
		isSecret  bool
	}{
		// ConfigMaps
		{
			mountPath: "/etc/config/app.yaml",
			name:      "app-config",
			namespace: "production",
			isSecret:  false,
		},
		{
			mountPath: "/etc/nginx/nginx.conf",
			name:      "nginx-config",
			namespace: "production",
			isSecret:  false,
		},
		// Secrets
		{
			mountPath: "/etc/secrets/db-password",
			name:      "database-credentials",
			namespace: "production",
			isSecret:  true,
		},
		{
			mountPath: "/etc/tls/cert.pem",
			name:      "frontend-tls",
			namespace: "production",
			isSecret:  true,
		},
		{
			mountPath: "/etc/tls/key.pem",
			name:      "frontend-tls",
			namespace: "production",
			isSecret:  true,
		},
		// Service account token (auto-mounted)
		{
			mountPath: "/var/run/secrets/kubernetes.io/serviceaccount/token",
			name:      "frontend-sa-token",
			namespace: "production",
			isSecret:  true,
		},
	}

	fmt.Printf("\n=== ConfigMap/Secret Mount Correlation Demo ===\n")

	// Update mount info (would be called by K8s watcher)
	if collector.objs != nil { // Only if eBPF is available
		for _, mount := range mounts {
			err := collector.UpdateMountInfo(mount.mountPath, mount.name,
				mount.namespace, mount.isSecret)
			if err == nil {
				mountType := "ConfigMap"
				if mount.isSecret {
					mountType = "Secret"
				}
				fmt.Printf("✓ Mapped %s: %s → %s/%s\n",
					mountType, mount.mountPath, mount.namespace, mount.name)
			}
		}
	}

	// Simulate file access events
	fmt.Printf("\n=== Simulated File Access Events ===\n")

	// Example 1: Pod reading configuration
	file1 := "/etc/config/app.yaml"
	fmt.Printf("\nFile access: %s\n", file1)

	if collector.objs != nil {
		if mount, err := collector.GetMountInfo(file1); err == nil {
			fmt.Printf("✓ Identified as ConfigMap: %s in namespace %s\n",
				collector.nullTerminatedString(mount.Name[:]),
				collector.nullTerminatedString(mount.Namespace[:]))
		}
	}

	// Example 2: Pod reading secret
	file2 := "/etc/secrets/db-password"
	fmt.Printf("\nFile access: %s\n", file2)

	if collector.objs != nil {
		if mount, err := collector.GetMountInfo(file2); err == nil {
			fmt.Printf("✓ Identified as Secret: %s in namespace %s\n",
				collector.nullTerminatedString(mount.Name[:]),
				collector.nullTerminatedString(mount.Namespace[:]))
			fmt.Printf("⚠️  Security Alert: Secret accessed!\n")
		}
	}

	// Show the narrative this enables
	fmt.Printf("\n=== Narrative ===\n")
	fmt.Printf("Container 'nginx' in pod 'frontend-xyz' accessed configuration\n")
	fmt.Printf("from ConfigMap 'app-config' at startup.\n")
	fmt.Printf("Later, it read database credentials from Secret 'database-credentials'.\n")
	fmt.Printf("This access pattern suggests the app is connecting to the database.\n")

	// Demonstrate path patterns
	fmt.Printf("\n=== Common K8s Mount Patterns ===\n")
	fmt.Printf("ConfigMaps: /etc/config/*, /etc/nginx/*, /app/config/*\n")
	fmt.Printf("Secrets: /etc/secrets/*, /etc/tls/*, /var/run/secrets/*\n")
	fmt.Printf("Service Account: /var/run/secrets/kubernetes.io/serviceaccount/*\n")
}

// TestFileEventProcessing tests file event processing in collector
func TestFileEventProcessing(t *testing.T) {
	collector, err := NewCollector("file-test")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Create a mock kernel event with file info
	event := KernelEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       5678,
		TID:       5678,
		EventType: 8, // EVENT_TYPE_FILE_OPEN
		Size:      0,
		CgroupID:  9999,
	}
	copy(event.Comm[:], "app")
	copy(event.PodUID[:], "app-pod-789")

	// Add file info to the data field
	fileInfo := FileInfo{}
	copy(fileInfo.Filename[:], "/etc/config/app.yaml")
	fileInfo.Flags = 0
	fileInfo.Mode = 0644

	// Test the event type conversion
	if collector.eventTypeToString(event.EventType) != "file_open" {
		t.Errorf("Event type conversion failed")
	}
}

// TestHashPath tests the path hashing function
func TestHashPath(t *testing.T) {
	collector, _ := NewCollector("hash-test")

	tests := []struct {
		path string
		want bool // Just check it's non-zero
	}{
		{"/etc/config/app.yaml", true},
		{"/etc/secrets/password", true},
		{"/var/run/secrets/kubernetes.io/serviceaccount/token", true},
		{"", true}, // Empty should still produce a hash
	}

	hashes := make(map[uint64]string)
	for _, tc := range tests {
		hash := collector.hashPath(tc.path)
		if hash == 0 && tc.want {
			t.Errorf("hashPath(%s) = 0, want non-zero", tc.path)
		}

		// Check for collisions in our test set
		if existing, exists := hashes[hash]; exists {
			t.Errorf("Hash collision: %s and %s both hash to %x",
				existing, tc.path, hash)
		}
		hashes[hash] = tc.path
	}
}

// Example of how file correlation enables configuration tracking
func ExampleCollector_UpdateMountInfo() {
	// This example shows how file operations can be correlated to ConfigMaps/Secrets
	// to track configuration usage and detect security issues

	// Raw file event: PID 5678 opens /etc/config/app.yaml
	// After correlation:
	//   - PID 5678 → Container app-backend → Pod backend-xyz
	//   - File /etc/config/app.yaml → ConfigMap app-config
	//
	// Narrative: "Backend pod loaded configuration from ConfigMap app-config"
	// This enables configuration change tracking and security auditing!

	fmt.Println("See TestMountInfoCorrelation for demonstration")
}
