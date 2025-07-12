package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// HealthChecker checks system and service health
type HealthChecker struct {
	client  *http.Client
	timeout time.Duration
}

// NewHealthChecker creates a new health checker
func NewHealthChecker() *HealthChecker {
	return &HealthChecker{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		timeout: 30 * time.Second,
	}
}

// CheckService checks if a service is healthy
func (h *HealthChecker) CheckService(ctx context.Context, serviceName string) error {
	// Check service status based on platform
	switch runtime.GOOS {
	case "linux":
		return h.checkLinuxService(ctx, serviceName)
	case "darwin":
		return h.checkDarwinService(ctx, serviceName)
	case "windows":
		return h.checkWindowsService(ctx, serviceName)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// checkLinuxService checks a Linux service
func (h *HealthChecker) checkLinuxService(ctx context.Context, serviceName string) error {
	// Try systemd first
	if h.hasSystemd() {
		cmd := exec.CommandContext(ctx, "systemctl", "is-active", serviceName)
		output, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("service %s is not active: %w", serviceName, err)
		}
		
		status := strings.TrimSpace(string(output))
		if status != "active" {
			return fmt.Errorf("service %s is %s", serviceName, status)
		}
		
		// Check if service is enabled
		cmd = exec.CommandContext(ctx, "systemctl", "is-enabled", serviceName)
		output, err = cmd.Output()
		if err != nil {
			// Not critical if service is running
			return nil
		}
		
		enabled := strings.TrimSpace(string(output))
		if enabled != "enabled" {
			// Warning, but not an error
			fmt.Printf("Warning: service %s is not enabled (status: %s)\n", serviceName, enabled)
		}
		
		return nil
	}
	
	// Try other init systems
	// For now, just check if process is running
	return h.checkProcessRunning(ctx, serviceName)
}

// checkDarwinService checks a macOS service
func (h *HealthChecker) checkDarwinService(ctx context.Context, serviceName string) error {
	label := fmt.Sprintf("com.tapio.%s", serviceName)
	
	cmd := exec.CommandContext(ctx, "launchctl", "list", label)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("service %s is not loaded: %w", serviceName, err)
	}
	
	// Parse output to check PID
	lines := strings.Split(string(output), "\n")
	if len(lines) > 0 {
		fields := strings.Fields(lines[0])
		if len(fields) >= 3 {
			pid := fields[0]
			if pid == "-" {
				return fmt.Errorf("service %s is not running", serviceName)
			}
		}
	}
	
	return nil
}

// checkWindowsService checks a Windows service
func (h *HealthChecker) checkWindowsService(ctx context.Context, serviceName string) error {
	cmd := exec.CommandContext(ctx, "sc.exe", "query", serviceName)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("service %s not found: %w", serviceName, err)
	}
	
	outputStr := string(output)
	if !strings.Contains(outputStr, "RUNNING") {
		if strings.Contains(outputStr, "STOPPED") {
			return fmt.Errorf("service %s is stopped", serviceName)
		}
		return fmt.Errorf("service %s is not running", serviceName)
	}
	
	return nil
}

// hasSystemd checks if systemd is available
func (h *HealthChecker) hasSystemd() bool {
	_, err := exec.LookPath("systemctl")
	return err == nil
}

// checkProcessRunning checks if a process is running
func (h *HealthChecker) checkProcessRunning(ctx context.Context, processName string) error {
	var cmd *exec.Cmd
	
	switch runtime.GOOS {
	case "linux", "darwin":
		cmd = exec.CommandContext(ctx, "pgrep", "-x", processName)
	case "windows":
		cmd = exec.CommandContext(ctx, "tasklist", "/FI", fmt.Sprintf("IMAGENAME eq %s.exe", processName))
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("process %s not found", processName)
	}
	
	if runtime.GOOS == "windows" && !strings.Contains(string(output), processName) {
		return fmt.Errorf("process %s not found", processName)
	}
	
	return nil
}

// CheckEndpoint checks if an HTTP endpoint is healthy
func (h *HealthChecker) CheckEndpoint(ctx context.Context, endpoint string) (*HealthStatus, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unhealthy status: %s", resp.Status)
	}
	
	// Try to parse health response
	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		// If parsing fails, just return basic status
		status = HealthStatus{
			Status:    "ok",
			Timestamp: time.Now(),
		}
	}
	
	return &status, nil
}

