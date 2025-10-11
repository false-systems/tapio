package k8scontext

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BenchmarkHandlerThroughput benchmarks event handler throughput
func BenchmarkHandlerThroughput(b *testing.B) {
	benchmarks := []struct {
		name     string
		resource interface{}
		handler  func(*Service, interface{})
	}{
		{
			name: "PodAdd",
			resource: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
				Status:     corev1.PodStatus{PodIP: "10.0.0.1"},
			},
			handler: func(s *Service, obj interface{}) { s.handlePodAdd(obj) },
		},
		{
			name: "ServiceAdd",
			resource: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "test-svc", Namespace: "default"},
				Spec:       corev1.ServiceSpec{ClusterIP: "10.96.0.1"},
			},
			handler: func(s *Service, obj interface{}) { s.handleServiceAdd(obj) },
		},
		{
			name: "DeploymentAdd",
			resource: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "test-deploy", Namespace: "default"},
				Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(3)},
			},
			handler: func(s *Service, obj interface{}) { s.handleDeploymentAdd(obj) },
		},
		{
			name: "NodeAdd",
			resource: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
			},
			handler: func(s *Service, obj interface{}) { s.handleNodeAdd(obj) },
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			mockKV := newMockKV()
			service := &Service{
				kv:          mockKV,
				eventBuffer: make(chan func() error, 10000), // Large buffer to not block
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				bm.handler(service, bm.resource)
			}
		})
	}
}

// BenchmarkStorageOperations benchmarks KV storage operations
func BenchmarkStorageOperations(b *testing.B) {
	benchmarks := []struct {
		name string
		op   func(*Service, *testing.B)
	}{
		{
			name: "StorePod",
			op: func(s *Service, b *testing.B) {
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
					Status:     corev1.PodStatus{PodIP: "10.0.0.1"},
				}
				for i := 0; i < b.N; i++ {
					if err := s.storePodMetadata(pod); err != nil {
						b.Fatal(err)
					}
				}
			},
		},
		{
			name: "StoreService",
			op: func(s *Service, b *testing.B) {
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "test-svc", Namespace: "default"},
					Spec:       corev1.ServiceSpec{ClusterIP: "10.96.0.1"},
				}
				for i := 0; i < b.N; i++ {
					if err := s.storeServiceMetadata(svc); err != nil {
						b.Fatal(err)
					}
				}
			},
		},
		{
			name: "StoreDeployment",
			op: func(s *Service, b *testing.B) {
				deployment := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "test-deploy", Namespace: "default"},
					Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(3)},
				}
				for i := 0; i < b.N; i++ {
					if err := s.storeDeploymentMetadata(deployment); err != nil {
						b.Fatal(err)
					}
				}
			},
		},
		{
			name: "StoreOwnership",
			op: func(s *Service, b *testing.B) {
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
						UID:       "pod-123",
						OwnerReferences: []metav1.OwnerReference{
							{Kind: "ReplicaSet", Name: "test-rs"},
						},
					},
				}
				for i := 0; i < b.N; i++ {
					if err := s.storeOwnerMetadata(pod); err != nil {
						b.Fatal(err)
					}
				}
			},
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			mockKV := newMockKV()
			service := &Service{kv: mockKV}

			b.ResetTimer()
			b.ReportAllocs()

			bm.op(service, b)
		})
	}
}

// BenchmarkTransformations benchmarks data transformation functions
func BenchmarkTransformations(b *testing.B) {
	benchmarks := []struct {
		name string
		op   func(*testing.B)
	}{
		{
			name: "ToPodInfo",
			op: func(b *testing.B) {
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
						Labels:    map[string]string{"app": "test", "version": "v1"},
					},
					Status: corev1.PodStatus{PodIP: "10.0.0.1", HostIP: "192.168.1.1"},
				}
				for i := 0; i < b.N; i++ {
					toPodInfo(pod)
				}
			},
		},
		{
			name: "ToDeploymentInfo",
			op: func(b *testing.B) {
				replicas := int32(3)
				deployment := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-deploy",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: &replicas,
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Image: "nginx:1.21"},
								},
							},
						},
					},
				}
				for i := 0; i < b.N; i++ {
					toDeploymentInfo(deployment)
				}
			},
		},
		{
			name: "ToOwnerInfo",
			op: func(b *testing.B) {
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						OwnerReferences: []metav1.OwnerReference{
							{Kind: "ReplicaSet", Name: "test-rs"},
							{Kind: "Deployment", Name: "test-deploy"},
						},
					},
				}
				for i := 0; i < b.N; i++ {
					toOwnerInfo(pod)
				}
			},
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			bm.op(b)
		})
	}
}

// BenchmarkSerialization benchmarks JSON serialization
func BenchmarkSerialization(b *testing.B) {
	benchmarks := []struct {
		name string
		op   func(*testing.B)
	}{
		{
			name: "SerializePodInfo",
			op: func(b *testing.B) {
				info := PodInfo{
					Name:      "test-pod",
					Namespace: "default",
					PodIP:     "10.0.0.1",
					HostIP:    "192.168.1.1",
					Labels:    map[string]string{"app": "test", "version": "v1"},
				}
				for i := 0; i < b.N; i++ {
					if _, err := serializePodInfo(info); err != nil {
						b.Fatal(err)
					}
				}
			},
		},
		{
			name: "SerializeDeploymentInfo",
			op: func(b *testing.B) {
				info := DeploymentInfo{
					Name:      "test-deploy",
					Namespace: "default",
					Replicas:  3,
					Image:     "nginx:1.21",
					Labels:    map[string]string{"app": "test"},
				}
				for i := 0; i < b.N; i++ {
					if err := serializeDeploymentInfo(info); err != nil {
						b.Fatalf("serializeDeploymentInfo failed: %v", err)
					}
				}
			},
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			bm.op(b)
		})
	}
}

// BenchmarkConcurrentLoad benchmarks concurrent event processing
func BenchmarkConcurrentLoad(b *testing.B) {
	sizes := []int{10, 100, 1000}

	for _, size := range sizes {
		b.Run("Events"+intToString(size), func(b *testing.B) {
			mockKV := newMockKV()
			service := &Service{
				kv:          mockKV,
				eventBuffer: make(chan func() error, size*2),
			}

			pods := make([]*corev1.Pod, size)
			for i := 0; i < size; i++ {
				pods[i] = &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-" + intToString(i),
						Namespace: "default",
					},
					Status: corev1.PodStatus{
						PodIP: "10.0.0." + intToString(i%256),
					},
				}
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				for _, pod := range pods {
					service.handlePodAdd(pod)
				}
			}
		})
	}
}

// BenchmarkBufferEnqueue benchmarks event buffer enqueue performance
func BenchmarkBufferEnqueue(b *testing.B) {
	bufferSizes := []int{10, 100, 1000, 10000}

	for _, size := range bufferSizes {
		b.Run("Buffer"+intToString(size), func(b *testing.B) {
			mockKV := newMockKV()
			service := &Service{
				kv:          mockKV,
				eventBuffer: make(chan func() error, size),
			}

			noopEvent := func() error { return nil }

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				service.enqueueEvent(noopEvent)
			}
		})
	}
}

// Helper functions
func int32Ptr(i int32) *int32 {
	return &i
}

func intToString(i int) string {
	// Simple int to string for benchmark names
	if i < 10 {
		return string(rune('0' + i))
	}
	// For larger numbers, use a simple approach
	result := ""
	for i > 0 {
		result = string(rune('0'+(i%10))) + result
		i /= 10
	}
	if result == "" {
		return "0"
	}
	return result
}
