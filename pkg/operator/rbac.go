package operator

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	tapiov1alpha1 "github.com/yairfalse/tapio/api/v1alpha1"
)

func (r *TapioObserverReconciler) reconcileServiceAccount(ctx context.Context, observer *tapiov1alpha1.TapioObserver) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      observer.Name,
			Namespace: observer.Namespace,
		},
	}

	if err := ctrl.SetControllerReference(observer, sa, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	existing := &corev1.ServiceAccount{}
	err := r.Get(ctx, types.NamespacedName{Name: sa.Name, Namespace: sa.Namespace}, existing)
	if errors.IsNotFound(err) {
		if err := r.Create(ctx, sa); err != nil {
			return fmt.Errorf("failed to create ServiceAccount: %w", err)
		}
		return nil
	}

	return err
}

func (r *TapioObserverReconciler) reconcileClusterRole(ctx context.Context, observer *tapiov1alpha1.TapioObserver) error {
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("tapio-observer-%s", observer.Name),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"events"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	existing := &rbacv1.ClusterRole{}
	err := r.Get(ctx, types.NamespacedName{Name: cr.Name}, existing)
	if errors.IsNotFound(err) {
		if err := r.Create(ctx, cr); err != nil {
			return fmt.Errorf("failed to create ClusterRole: %w", err)
		}
		return nil
	}

	return err
}
