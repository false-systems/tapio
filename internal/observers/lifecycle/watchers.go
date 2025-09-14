package lifecycle

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

// setupWatchers creates informers for resources we care about
func (o *Observer) setupWatchers() error {
	// Watch Deployments
	deploymentInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return o.client.AppsV1().Deployments("").List(o.LifecycleManager.Context(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return o.client.AppsV1().Deployments("").Watch(o.LifecycleManager.Context(), options)
			},
		},
		&appsv1.Deployment{},
		30*60*1000, // 30 minute resync
		cache.Indexers{},
	)

	deploymentInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			// We don't care about additions - not breaking
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if transition := o.detector.DetectTransition("Deployment",
				oldObj.(runtime.Object), newObj.(runtime.Object)); transition != nil {
				o.handleTransition(transition)
			}
		},
		DeleteFunc: func(obj interface{}) {
			if transition := o.detector.DetectTransition("Deployment",
				obj.(runtime.Object), nil); transition != nil {
				o.handleTransition(transition)
			}
		},
	})
	o.informers = append(o.informers, deploymentInformer)

	// Watch StatefulSets
	statefulSetInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return o.client.AppsV1().StatefulSets("").List(o.LifecycleManager.Context(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return o.client.AppsV1().StatefulSets("").Watch(o.LifecycleManager.Context(), options)
			},
		},
		&appsv1.StatefulSet{},
		30*60*1000,
		cache.Indexers{},
	)

	statefulSetInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			if transition := o.detector.DetectTransition("StatefulSet",
				oldObj.(runtime.Object), newObj.(runtime.Object)); transition != nil {
				o.handleTransition(transition)
			}
		},
		DeleteFunc: func(obj interface{}) {
			if transition := o.detector.DetectTransition("StatefulSet",
				obj.(runtime.Object), nil); transition != nil {
				o.handleTransition(transition)
			}
		},
	})
	o.informers = append(o.informers, statefulSetInformer)

	// Watch Pods for OOM, evictions, crash loops
	podInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return o.client.CoreV1().Pods("").List(o.LifecycleManager.Context(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return o.client.CoreV1().Pods("").Watch(o.LifecycleManager.Context(), options)
			},
		},
		&corev1.Pod{},
		30*60*1000,
		cache.Indexers{},
	)

	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			if transition := o.detector.DetectTransition("Pod",
				oldObj.(runtime.Object), newObj.(runtime.Object)); transition != nil {
				o.handleTransition(transition)
			}
		},
		DeleteFunc: func(obj interface{}) {
			// Check if eviction
			if pod, ok := obj.(*corev1.Pod); ok && pod.Status.Reason == "Evicted" {
				if transition := o.detector.DetectTransition("Pod",
					obj.(runtime.Object), nil); transition != nil {
					o.handleTransition(transition)
				}
			}
		},
	})
	o.informers = append(o.informers, podInformer)

	// Watch Nodes for pressure, not ready
	nodeInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return o.client.CoreV1().Nodes().List(o.LifecycleManager.Context(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return o.client.CoreV1().Nodes().Watch(o.LifecycleManager.Context(), options)
			},
		},
		&corev1.Node{},
		30*60*1000,
		cache.Indexers{},
	)

	nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			if transition := o.detector.DetectTransition("Node",
				oldObj.(runtime.Object), newObj.(runtime.Object)); transition != nil {
				o.handleTransition(transition)
			}
		},
	})
	o.informers = append(o.informers, nodeInformer)

	o.logger.Info(fmt.Sprintf("Set up %d watchers", len(o.informers)))
	return nil
}
