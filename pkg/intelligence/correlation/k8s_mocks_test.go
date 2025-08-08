package correlation

import (
	"context"

	"github.com/stretchr/testify/mock"
	"github.com/yairfalse/tapio/pkg/domain"
)

// MockK8sClient for testing - implements domain.K8sClient interface
type MockK8sClient struct {
	mock.Mock
}

func (m *MockK8sClient) GetPod(ctx context.Context, namespace, name string) (*domain.K8sPod, error) {
	args := m.Called(ctx, namespace, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.K8sPod), args.Error(1)
}

func (m *MockK8sClient) GetService(ctx context.Context, namespace, name string) (*domain.K8sService, error) {
	args := m.Called(ctx, namespace, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.K8sService), args.Error(1)
}

func (m *MockK8sClient) GetDeployment(ctx context.Context, namespace, name string) (*domain.K8sDeployment, error) {
	args := m.Called(ctx, namespace, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.K8sDeployment), args.Error(1)
}

func (m *MockK8sClient) GetReplicaSet(ctx context.Context, namespace, name string) (*domain.K8sReplicaSet, error) {
	args := m.Called(ctx, namespace, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.K8sReplicaSet), args.Error(1)
}

func (m *MockK8sClient) GetStatefulSet(ctx context.Context, namespace, name string) (*domain.K8sStatefulSet, error) {
	args := m.Called(ctx, namespace, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.K8sStatefulSet), args.Error(1)
}

func (m *MockK8sClient) GetDaemonSet(ctx context.Context, namespace, name string) (*domain.K8sDaemonSet, error) {
	args := m.Called(ctx, namespace, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.K8sDaemonSet), args.Error(1)
}

func (m *MockK8sClient) ListPods(ctx context.Context, namespace string, selector map[string]string) ([]*domain.K8sPod, error) {
	args := m.Called(ctx, namespace, selector)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.K8sPod), args.Error(1)
}

func (m *MockK8sClient) ListServices(ctx context.Context, namespace string, selector map[string]string) ([]*domain.K8sService, error) {
	args := m.Called(ctx, namespace, selector)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.K8sService), args.Error(1)
}

func (m *MockK8sClient) WatchPods(ctx context.Context, namespace string) (<-chan domain.K8sWatchEvent, error) {
	args := m.Called(ctx, namespace)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(<-chan domain.K8sWatchEvent), args.Error(1)
}

func (m *MockK8sClient) WatchServices(ctx context.Context, namespace string) (<-chan domain.K8sWatchEvent, error) {
	args := m.Called(ctx, namespace)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(<-chan domain.K8sWatchEvent), args.Error(1)
}

func (m *MockK8sClient) GetEvents(ctx context.Context, involvedObjectKind, namespace, name string) ([]*domain.K8sEvent, error) {
	args := m.Called(ctx, involvedObjectKind, namespace, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.K8sEvent), args.Error(1)
}

func (m *MockK8sClient) GetOwnerReferences(ctx context.Context, kind, namespace, name string) ([]domain.K8sOwnerReference, error) {
	args := m.Called(ctx, kind, namespace, name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.K8sOwnerReference), args.Error(1)
}
