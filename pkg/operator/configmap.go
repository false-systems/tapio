package operator

import (
	"context"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	tapiov1alpha1 "github.com/yairfalse/tapio/api/v1alpha1"
)

func (r *TapioObserverReconciler) reconcileConfigMap(ctx context.Context, observer *tapiov1alpha1.TapioObserver) error {
	data := make(map[string]string)
	data["otlp_endpoint"] = observer.Spec.OTLPEndpoint
	data["otlp_insecure"] = strconv.FormatBool(observer.Spec.OTLPInsecure)

	if observer.Spec.NetworkObserver != nil {
		data["network_enabled"] = strconv.FormatBool(observer.Spec.NetworkObserver.Enabled)
		if observer.Spec.NetworkObserver.InterfaceFilter != "" {
			data["network_interface_filter"] = observer.Spec.NetworkObserver.InterfaceFilter
		}
		if observer.Spec.NetworkObserver.BufferSize > 0 {
			data["network_buffer_size"] = strconv.FormatInt(int64(observer.Spec.NetworkObserver.BufferSize), 10)
		}
		if observer.Spec.NetworkObserver.MapSize > 0 {
			data["network_map_size"] = strconv.FormatInt(int64(observer.Spec.NetworkObserver.MapSize), 10)
		}
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      observer.Name + "-config",
			Namespace: observer.Namespace,
		},
		Data: data,
	}

	if err := ctrl.SetControllerReference(observer, cm, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, existing)
	if errors.IsNotFound(err) {
		if err := r.Create(ctx, cm); err != nil {
			return fmt.Errorf("failed to create ConfigMap: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	existing.Data = data
	if err := r.Update(ctx, existing); err != nil {
		return fmt.Errorf("failed to update ConfigMap: %w", err)
	}

	return nil
}
