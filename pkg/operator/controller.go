package operator

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tapiov1alpha1 "github.com/yairfalse/tapio/api/v1alpha1"
)

// TapioObserverReconciler reconciles a TapioObserver object
type TapioObserverReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile implements the reconciliation loop for TapioObserver
func (r *TapioObserverReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var observer tapiov1alpha1.TapioObserver
	if err := r.Get(ctx, req.NamespacedName, &observer); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	shouldDelete, err := r.handleFinalizer(ctx, &observer)
	if err != nil {
		return ctrl.Result{}, err
	}
	if shouldDelete {
		return ctrl.Result{}, nil
	}

	if err := r.reconcileServiceAccount(ctx, &observer); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileClusterRole(ctx, &observer); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileClusterRoleBinding(ctx, &observer); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileConfigMap(ctx, &observer); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileDaemonSet(ctx, &observer); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *TapioObserverReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tapiov1alpha1.TapioObserver{}).
		Complete(r)
}
