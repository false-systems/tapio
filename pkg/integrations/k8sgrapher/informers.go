package k8sgrapher

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/cache"
)

// startServiceInformer creates an informer for Services
func (g *K8sGrapher) startServiceInformer() {
	listWatcher := cache.NewListWatchFromClient(
		g.kubeClient.CoreV1().RESTClient(),
		"services",
		g.namespace,
		fields.Everything(),
	)

	informer := cache.NewSharedIndexInformer(
		listWatcher,
		&corev1.Service{},
		g.resyncPeriod,
		cache.Indexers{},
	)

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			svc := obj.(*corev1.Service)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "service"),
					attribute.String("event", "add"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "create",
				nodeType:  "Service",
				data:      svc,
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			svc := newObj.(*corev1.Service)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "service"),
					attribute.String("event", "update"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "update",
				nodeType:  "Service",
				data:      svc,
			}
		},
		DeleteFunc: func(obj interface{}) {
			svc := obj.(*corev1.Service)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "service"),
					attribute.String("event", "delete"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "delete",
				nodeType:  "Service",
				data:      svc,
			}
		},
	})

	g.informers["services"] = informer
}

// startPodInformer creates an informer for Pods
func (g *K8sGrapher) startPodInformer() {
	listWatcher := cache.NewListWatchFromClient(
		g.kubeClient.CoreV1().RESTClient(),
		"pods",
		g.namespace,
		fields.Everything(),
	)

	informer := cache.NewSharedIndexInformer(
		listWatcher,
		&corev1.Pod{},
		g.resyncPeriod,
		cache.Indexers{},
	)

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod := obj.(*corev1.Pod)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "pod"),
					attribute.String("event", "add"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "create",
				nodeType:  "Pod",
				data:      pod,
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			pod := newObj.(*corev1.Pod)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "pod"),
					attribute.String("event", "update"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "update",
				nodeType:  "Pod",
				data:      pod,
			}
		},
		DeleteFunc: func(obj interface{}) {
			pod := obj.(*corev1.Pod)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "pod"),
					attribute.String("event", "delete"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "delete",
				nodeType:  "Pod",
				data:      pod,
			}
		},
	})

	g.informers["pods"] = informer
}

// startConfigMapInformer creates an informer for ConfigMaps
func (g *K8sGrapher) startConfigMapInformer() {
	listWatcher := cache.NewListWatchFromClient(
		g.kubeClient.CoreV1().RESTClient(),
		"configmaps",
		g.namespace,
		fields.Everything(),
	)

	informer := cache.NewSharedIndexInformer(
		listWatcher,
		&corev1.ConfigMap{},
		g.resyncPeriod,
		cache.Indexers{},
	)

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			cm := obj.(*corev1.ConfigMap)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "configmap"),
					attribute.String("event", "add"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "create",
				nodeType:  "ConfigMap",
				data:      cm,
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			cm := newObj.(*corev1.ConfigMap)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "configmap"),
					attribute.String("event", "update"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "update",
				nodeType:  "ConfigMap",
				data:      cm,
			}
		},
		DeleteFunc: func(obj interface{}) {
			cm := obj.(*corev1.ConfigMap)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "configmap"),
					attribute.String("event", "delete"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "delete",
				nodeType:  "ConfigMap",
				data:      cm,
			}
		},
	})

	g.informers["configmaps"] = informer
}

// startSecretInformer creates an informer for Secrets
func (g *K8sGrapher) startSecretInformer() {
	listWatcher := cache.NewListWatchFromClient(
		g.kubeClient.CoreV1().RESTClient(),
		"secrets",
		g.namespace,
		fields.Everything(),
	)

	informer := cache.NewSharedIndexInformer(
		listWatcher,
		&corev1.Secret{},
		g.resyncPeriod,
		cache.Indexers{},
	)

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			secret := obj.(*corev1.Secret)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "secret"),
					attribute.String("event", "add"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "create",
				nodeType:  "Secret",
				data:      secret,
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			secret := newObj.(*corev1.Secret)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "secret"),
					attribute.String("event", "update"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "update",
				nodeType:  "Secret",
				data:      secret,
			}
		},
		DeleteFunc: func(obj interface{}) {
			secret := obj.(*corev1.Secret)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "secret"),
					attribute.String("event", "delete"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "delete",
				nodeType:  "Secret",
				data:      secret,
			}
		},
	})

	g.informers["secrets"] = informer
}

