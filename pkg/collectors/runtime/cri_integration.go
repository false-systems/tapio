// Package runtime provides container runtime integration for Tapio
// Supports containerd, CRI-O, and Docker through unified interface
package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	criapi "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// ContainerRuntime represents a container runtime interface
type ContainerRuntime interface {
	// GetContainerInfo returns container information by container ID
	GetContainerInfo(ctx context.Context, containerID string) (*ContainerInfo, error)
	// GetPodSandboxInfo returns pod sandbox information
	GetPodSandboxInfo(ctx context.Context, podID string) (*PodSandboxInfo, error)
	// ListContainers returns all running containers
	ListContainers(ctx context.Context) ([]*ContainerInfo, error)
	// GetContainerPID returns the main process PID of a container
	GetContainerPID(ctx context.Context, containerID string) (uint32, error)
	// GetCgroupPath returns the cgroup path for a container
	GetCgroupPath(ctx context.Context, containerID string) (string, error)
	// Close closes the runtime connection
	Close() error
}

// ContainerInfo holds container runtime information
type ContainerInfo struct {
	ID           string
	Name         string
	PodID        string
	PodName      string
	PodNamespace string
	PodUID       string
	Image        string
	ImageID      string
	PID          uint32
	CgroupPath   string
	CgroupID     uint64
	RuntimeName  string
	Labels       map[string]string
	Annotations  map[string]string
	State        string
	CreatedAt    time.Time
	StartedAt    time.Time
	NetworkMode  string
	NetworkNS    string
	PidNS        string
	IpcNS        string
	UtsNS        string
	UserNS       string
	MountNS      string
}

// PodSandboxInfo holds pod sandbox information
type PodSandboxInfo struct {
	ID           string
	Name         string
	Namespace    string
	UID          string
	Labels       map[string]string
	Annotations  map[string]string
	CgroupParent string
	NetworkNS    string
	CreatedAt    time.Time
}

// RuntimeManager manages multiple container runtime connections
type RuntimeManager struct {
	logger   *zap.Logger
	runtimes map[string]ContainerRuntime
	primary  ContainerRuntime
	mu       sync.RWMutex

	// Cache for container info
	containerCache *sync.Map // map[string]*ContainerInfo
	cacheTimeout   time.Duration

	// Metrics
	lookups     uint64
	cacheHits   uint64
	cacheMisses uint64
}

// NewRuntimeManager creates a new runtime manager with auto-detection
func NewRuntimeManager(logger *zap.Logger) (*RuntimeManager, error) {
	rm := &RuntimeManager{
		logger:         logger,
		runtimes:       make(map[string]ContainerRuntime),
		containerCache: &sync.Map{},
		cacheTimeout:   30 * time.Second,
	}

	// Auto-detect and connect to available runtimes
	if err := rm.autoDetectRuntimes(); err != nil {
		return nil, fmt.Errorf("failed to detect container runtimes: %w", err)
	}

	if rm.primary == nil {
		return nil, fmt.Errorf("no container runtime detected")
	}

	// Start cache cleanup
	go rm.cleanupCache()

	return rm, nil
}

// autoDetectRuntimes detects and connects to available container runtimes
func (rm *RuntimeManager) autoDetectRuntimes() error {
	// Try containerd first (most common in modern k8s)
	if runtime, err := rm.tryContainerd(); err == nil {
		rm.runtimes["containerd"] = runtime
		rm.primary = runtime
		rm.logger.Info("Connected to containerd runtime")
		return nil
	}

	// Try CRI-O
	if runtime, err := rm.tryCRIO(); err == nil {
		rm.runtimes["cri-o"] = runtime
		rm.primary = runtime
		rm.logger.Info("Connected to CRI-O runtime")
		return nil
	}

	// Try Docker (legacy but still used)
	if runtime, err := rm.tryDocker(); err == nil {
		rm.runtimes["docker"] = runtime
		rm.primary = runtime
		rm.logger.Info("Connected to Docker runtime")
		return nil
	}

	return fmt.Errorf("no container runtime found")
}

