package controllers

import (
	"context"
	"errors"
	"time"

	"github.com/go-logr/logr"
	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/controllers/loki/internal/lokistack"
	"github.com/grafana/loki/operator/controllers/loki/internal/management/state"
	"github.com/grafana/loki/operator/internal/certrotation"
	"github.com/grafana/loki/operator/internal/handlers"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	checkCertExpiryAfter = 12 * time.Hour
)

// CertRotationReconciler reconciles the `loki.grafana.com/certRotationRequiredAt` annotation on
// any LokiStack object associated with any of the owned signer/client/serving certificates secrets
// and CA bundle configmap.
type CertRotationReconciler struct {
	client.Client
	Log          logr.Logger
	Scheme       *runtime.Scheme
	FeatureGates configv1.FeatureGates
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// Compare the state specified by the LokiStack object against the actual cluster state,
// and then perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *CertRotationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	managed, err := state.IsManaged(ctx, req, r.Client)
	if err != nil {
		return ctrl.Result{
			Requeue: true,
		}, err
	}
	if !managed {
		r.Log.Info("Skipping reconciliation for unmanaged LokiStack resource", "name", req.String())
		// Stop requeueing for unmanaged LokiStack custom resources
		return ctrl.Result{}, nil
	}

	r.Log.Info("Checking if LokiStack certificates expired", "name", req.String())

	var expired *certrotation.CertExpiredError
	err = handlers.CheckCertExpiry(ctx, r.Log, req, r.Client, r.FeatureGates)
	if !errors.As(err, &expired) {
		r.Log.Info("Skipping cert rotation, all LokiStack certificates still valid", "name", req.String())
		return ctrl.Result{
			RequeueAfter: checkCertExpiryAfter,
		}, nil
	}

	r.Log.Error(err, "LokiStack certificates expired", "name", req.String())
	err = lokistack.AnnotateForRequiredCertRotation(ctx, r.Client, req.Name, req.Namespace)
	if err != nil {
		r.Log.Error(err, "failed to annotate required cert rotation", "name", req.String())
		return ctrl.Result{
			Requeue: true,
		}, err
	}

	return ctrl.Result{
		RequeueAfter: checkCertExpiryAfter,
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CertRotationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&lokiv1.LokiStack{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}