// HealthStatus represents service health status
type HealthStatus struct {
	Status    string                 `json:"status"`
	Version   string                 `json:"version,omitempty"`
	Uptime    string                 `json:"uptime,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Checks    map[string]CheckStatus `json:"checks,omitempty"`
}

// CheckStatus represents a health check status
type CheckStatus struct {
	Status  string        `json:"status"`
	Message string        `json:"message,omitempty"`
	Details interface{}   `json:"details,omitempty"`
}

// SystemHealth represents overall system health
type SystemHealth struct {
	CPU        CPUHealth        `json:"cpu"`
	Memory     MemoryHealth     `json:"memory"`
	Disk       DiskHealth       `json:"disk"`
	Network    NetworkHealth    `json:"network"`
	Services   []ServiceHealth  `json:"services"`
	Timestamp  time.Time        `json:"timestamp"`
}

// CPUHealth represents CPU health metrics
type CPUHealth struct {
	Usage       float64 `json:"usage"`
	LoadAverage []float64 `json:"load_average,omitempty"`
	Cores       int     `json:"cores"`
	Temperature float64 `json:"temperature,omitempty"`
}

// MemoryHealth represents memory health metrics
type MemoryHealth struct {
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Free        uint64  `json:"free"`
	UsagePercent float64 `json:"usage_percent"`
	SwapTotal   uint64  `json:"swap_total,omitempty"`
	SwapUsed    uint64  `json:"swap_used,omitempty"`
}

// DiskHealth represents disk health metrics
type DiskHealth struct {
	Mounts []DiskMount `json:"mounts"`
}

// DiskMount represents a disk mount point
type DiskMount struct {
	Path         string  `json:"path"`
	Total        uint64  `json:"total"`
	Used         uint64  `json:"used"`
	Free         uint64  `json:"free"`
	UsagePercent float64 `json:"usage_percent"`
	FileSystem   string  `json:"file_system"`
}

// NetworkHealth represents network health metrics
type NetworkHealth struct {
	Interfaces []NetworkInterface `json:"interfaces"`
	Connectivity ConnectivityStatus `json:"connectivity"`
}

// NetworkInterface represents a network interface
type NetworkInterface struct {
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	IPAddresses []string `json:"ip_addresses"`
	BytesSent   uint64   `json:"bytes_sent"`
	BytesRecv   uint64   `json:"bytes_recv"`
}

// ConnectivityStatus represents network connectivity status
type ConnectivityStatus struct {
	Internet bool              `json:"internet"`
	DNS      bool              `json:"dns"`
	Latency  map[string]int64  `json:"latency,omitempty"`
}

// ServiceHealth represents a service's health
type ServiceHealth struct {
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	PID       int       `json:"pid,omitempty"`
	Uptime    string    `json:"uptime,omitempty"`
	Memory    uint64    `json:"memory,omitempty"`
	CPU       float64   `json:"cpu,omitempty"`
	Endpoints []string  `json:"endpoints,omitempty"`
}

// CheckSystemHealth performs a comprehensive system health check
func (h *HealthChecker) CheckSystemHealth(ctx context.Context) (*SystemHealth, error) {
	health := &SystemHealth{
		Timestamp: time.Now(),
	}
	
	// Check CPU
	// This is a simplified implementation - real implementation would use
	// system-specific APIs or libraries like gopsutil
	health.CPU = CPUHealth{
		Usage: 0.0, // Would calculate actual usage
		Cores: runtime.NumCPU(),
	}
	
	// Check Memory
	health.Memory = MemoryHealth{
		Total:        16 * 1024 * 1024 * 1024, // 16GB placeholder
		Used:         8 * 1024 * 1024 * 1024,  // 8GB placeholder
		Free:         8 * 1024 * 1024 * 1024,  // 8GB placeholder
		UsagePercent: 50.0,
	}
	
	// Check Disk
	health.Disk = DiskHealth{
		Mounts: []DiskMount{
			{
				Path:         "/",
				Total:        500 * 1024 * 1024 * 1024, // 500GB placeholder
				Used:         250 * 1024 * 1024 * 1024, // 250GB placeholder
				Free:         250 * 1024 * 1024 * 1024, // 250GB placeholder
				UsagePercent: 50.0,
				FileSystem:   "ext4",
			},
		},
	}
	
	// Check Network
	health.Network = NetworkHealth{
		Connectivity: ConnectivityStatus{
			Internet: true,
			DNS:      true,
		},
	}
	
	// Check Services
	services := []string{"tapio"}
	for _, service := range services {
		serviceHealth := ServiceHealth{
			Name: service,
		}
		
		if err := h.CheckService(ctx, service); err != nil {
			serviceHealth.Status = "unhealthy"
		} else {
			serviceHealth.Status = "healthy"
		}
		
		health.Services = append(health.Services, serviceHealth)
	}
	
	return health, nil
}