//go:build test || integration
// +build test integration

package simple

import (
	"context"

	"github.com/yairfalse/tapio/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// MockChecker implements CheckerInterface for testing
type MockChecker struct {
	CheckFunc   func(ctx context.Context, req *types.CheckRequest) (*types.CheckResult, error)
	EBPFMonitor interface{}
	KubeClient  kubernetes.Interface
}

// Check implements CheckerInterface
func (m *MockChecker) Check(ctx context.Context, req *types.CheckRequest) (*types.CheckResult, error) {
	if m.CheckFunc != nil {
		return m.CheckFunc(ctx, req)
	}
	// Return empty result by default
	return &types.CheckResult{
		Problems:   []types.Problem{},
		Namespaces: []string{"default"},
		TotalPods:  0,
	}, nil
}

// GetEBPFMonitor returns the mock eBPF monitor
func (m *MockChecker) GetEBPFMonitor() interface{} {
	return m.EBPFMonitor
}

// GetKubeClient returns the mock Kubernetes client
func (m *MockChecker) GetKubeClient() interface{} {
	return m.KubeClient
}
