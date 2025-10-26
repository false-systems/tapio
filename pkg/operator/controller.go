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

	return ctrl.Result{}, nil
}