// tryContainerd attempts to connect to containerd
func (rm *RuntimeManager) tryContainerd() (ContainerRuntime, error) {
	socketPaths := []string{
		"/run/containerd/containerd.sock",
		"/var/run/containerd/containerd.sock",
		"/run/k3s/containerd/containerd.sock",
	}

	for _, path := range socketPaths {
		if _, err := os.Stat(path); err == nil {
			return NewContainerdRuntime(path, rm.logger)
		}
	}

	return nil, fmt.Errorf("containerd socket not found")
}

// tryCRIO attempts to connect to CRI-O
func (rm *RuntimeManager) tryCRIO() (ContainerRuntime, error) {
	socketPaths := []string{
		"/run/crio/crio.sock",
		"/var/run/crio/crio.sock",
	}

	for _, path := range socketPaths {
		if _, err := os.Stat(path); err == nil {
			return NewCRIORuntime(path, rm.logger)
		}
	}

	return nil, fmt.Errorf("CRI-O socket not found")
}

// tryDocker attempts to connect to Docker
func (rm *RuntimeManager) tryDocker() (ContainerRuntime, error) {
	socketPaths := []string{
		"/var/run/docker.sock",
		"/run/docker.sock",
	}

	for _, path := range socketPaths {
		if _, err := os.Stat(path); err == nil {
			return NewDockerRuntime(path, rm.logger)
		}
	}

	return nil, fmt.Errorf("Docker socket not found")
}

// GetContainerInfoByPID gets container info by process PID
func (rm *RuntimeManager) GetContainerInfoByPID(pid uint32) (*ContainerInfo, error) {
	// Read cgroup from /proc/PID/cgroup
	cgroupPath := fmt.Sprintf("/proc/%d/cgroup", pid)
	data, err := os.ReadFile(cgroupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cgroup: %w", err)
	}

	// Parse container ID from cgroup path
	containerID := rm.parseContainerID(string(data))
	if containerID == "" {
		return nil, fmt.Errorf("container ID not found in cgroup")
	}

	// Check cache first
	if cached, ok := rm.containerCache.Load(containerID); ok {
		if info, ok := cached.(*ContainerInfo); ok {
			rm.cacheHits++
			return info, nil
		}
	}

	rm.cacheMisses++

	// Query runtime
	info, err := rm.primary.GetContainerInfo(context.Background(), containerID)
	if err != nil {
		return nil, err
	}

	// Update cache
	rm.containerCache.Store(containerID, info)

	return info, nil
}

// parseContainerID extracts container ID from cgroup path
func (rm *RuntimeManager) parseContainerID(cgroupData string) string {
	lines := strings.Split(cgroupData, "\n")
	for _, line := range lines {
		// Format: hierarchy-ID:controller-list:cgroup-path
		parts := strings.Split(line, ":")
		if len(parts) != 3 {
			continue
		}

		cgroupPath := parts[2]

		// Docker format: /docker/<container-id>
		if strings.Contains(cgroupPath, "/docker/") {
			idx := strings.LastIndex(cgroupPath, "/docker/")
			if idx >= 0 {
				id := cgroupPath[idx+8:]
				if len(id) >= 12 {
					return id[:12] // Docker uses 12 char short IDs
				}
			}
		}

		// Containerd format: /containerd/<container-id>
		if strings.Contains(cgroupPath, "/containerd/") {
			idx := strings.LastIndex(cgroupPath, "/containerd/")
			if idx >= 0 {
				id := cgroupPath[idx+12:]
				// Remove any trailing .scope
				if idx := strings.Index(id, ".scope"); idx > 0 {
					id = id[:idx]
				}
				return id
			}
		}

		// Kubernetes pod format: /kubepods/.../<pod-id>/<container-id>
		if strings.Contains(cgroupPath, "/kubepods/") {
			parts := strings.Split(cgroupPath, "/")
			if len(parts) > 0 {
				// Last part is usually container ID
				id := parts[len(parts)-1]
				// Remove crio- prefix if present
				id = strings.TrimPrefix(id, "crio-")
				// Remove .scope suffix if present
				id = strings.TrimSuffix(id, ".scope")
				if len(id) >= 12 {
					return id
				}
			}
		}
	}

	return ""
}

// GetCgroupID returns the cgroup inode ID for a container
func (rm *RuntimeManager) GetCgroupID(containerID string) (uint64, error) {
	info, err := rm.primary.GetContainerInfo(context.Background(), containerID)
	if err != nil {
		return 0, err
	}

	// Get cgroup path and resolve to inode
	return rm.getCgroupInode(info.CgroupPath)
}

