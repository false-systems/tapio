//go:build linux

package systemd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractUnitFromCgroup(t *testing.T) {
	collector := &Collector{}

	tests := []struct {
		name        string
		cgroupData  string
		expected    string
		description string
	}{
		{
			name: "standard systemd service cgroup v1",
			cgroupData: `11:devices:/system.slice/nginx.service
10:blkio:/system.slice/nginx.service
9:memory:/system.slice/nginx.service
8:net_cls,net_prio:/
7:freezer:/
6:pids:/system.slice/nginx.service
5:cpuset:/
4:cpu,cpuacct:/system.slice/nginx.service
3:perf_event:/
2:hugetlb:/
1:name=systemd:/system.slice/nginx.service
0::/system.slice/nginx.service`,
			expected:    "nginx.service",
			description: "Should extract service unit from cgroup v1 format",
		},
		{
			name: "systemd service with user slice",
			cgroupData: `11:devices:/user.slice/user-1000.slice/user@1000.service/app.slice/snap.code.code.service
10:blkio:/user.slice/user-1000.slice/user@1000.service/app.slice/snap.code.code.service
9:memory:/user.slice/user-1000.slice/user@1000.service/app.slice/snap.code.code.service
8:net_cls,net_prio:/
7:freezer:/
6:pids:/user.slice/user-1000.slice/user@1000.service/app.slice/snap.code.code.service
5:cpuset:/
4:cpu,cpuacct:/user.slice/user-1000.slice/user@1000.service/app.slice/snap.code.code.service
3:perf_event:/
2:hugetlb:/
1:name=systemd:/user.slice/user-1000.slice/user@1000.service/app.slice/snap.code.code.service
0::/user.slice/user-1000.slice/user@1000.service/app.slice/snap.code.code.service`,
			expected:    "snap.code.code.service",
			description: "Should extract service from user slice cgroup path",
		},
		{
			name:        "cgroup v2 unified hierarchy",
			cgroupData:  `0::/system.slice/docker.service/docker/a1b2c3d4e5f6`,
			expected:    "docker.service",
			description: "Should extract service from cgroup v2 unified hierarchy",
		},
		{
			name:        "container within systemd service",
			cgroupData:  `0::/system.slice/containerd.service/kubepods-besteffort-pod123.slice/cri-containerd-abc123.scope`,
			expected:    "containerd.service",
			description: "Should extract parent systemd service from container cgroup",
		},
		{
			name: "kubernetes pod cgroup",
			cgroupData: `11:devices:/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod123abc.slice/cri-containerd-456def.scope
10:blkio:/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod123abc.slice/cri-containerd-456def.scope
9:memory:/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod123abc.slice/cri-containerd-456def.scope
8:net_cls,net_prio:/
7:freezer:/
6:pids:/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod123abc.slice/cri-containerd-456def.scope
5:cpuset:/
4:cpu,cpuacct:/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod123abc.slice/cri-containerd-456def.scope
3:perf_event:/
2:hugetlb:/
1:name=systemd:/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod123abc.slice/cri-containerd-456def.scope
0::/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod123abc.slice/cri-containerd-456def.scope`,
			expected:    "cri-containerd-456def.scope",
			description: "Should extract container scope from k8s pod cgroup",
		},
		{
			name: "systemd timer unit",
			cgroupData: `11:devices:/system.slice/backup.timer
10:blkio:/system.slice/backup.timer
9:memory:/system.slice/backup.timer
8:net_cls,net_prio:/
7:freezer:/
6:pids:/system.slice/backup.timer
5:cpuset:/
4:cpu,cpuacct:/system.slice/backup.timer
3:perf_event:/
2:hugetlb:/
1:name=systemd:/system.slice/backup.timer
0::/system.slice/backup.timer`,
			expected:    "backup.timer",
			description: "Should extract timer unit from cgroup",
		},
		{
			name: "systemd socket unit",
			cgroupData: `1:name=systemd:/system.slice/docker.socket
0::/system.slice/docker.socket`,
			expected:    "docker.socket",
			description: "Should extract socket unit from cgroup",
		},
		{
			name: "systemd mount unit",
			cgroupData: `1:name=systemd:/system.slice/tmp.mount
0::/system.slice/tmp.mount`,
			expected:    "tmp.mount",
			description: "Should extract mount unit from cgroup",
		},
		{
			name: "systemd target unit",
			cgroupData: `1:name=systemd:/system.slice/multi-user.target
0::/system.slice/multi-user.target`,
			expected:    "multi-user.target",
			description: "Should extract target unit from cgroup",
		},
		{
			name: "nested docker container",
			cgroupData: `11:devices:/docker/a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0
10:blkio:/docker/a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0
9:memory:/docker/a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0
8:net_cls,net_prio:/
7:freezer:/docker/a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0
6:pids:/docker/a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0
5:cpuset:/docker/a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0
4:cpu,cpuacct:/docker/a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0
3:perf_event:/
2:hugetlb:/
1:name=systemd:/docker/a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0
0::/docker/a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0`,
			expected:    "unknown",
			description: "Should return unknown for direct docker containers without systemd service",
		},
		{
			name:        "empty cgroup data",
			cgroupData:  "",
			expected:    "unknown",
			description: "Should return unknown for empty cgroup data",
		},
		{
			name: "malformed cgroup line",
			cgroupData: `malformed-line-without-colons
1:name=systemd:/system.slice/test.service`,
			expected:    "test.service",
			description: "Should handle malformed lines gracefully",
		},
		{
			name: "init process (PID 1)",
			cgroupData: `11:devices:/
10:blkio:/
9:memory:/
8:net_cls,net_prio:/
7:freezer:/
6:pids:/
5:cpuset:/
4:cpu,cpuacct:/
3:perf_event:/
2:hugetlb:/
1:name=systemd:/
0::/`,
			expected:    "unknown",
			description: "Should return unknown for init process (PID 1) with root cgroups",
		},
		{
			name:        "cgroup v2 with scope unit",
			cgroupData:  `0::/user.slice/user-1000.slice/session-1.scope`,
			expected:    "session-1.scope",
			description: "Should extract scope unit from cgroup v2",
		},
		{
			name:        "deeply nested systemd service path",
			cgroupData:  `0::/system.slice/system-getty.slice/getty@tty1.service`,
			expected:    "getty@tty1.service",
			description: "Should extract template service instance from deep path",
		},
		{
			name: "special characters in unit name",
			cgroupData: `1:name=systemd:/system.slice/dbus-:1.2-com.example.Service@0.service
0::/system.slice/dbus-:1.2-com.example.Service@0.service`,
			expected:    "dbus-:1.2-com.example.Service@0.service",
			description: "Should handle special characters in unit names",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.extractUnitFromCgroup(tt.cgroupData)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

func TestIsValidSystemdProcess(t *testing.T) {
	collector := &Collector{}

	tests := []struct {
		name     string
		comm     string
		expected bool
	}{
		{
			name:     "systemd main process",
			comm:     "systemd",
			expected: true,
		},
		{
			name:     "systemd-logind",
			comm:     "systemd-logind",
			expected: true,
		},
		{
			name:     "systemd-resolve",
			comm:     "systemd-resolve",
			expected: true,
		},
		{
			name:     "systemd-networkd",
			comm:     "systemd-networkd",
			expected: true,
		},
		{
			name:     "systemd-journal",
			comm:     "systemd-journal",
			expected: true,
		},
		{
			name:     "regular process",
			comm:     "nginx",
			expected: false,
		},
		{
			name:     "empty string",
			comm:     "",
			expected: false,
		},
		{
			name:     "too long string",
			comm:     "systemd-this-is-way-too-long-to-be-valid",
			expected: false,
		},
		{
			name:     "partial match",
			comm:     "mysystemd",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.isValidSystemdProcess(tt.comm)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test for cgroup extraction edge cases and security
func TestExtractUnitFromCgroupEdgeCases(t *testing.T) {
	collector := &Collector{}

	tests := []struct {
		name        string
		cgroupData  string
		expected    string
		description string
	}{
		{
			name: "path traversal attempt",
			cgroupData: `1:name=systemd:/system.slice/../../../etc/passwd.service
0::/system.slice/test.service`,
			expected:    "test.service",
			description: "Should not be fooled by path traversal attempts",
		},
		{
			name: "extremely long path",
			cgroupData: func() string {
				longPath := "/system.slice/"
				for i := 0; i < 1000; i++ {
					longPath += "very-long-component-name/"
				}
				longPath += "test.service"
				return "1:name=systemd:" + longPath + "\n0::" + longPath
			}(),
			expected:    "test.service",
			description: "Should handle extremely long paths gracefully",
		},
		{
			name:        "null bytes in input",
			cgroupData:  "1:name=systemd:/system.slice/test\x00injection.service\n0::/system.slice/safe.service",
			expected:    "safe.service",
			description: "Should handle null bytes securely",
		},
		{
			name: "unicode in unit name",
			cgroupData: `1:name=systemd:/system.slice/测试.service
0::/system.slice/测试.service`,
			expected:    "测试.service",
			description: "Should handle unicode in unit names",
		},
		{
			name: "multiple unit types in path",
			cgroupData: `1:name=systemd:/system.slice/multi.service/test.timer
0::/system.slice/multi.service/test.timer`,
			expected:    "test.timer",
			description: "Should extract the deepest/most specific unit type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.extractUnitFromCgroup(tt.cgroupData)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}
