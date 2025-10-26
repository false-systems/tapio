package operator

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	tapiov1alpha1 "github.com/yairfalse/tapio/api/v1alpha1"
)

func (r *TapioObserverReconciler) reconcileDaemonSet(ctx context.Context, observer *tapiov1alpha1.TapioObserver) error {
	ds := buildDaemonSet(observer)

	if err := ctrl.SetControllerReference(observer, ds, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	existing := &appsv1.DaemonSet{}
	err := r.Get(ctx, types.NamespacedName{Name: ds.Name, Namespace: ds.Namespace}, existing)
	if errors.IsNotFound(err) {
		if err := r.Create(ctx, ds); err != nil {
			return fmt.Errorf("failed to create DaemonSet: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get DaemonSet: %w", err)
	}

	existing.Spec = ds.Spec
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update DaemonSet: %w", err)
	}

	return nil
}

func buildDaemonSet(observer *tapiov1alpha1.TapioObserver) *appsv1.DaemonSet {
	labels := map[string]string{
		"app":                          "tapio-observer",
		"tapio.io/observer":            observer.Name,
		"app.kubernetes.io/name":       "tapio-observer",
		"app.kubernetes.io/managed-by": "tapio-operator",
	}

	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      observer.Name,
			Namespace: observer.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       buildPodSpec(observer),
			},
		},
	}
}

func buildPodSpec(observer *tapiov1alpha1.TapioObserver) corev1.PodSpec {
	return corev1.PodSpec{
		ServiceAccountName: observer.Name,
		HostNetwork:        true,
		Containers:         []corev1.Container{buildContainer(observer)},
		Volumes:            buildVolumes(observer),
	}
}

func buildContainer(observer *tapiov1alpha1.TapioObserver) corev1.Container {
	privileged := false
	bidirectional := corev1.MountPropagationBidirectional

	return corev1.Container{
		Name:            "tapio-observer",
		Image:           observer.Spec.Image,
		ImagePullPolicy: observer.Spec.ImagePullPolicy,
		SecurityContext: &corev1.SecurityContext{
			Privileged: &privileged,
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{"SYS_ADMIN", "NET_ADMIN", "SYS_RESOURCE"},
			},
		},
		Resources: observer.Spec.Resources,
		VolumeMounts: []corev1.VolumeMount{
			{Name: "config", MountPath: "/etc/tapio", ReadOnly: true},
			{Name: "bpf-maps", MountPath: "/sys/fs/bpf", MountPropagation: &bidirectional},
		},
	}
}

func buildVolumes(observer *tapiov1alpha1.TapioObserver) []corev1.Volume {
	dirOrCreate := corev1.HostPathDirectoryOrCreate
	return []corev1.Volume{
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: observer.Name + "-config",
					},
				},
			},
		},
		{
			Name: "bpf-maps",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/sys/fs/bpf",
					Type: &dirOrCreate,
				},
			},
		},
	}
}
