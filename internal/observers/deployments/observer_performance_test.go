package deployments

import (
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

// TDD: Performance benchmarks

func BenchmarkDetectEventType(b *testing.B) {
	old := createDeployment("app", 1, 1)
	new := createDeployment("app", 1, 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		detectEventType(old, new)
	}
}

func BenchmarkDetectReplicaChange(b *testing.B) {
	old := createDeployment("app", 1, 1)
	new := createDeployment("app", 5, 5)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		detectReplicaChange(old, new)
	}
}

func BenchmarkDetectConditionChange(b *testing.B) {
	old := createDeploymentWithCondition("app", "Available", "False")
	new := createDeploymentWithCondition("app", "Available", "True")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		detectConditionChange(old, new)
	}
}

func BenchmarkCreateDomainEvent(b *testing.B) {
	old := createDeployment("app", 1, 1)
	new := createDeployment("app", 5, 5)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		createDomainEvent(old, new)
	}
}

func BenchmarkHandleAdd(b *testing.B) {
	clientset := fake.NewSimpleClientset()
	emitter := &captureEmitter{events: make([]*capturedEvent, 0, b.N)}

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	if err != nil {
		b.Fatal(err)
	}

	deploy := createDeployment("app", 3, 3)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		observer.handleAdd(deploy)
	}
}

func BenchmarkHandleUpdate(b *testing.B) {
	clientset := fake.NewSimpleClientset()
	emitter := &captureEmitter{events: make([]*capturedEvent, 0, b.N)}

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	if err != nil {
		b.Fatal(err)
	}

	old := createDeployment("app", 1, 1)
	new := createDeployment("app", 5, 5)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		observer.handleUpdate(old, new)
	}
}

func BenchmarkHandleDelete(b *testing.B) {
	clientset := fake.NewSimpleClientset()
	emitter := &captureEmitter{events: make([]*capturedEvent, 0, b.N)}

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	if err != nil {
		b.Fatal(err)
	}

	deploy := createDeployment("app", 3, 3)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		observer.handleDelete(deploy)
	}
}

// Benchmark full event processing pipeline
func BenchmarkFullEventPipeline(b *testing.B) {
	clientset := fake.NewSimpleClientset()
	emitter := &captureEmitter{events: make([]*capturedEvent, 0, b.N)}

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	_, err := NewDeploymentsObserver("deployments", config)
	if err != nil {
		b.Fatal(err)
	}

	old := createDeployment("app", 1, 1)
	new := createDeployment("app", 5, 5)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate full event processing
		evt := createDomainEvent(old, new)
		if err := emitter.Emit(nil, evt); err != nil {
			b.Fatal(err)
		}
	}
}