// getCgroupInode gets the inode number of a cgroup path
func (rm *RuntimeManager) getCgroupInode(cgroupPath string) (uint64, error) {
	// cgroup v2 path
	cgroupV2Path := filepath.Join("/sys/fs/cgroup", cgroupPath)
	if stat, err := os.Stat(cgroupV2Path); err == nil {
		if sys, ok := stat.Sys().(*os.FileInfo); ok {
			// Extract inode from stat - platform specific
			return extractInode(sys), nil
		}
	}

	// cgroup v1 path (try memory controller)
	cgroupV1Path := filepath.Join("/sys/fs/cgroup/memory", cgroupPath)
	if stat, err := os.Stat(cgroupV1Path); err == nil {
		if sys, ok := stat.Sys().(*os.FileInfo); ok {
			return extractInode(sys), nil
		}
	}

	return 0, fmt.Errorf("cgroup path not found: %s", cgroupPath)
}

// cleanupCache periodically cleans expired cache entries
func (rm *RuntimeManager) cleanupCache() {
	ticker := time.NewTicker(rm.cacheTimeout)
	defer ticker.Stop()

	for range ticker.C {
		// In production, track timestamps and remove old entries
		// For now, clear entire cache periodically
		rm.containerCache.Range(func(key, value interface{}) bool {
			rm.containerCache.Delete(key)
			return true
		})
	}
}

// Close closes all runtime connections
func (rm *RuntimeManager) Close() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	for name, runtime := range rm.runtimes {
		if err := runtime.Close(); err != nil {
			rm.logger.Error("Failed to close runtime", zap.String("runtime", name), zap.Error(err))
		}
	}

	return nil
}

// ContainerdRuntime implements ContainerRuntime for containerd
type ContainerdRuntime struct {
	client criapi.RuntimeServiceClient
	conn   *grpc.ClientConn
	logger *zap.Logger
}

// NewContainerdRuntime creates a new containerd runtime client
func NewContainerdRuntime(socketPath string, logger *zap.Logger) (*ContainerdRuntime, error) {
	conn, err := grpc.Dial(
		"unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(5*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to containerd: %w", err)
	}

	client := criapi.NewRuntimeServiceClient(conn)

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := client.Version(ctx, &criapi.VersionRequest{}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to get containerd version: %w", err)
	}

	return &ContainerdRuntime{
		client: client,
		conn:   conn,
		logger: logger,
	}, nil
}

