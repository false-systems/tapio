//go:build linux
// +build linux

package systemd

import (
	"testing"
)

func TestKubernetesServiceFiltering(t *testing.T) {
	tests := []struct {
		serviceName string
		command     string
		expected    bool
	}{
		{"kubelet.service", "/usr/bin/kubelet", true},
		{"kube-proxy.service", "/usr/bin/kube-proxy", true},
		{"containerd.service", "/usr/bin/containerd", true},
		{"docker.service", "/usr/bin/dockerd", true},
		{"etcd.service", "/usr/bin/etcd", true},
		{"apache2.service", "/usr/bin/apache2", false},
		{"mysql.service", "/usr/bin/mysqld", false},
		{"nginx.service", "/usr/bin/nginx", false},
		{"unknown.service", "/usr/bin/calico-node", true}, // Command contains K8s component
	}

	for _, test := range tests {
		result := isKubernetesService(test.serviceName, test.command)
		if result != test.expected {
			t.Errorf("isKubernetesService(%s, %s) = %v, expected %v",
				test.serviceName, test.command, result, test.expected)
		}
	}
}

func TestServiceEventTypeConversion(t *testing.T) {
	tests := []struct {
		eventType uint32
		expected  string
	}{
		{ServiceEventStart, "service_start"},
		{ServiceEventStop, "service_stop"},
		{ServiceEventRestart, "service_restart"},
		{ServiceEventFailed, "service_failed"},
		{ServiceEventSyscall, "service_syscall"},
		{999, "unknown"},
	}

	for _, test := range tests {
		result := getServiceEventType(test.eventType)
		if result != test.expected {
			t.Errorf("getServiceEventType(%d) = %s, expected %s",
				test.eventType, result, test.expected)
		}
	}
}
