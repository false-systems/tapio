package operator

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	tapiov1alpha1 "github.com/yairfalse/tapio/api/v1alpha1"
)

const finalizerName = "tapio.io/finalizer"

func (r *TapioObserverReconciler) handleFinalizer(ctx context.Context, observer *tapiov1alpha1.TapioObserver) (bool, error) {
	if observer.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(observer, finalizerName) {
			controllerutil.AddFinalizer(observer, finalizerName)
			if err := r.Update(ctx, observer); err != nil {
				return false, fmt.Errorf("failed to add finalizer: %w", err)
			}
		}
		return false, nil
	}

	if controllerutil.ContainsFinalizer(observer, finalizerName) {
		if err := r.cleanupClusterResources(ctx, observer); err != nil {
			return false, fmt.Errorf("failed to cleanup cluster resources: %w", err)
		}

		controllerutil.RemoveFinalizer(observer, finalizerName)
		if err := r.Update(ctx, observer); err != nil {
			return false, fmt.Errorf("failed to remove finalizer: %w", err)
		}
	}

	return true, nil
}

func (r *TapioObserverReconciler) cleanupClusterResources(ctx context.Context, observer *tapiov1alpha1.TapioObserver) error {
	crName := fmt.Sprintf("tapio-observer-%s-%s", observer.Namespace, observer.Name)

	cr := &rbacv1.ClusterRole{}
	err := r.Get(ctx, types.NamespacedName{Name: crName}, cr)
	if err == nil {
		if err := r.Delete(ctx, cr); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete ClusterRole: %w", err)
		}
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get ClusterRole: %w", err)
	}

	crb := &rbacv1.ClusterRoleBinding{}
	err = r.Get(ctx, types.NamespacedName{Name: crName}, crb)
	if err == nil {
		if err := r.Delete(ctx, crb); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete ClusterRoleBinding: %w", err)
		}
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get ClusterRoleBinding: %w", err)
	}

	return nil
}