// GetContainerInfo implements ContainerRuntime
func (c *ContainerdRuntime) GetContainerInfo(ctx context.Context, containerID string) (*ContainerInfo, error) {
	// Get container status
	resp, err := c.client.ContainerStatus(ctx, &criapi.ContainerStatusRequest{
		ContainerId: containerID,
		Verbose:     true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get container status: %w", err)
	}

	status := resp.Status
	if status == nil {
		return nil, fmt.Errorf("container not found: %s", containerID)
	}

	info := &ContainerInfo{
		ID:          status.Id,
		Labels:      status.Labels,
		Annotations: status.Annotations,
		State:       status.State.String(),
		CreatedAt:   time.Unix(0, status.CreatedAt),
		StartedAt:   time.Unix(0, status.StartedAt),
		Image:       status.Image.Image,
		ImageID:     status.ImageRef,
		RuntimeName: "containerd",
	}

	// Extract metadata from labels
	if podName, ok := status.Labels["io.kubernetes.pod.name"]; ok {
		info.PodName = podName
	}
	if podNamespace, ok := status.Labels["io.kubernetes.pod.namespace"]; ok {
		info.PodNamespace = podNamespace
	}
	if podUID, ok := status.Labels["io.kubernetes.pod.uid"]; ok {
		info.PodUID = podUID
	}

	// Get PID from verbose info (JSON in info map)
	if pidStr, ok := resp.Info["pid"]; ok {
		if pid, err := strconv.ParseUint(pidStr, 10, 32); err == nil {
			info.PID = uint32(pid)
		}
	}

	return info, nil
}

// GetPodSandboxInfo implements ContainerRuntime
func (c *ContainerdRuntime) GetPodSandboxInfo(ctx context.Context, podID string) (*PodSandboxInfo, error) {
	resp, err := c.client.PodSandboxStatus(ctx, &criapi.PodSandboxStatusRequest{
		PodSandboxId: podID,
		Verbose:      true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod sandbox status: %w", err)
	}

	status := resp.Status
	if status == nil {
		return nil, fmt.Errorf("pod sandbox not found: %s", podID)
	}

	return &PodSandboxInfo{
		ID:          status.Id,
		Labels:      status.Labels,
		Annotations: status.Annotations,
		CreatedAt:   time.Unix(0, status.CreatedAt),
	}, nil
}

// ListContainers implements ContainerRuntime
func (c *ContainerdRuntime) ListContainers(ctx context.Context) ([]*ContainerInfo, error) {
	resp, err := c.client.ListContainers(ctx, &criapi.ListContainersRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	containers := make([]*ContainerInfo, 0, len(resp.Containers))
	for _, container := range resp.Containers {
		info, err := c.GetContainerInfo(ctx, container.Id)
		if err != nil {
			c.logger.Warn("Failed to get container info", zap.String("id", container.Id), zap.Error(err))
			continue
		}
		containers = append(containers, info)
	}

	return containers, nil
}

// GetContainerPID implements ContainerRuntime
func (c *ContainerdRuntime) GetContainerPID(ctx context.Context, containerID string) (uint32, error) {
	info, err := c.GetContainerInfo(ctx, containerID)
	if err != nil {
		return 0, err
	}
	return info.PID, nil
}

// GetCgroupPath implements ContainerRuntime
func (c *ContainerdRuntime) GetCgroupPath(ctx context.Context, containerID string) (string, error) {
	info, err := c.GetContainerInfo(ctx, containerID)
	if err != nil {
		return "", err
	}
	return info.CgroupPath, nil
}

// Close implements ContainerRuntime
func (c *ContainerdRuntime) Close() error {
	return c.conn.Close()
}

// CRIORuntime implements ContainerRuntime for CRI-O
type CRIORuntime struct {
	ContainerdRuntime // CRI-O uses the same CRI API
}

// NewCRIORuntime creates a new CRI-O runtime client
func NewCRIORuntime(socketPath string, logger *zap.Logger) (*CRIORuntime, error) {
	containerd, err := NewContainerdRuntime(socketPath, logger)
	if err != nil {
		return nil, err
	}
	return &CRIORuntime{*containerd}, nil
}

// DockerRuntime implements ContainerRuntime for Docker
type DockerRuntime struct {
	// In production, use Docker API client
	// For now, simplified implementation
	socketPath string
	logger     *zap.Logger
}

// NewDockerRuntime creates a new Docker runtime client
func NewDockerRuntime(socketPath string, logger *zap.Logger) (*DockerRuntime, error) {
	// Verify socket exists
	if _, err := os.Stat(socketPath); err != nil {
		return nil, fmt.Errorf("Docker socket not found: %w", err)
	}

	return &DockerRuntime{
		socketPath: socketPath,
		logger:     logger,
	}, nil
}

// Implement ContainerRuntime methods for Docker...
// (Simplified stubs for now - would use Docker API in production)

func (d *DockerRuntime) GetContainerInfo(ctx context.Context, containerID string) (*ContainerInfo, error) {
	// Would use Docker API client here
	return nil, fmt.Errorf("Docker runtime not fully implemented")
}

func (d *DockerRuntime) GetPodSandboxInfo(ctx context.Context, podID string) (*PodSandboxInfo, error) {
	return nil, fmt.Errorf("Docker runtime not fully implemented")
}

func (d *DockerRuntime) ListContainers(ctx context.Context) ([]*ContainerInfo, error) {
	return nil, fmt.Errorf("Docker runtime not fully implemented")
}

func (d *DockerRuntime) GetContainerPID(ctx context.Context, containerID string) (uint32, error) {
	return 0, fmt.Errorf("Docker runtime not fully implemented")
}

func (d *DockerRuntime) GetCgroupPath(ctx context.Context, containerID string) (string, error) {
	return "", fmt.Errorf("Docker runtime not fully implemented")
}

func (d *DockerRuntime) Close() error {
	return nil
}

// Platform-specific inode extraction
// This would be in a separate file with build tags in production
func extractInode(info interface{}) uint64 {
	// Simplified - would use syscall.Stat_t in production
	return 0
}
