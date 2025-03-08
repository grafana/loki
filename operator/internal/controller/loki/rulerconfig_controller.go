package loki

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/controller/loki/internal/lokistack"
)

const ControllerNameRulerConfig = "rulerconfig"

// RulerConfigReconciler reconciles a RulerConfig object
type RulerConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=loki.grafana.com,resources=rulerconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=loki.grafana.com,resources=rulerconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=loki.grafana.com,resources=rulerconfigs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *RulerConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var rc lokiv1.RulerConfig
	key := client.ObjectKey{Name: req.Name, Namespace: req.Namespace}
	if err := r.Get(ctx, key, &rc); err != nil {
		if errors.IsNotFound(err) {
			// RulerConfig not found, remove annotation from LokiStack.
			err = lokistack.RemoveRulerConfigAnnotation(ctx, r.Client, req.Name, req.Namespace)
			if err != nil {
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	err := lokistack.AnnotateForRulerConfig(ctx, r.Client, req.Name, req.Namespace)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RulerConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&lokiv1.RulerConfig{}).
		Named(ControllerNameRulerConfig).
		Complete(r)
}
