package deployments

import (
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func BenchmarkShouldTrackDeployment(b *testing.B) {
	config := DefaultConfig()
	config.MockMode = true
	config.Namespaces = []string{"production", "staging"}
	config.AnnotationFilter = "tapio.io/monitor"

	observer, err := NewObserver("bench", config)
	if err != nil {
		b.Fatal(err)
	}

	deployment := createTestDeployment("test-app", "production")
	deployment.Annotations = map[string]string{
		"tapio.io/monitor": "true",
		"other-annotation": "value",
	}

	b.ResetTimer()
	var result bool
	for i := 0; i < b.N; i++ {
		result = observer.shouldTrackDeployment(deployment)
	}
	if result {
		// Prevent compiler optimization
	}
}

func BenchmarkHasSignificantChange(b *testing.B) {
	config := DefaultConfig()
	config.MockMode = true
	observer, err := NewObserver("bench", config)
	if err != nil {
		b.Fatal(err)
	}

	oldDep := createTestDeployment("test-app", "default")
	newDep := createTestDeployment("test-app", "default")
	newDep.Spec.Template.Spec.Containers[0].Image = "nginx:1.20"

	b.ResetTimer()
	var result bool
	for i := 0; i < b.N; i++ {
		result = observer.hasSignificantChange(oldDep, newDep)
	}
	if result {
		// Prevent compiler optimization
	}
}

func BenchmarkCreateDeploymentEvent(b *testing.B) {
	config := DefaultConfig()
	config.MockMode = true
	observer, err := NewObserver("bench", config)
	if err != nil {
		b.Fatal(err)
	}

	deployment := createTestDeployment("test-app", "default")
	oldDeployment := createTestDeployment("test-app", "default")

	b.ResetTimer()
	var event *domain.CollectorEvent
	for i := 0; i < b.N; i++ {
		event = observer.createDeploymentEvent(deployment, "updated", oldDeployment)
	}
	if event != nil {
		// Prevent compiler optimization
	}
}

func BenchmarkEventDeduplication(b *testing.B) {
	config := DefaultConfig()
	config.MockMode = true
	config.DeduplicationWindow = 100 * time.Millisecond
	observer, err := NewObserver("bench", config)
	if err != nil {
		b.Fatal(err)
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event := observer.createDeploymentEvent(deployment, "updated", nil)
		observer.sendEvent(event)
	}
}
