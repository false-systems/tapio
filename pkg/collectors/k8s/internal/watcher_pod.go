package internal

import (
	"github.com/yairfalse/tapio/pkg/collectors/k8s/core"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

// podWatcher watches Pod resources
type podWatcher struct {
	*baseWatcher
	clientset kubernetes.Interface
}

// newPodWatcher creates a new Pod watcher
func newPodWatcher(clientset kubernetes.Interface, config core.Config) core.ResourceWatcher {
	watcher := &podWatcher{
		baseWatcher: newBaseWatcher("Pod", config),
		clientset:   clientset,
	}
	
	// Create informer factory
	factory := informers.NewSharedInformerFactoryWithOptions(
		clientset,
		config.ResyncPeriod,
		informers.WithNamespace(config.Namespace),
	)
	
	// Create Pod informer
	watcher.informer = factory.Core().V1().Pods().Informer()
	
	return watcher
}