package controllers

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/controllers/loki/internal/lokistack"
	"github.com/grafana/loki/operator/controllers/loki/internal/management/state"
	"github.com/grafana/loki/operator/internal/handlers"
)

type CredentialsRequestsReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

func (r *CredentialsRequestsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	managed, err := state.IsManaged(ctx, req, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !managed {
		r.Log.Info("Skipping reconciliation for unmanaged LokiStack resource", "name", req.String())
		// Stop requeueing for unmanaged LokiStack custom resources
		return ctrl.Result{}, nil
	}

	var stack *lokiv1.LokiStack
	if err = r.Client.Get(ctx, req.NamespacedName, stack); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, handlers.DeleteCredentialsRequest(ctx, r.Client, req.NamespacedName)
		}
		return ctrl.Result{}, err
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

// SetupWithManager sets up the controller with the Manager to only call this controller on create/delete/generic events.
func (r *CredentialsRequestsReconciler) SetupWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&lokiv1.LokiStack{}, createOrDeletesPred).
		Complete(r)
}
