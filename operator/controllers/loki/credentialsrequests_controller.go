package controllers

import (
	"context"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/controllers/loki/internal/lokistack"
	"github.com/grafana/loki/operator/controllers/loki/internal/management/state"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/handlers"
)

// CredentialsRequestsReconciler reconciles a single CredentialsRequest resource for each LokiStack request.
type CredentialsRequestsReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// Reconcile creates a single CredentialsRequest per LokiStack for the OpenShift cloud-credentials-operator (CCO) to
// provide a managed cloud credentials Secret. On successful creation, the LokiStack resource is annotated
// with `loki.grafana.com/credentials-request-secret-ref` that refers to the secret provided by CCO. If the LokiStack
// resource is not found its accompanying CredentialsRequest resource is deleted.
func (r *CredentialsRequestsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var stack lokiv1.LokiStack
	if err := r.Client.Get(ctx, req.NamespacedName, &stack); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, handlers.DeleteCredentialsRequest(ctx, r.Client, req.NamespacedName)
		}
		return ctrl.Result{}, err
	}

	managed, err := state.IsManaged(ctx, req, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !managed {
		r.Log.Info("Skipping reconciliation for unmanaged LokiStack resource", "name", req.String())
		// Stop requeueing for unmanaged LokiStack custom resources
		return ctrl.Result{}, nil
	}

	secretRef, err := handlers.CreateCredentialsRequest(ctx, r.Client, req.NamespacedName)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := lokistack.AnnotateForCredentialsRequest(ctx, r.Client, req.NamespacedName, secretRef); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CredentialsRequestsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr)
	return r.buildController(k8s.NewCtrlBuilder(b))
}

func (r *CredentialsRequestsReconciler) buildController(bld k8s.Builder) error {
	return bld.
		For(&lokiv1.LokiStack{}).
		Complete(r)
}
