package deployments

import (
	"context"
	"testing"

	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/intelligence"
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
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	config := Config{
		Clientset: clientset,
		Namespace: "default",
	}

	observer, err := New(config, deps)
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
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	config := Config{
		Clientset: clientset,
		Namespace: "default",
	}

	observer, err := New(config, deps)
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
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	config := Config{
		Clientset: clientset,
		Namespace: "default",
	}

	observer, err := New(config, deps)
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
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	config := Config{
		Clientset: clientset,
		Namespace: "default",
	}

	_, err := New(config, deps)
	if err != nil {
		b.Fatal(err)
	}

	old := createDeployment("app", 1, 1)
	new := createDeployment("app", 5, 5)

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate full event processing
		evt := createDomainEvent(old, new)
		if err := emitter.Emit(ctx, evt); err != nil {
			b.Fatal(err)
		}
	}
}