// startDeploymentInformer creates an informer for Deployments
func (g *K8sGrapher) startDeploymentInformer() {
	listWatcher := cache.NewListWatchFromClient(
		g.kubeClient.AppsV1().RESTClient(),
		"deployments",
		g.namespace,
		fields.Everything(),
	)

	informer := cache.NewSharedIndexInformer(
		listWatcher,
		&appsv1.Deployment{},
		g.resyncPeriod,
		cache.Indexers{},
	)

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			deploy := obj.(*appsv1.Deployment)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "deployment"),
					attribute.String("event", "add"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "create",
				nodeType:  "Deployment",
				data:      deploy,
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			deploy := newObj.(*appsv1.Deployment)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "deployment"),
					attribute.String("event", "update"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "update",
				nodeType:  "Deployment",
				data:      deploy,
			}
		},
		DeleteFunc: func(obj interface{}) {
			deploy := obj.(*appsv1.Deployment)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "deployment"),
					attribute.String("event", "delete"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "delete",
				nodeType:  "Deployment",
				data:      deploy,
			}
		},
	})

	g.informers["deployments"] = informer
}

// startReplicaSetInformer creates an informer for ReplicaSets
func (g *K8sGrapher) startReplicaSetInformer() {
	listWatcher := cache.NewListWatchFromClient(
		g.kubeClient.AppsV1().RESTClient(),
		"replicasets",
		g.namespace,
		fields.Everything(),
	)

	informer := cache.NewSharedIndexInformer(
		listWatcher,
		&appsv1.ReplicaSet{},
		g.resyncPeriod,
		cache.Indexers{},
	)

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			rs := obj.(*appsv1.ReplicaSet)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "replicaset"),
					attribute.String("event", "add"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "create",
				nodeType:  "ReplicaSet",
				data:      rs,
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			rs := newObj.(*appsv1.ReplicaSet)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "replicaset"),
					attribute.String("event", "update"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "update",
				nodeType:  "ReplicaSet",
				data:      rs,
			}
		},
		DeleteFunc: func(obj interface{}) {
			rs := obj.(*appsv1.ReplicaSet)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "replicaset"),
					attribute.String("event", "delete"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "delete",
				nodeType:  "ReplicaSet",
				data:      rs,
			}
		},
	})

	g.informers["replicasets"] = informer
}

// startPVCInformer creates an informer for PersistentVolumeClaims
func (g *K8sGrapher) startPVCInformer() {
	listWatcher := cache.NewListWatchFromClient(
		g.kubeClient.CoreV1().RESTClient(),
		"persistentvolumeclaims",
		g.namespace,
		fields.Everything(),
	)

	informer := cache.NewSharedIndexInformer(
		listWatcher,
		&corev1.PersistentVolumeClaim{},
		g.resyncPeriod,
		cache.Indexers{},
	)

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pvc := obj.(*corev1.PersistentVolumeClaim)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "pvc"),
					attribute.String("event", "add"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "create",
				nodeType:  "PVC",
				data:      pvc,
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			pvc := newObj.(*corev1.PersistentVolumeClaim)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "pvc"),
					attribute.String("event", "update"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "update",
				nodeType:  "PVC",
				data:      pvc,
			}
		},
		DeleteFunc: func(obj interface{}) {
			pvc := obj.(*corev1.PersistentVolumeClaim)
			g.instrumentation.K8sWatchEvents.Add(g.ctx(), 1,
				metric.WithAttributes(
					attribute.String("resource", "pvc"),
					attribute.String("event", "delete"),
				))
			g.graphUpdateChan <- graphUpdate{
				operation: "delete",
				nodeType:  "PVC",
				data:      pvc,
			}
		},
	})

	g.informers["pvcs"] = informer
}

// ctx returns a context for metrics recording
func (g *K8sGrapher) ctx() context.Context {
	// In production, this could include trace context
	return context.Background()
}
