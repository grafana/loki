package loki

import (
	"context"
	"errors"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/certrotation"
	"github.com/grafana/loki/operator/internal/controller/loki/internal/lokistack"
	"github.com/grafana/loki/operator/internal/controller/loki/internal/management/state"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/handlers"
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
		return ctrl.Result{}, err
	}
	if !managed {
		r.Log.Info("Skipping reconciliation for unmanaged LokiStack resource", "name", req.String())
		// Stop requeueing for unmanaged LokiStack custom resources
		return ctrl.Result{}, nil
	}

	rt, err := certrotation.ParseRotation(r.FeatureGates.BuiltInCertManagement)
	if err != nil {
		return ctrl.Result{}, err
	}

	checkExpiryAfter := expiryRetryAfter(rt.TargetCertRefresh)
	r.Log.Info("Checking if LokiStack certificates expired", "name", req.String(), "interval", checkExpiryAfter.String())

	var expired *certrotation.CertExpiredError

	err = handlers.CheckCertExpiry(ctx, r.Log, req, r.Client, r.FeatureGates)
	switch {
	case errors.As(err, &expired):
		r.Log.Info("Certificate expired", "msg", expired.Error())
	case err != nil:
		return ctrl.Result{}, err
	default:
		r.Log.Info("Skipping cert rotation, all LokiStack certificates still valid", "name", req.String())
		return ctrl.Result{
			RequeueAfter: checkExpiryAfter,
		}, nil
	}

	r.Log.Error(err, "LokiStack certificates expired", "name", req.String())
	err = lokistack.AnnotateForRequiredCertRotation(ctx, r.Client, req.Name, req.Namespace)
	if err != nil {
		r.Log.Error(err, "failed to annotate required cert rotation", "name", req.String())
		return ctrl.Result{}, err
	}

	return ctrl.Result{
		RequeueAfter: checkExpiryAfter,
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CertRotationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr)
	return r.buildController(k8s.NewCtrlBuilder(b))
}

func (r *CertRotationReconciler) buildController(bld k8s.Builder) error {
	return bld.
		For(&lokiv1.LokiStack{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}

func expiryRetryAfter(certRefresh time.Duration) time.Duration {
	day := 24 * time.Hour
	if certRefresh > day {
		return 12 * time.Hour
	}

	return certRefresh / 4
}
