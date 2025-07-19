package internal

import (
	"github.com/yairfalse/tapio/pkg/collectors/k8s/core"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

// nodeWatcher watches Node resources
type nodeWatcher struct {
	*baseWatcher
	clientset kubernetes.Interface
}

func newNodeWatcher(clientset kubernetes.Interface, config core.Config) core.ResourceWatcher {
	watcher := &nodeWatcher{
		baseWatcher: newBaseWatcher("Node", config),
		clientset:   clientset,
	}

	factory := informers.NewSharedInformerFactory(clientset, config.ResyncPeriod)
	watcher.informer = factory.Core().V1().Nodes().Informer()

	return watcher
}

// serviceWatcher watches Service resources
type serviceWatcher struct {
	*baseWatcher
	clientset kubernetes.Interface
}

func newServiceWatcher(clientset kubernetes.Interface, config core.Config) core.ResourceWatcher {
	watcher := &serviceWatcher{
		baseWatcher: newBaseWatcher("Service", config),
		clientset:   clientset,
	}

	factory := informers.NewSharedInformerFactoryWithOptions(
		clientset,
		config.ResyncPeriod,
		informers.WithNamespace(config.Namespace),
	)
	watcher.informer = factory.Core().V1().Services().Informer()

	return watcher
}

// deploymentWatcher watches Deployment resources
type deploymentWatcher struct {
	*baseWatcher
	clientset kubernetes.Interface
}

func newDeploymentWatcher(clientset kubernetes.Interface, config core.Config) core.ResourceWatcher {
	watcher := &deploymentWatcher{
		baseWatcher: newBaseWatcher("Deployment", config),
		clientset:   clientset,
	}

	factory := informers.NewSharedInformerFactoryWithOptions(
		clientset,
		config.ResyncPeriod,
		informers.WithNamespace(config.Namespace),
	)
	watcher.informer = factory.Apps().V1().Deployments().Informer()

	return watcher
}

// eventWatcher watches Event resources
type eventWatcher struct {
	*baseWatcher
	clientset kubernetes.Interface
}

func newEventWatcher(clientset kubernetes.Interface, config core.Config) core.ResourceWatcher {
	watcher := &eventWatcher{
		baseWatcher: newBaseWatcher("Event", config),
		clientset:   clientset,
	}

	factory := informers.NewSharedInformerFactoryWithOptions(
		clientset,
		config.ResyncPeriod,
		informers.WithNamespace(config.Namespace),
	)
	watcher.informer = factory.Core().V1().Events().Informer()

	return watcher
}

// configMapWatcher watches ConfigMap resources
type configMapWatcher struct {
	*baseWatcher
	clientset kubernetes.Interface
}

func newConfigMapWatcher(clientset kubernetes.Interface, config core.Config) core.ResourceWatcher {
	watcher := &configMapWatcher{
		baseWatcher: newBaseWatcher("ConfigMap", config),
		clientset:   clientset,
	}

	factory := informers.NewSharedInformerFactoryWithOptions(
		clientset,
		config.ResyncPeriod,
		informers.WithNamespace(config.Namespace),
	)
	watcher.informer = factory.Core().V1().ConfigMaps().Informer()

	return watcher
}

// secretWatcher watches Secret resources
type secretWatcher struct {
	*baseWatcher
	clientset kubernetes.Interface
}

func newSecretWatcher(clientset kubernetes.Interface, config core.Config) core.ResourceWatcher {
	watcher := &secretWatcher{
		baseWatcher: newBaseWatcher("Secret", config),
		clientset:   clientset,
	}

	factory := informers.NewSharedInformerFactoryWithOptions(
		clientset,
		config.ResyncPeriod,
		informers.WithNamespace(config.Namespace),
	)
	watcher.informer = factory.Core().V1().Secrets().Informer()

	return watcher
}
