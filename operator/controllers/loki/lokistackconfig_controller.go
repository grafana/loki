package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"
	lokiv1beta1 "github.com/grafana/loki/operator/apis/loki/v1beta1"
	"github.com/grafana/loki/operator/controllers/loki/internal/lokistack"
)

// LokiStackConfigReconciler reconciles a LokiStackConfig object
type LokiStackConfigReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=loki.grafana.com,resources=lokistackconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=loki.grafana.com,resources=lokistackconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=loki.grafana.com,resources=lokistackconfigs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the LokiStackConfig object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *LokiStackConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if err := lokistack.AnnotateForLokiStackConfig(ctx, r.Client); err != nil {
		r.Log.Error(err, "failed to annotate lokistackconfig change", "name", req.String())
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LokiStackConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&lokiv1beta1.LokiStackConfig{}).
		Complete(r)
}
