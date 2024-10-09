package controllers

import (
	"context"
	"errors"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/controllers/loki/internal/lokistack"
	"github.com/grafana/loki/operator/controllers/loki/internal/management/state"
	"github.com/grafana/loki/operator/internal/certrotation"
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
	ll := logWithLokiStackRef(r.Log, req, CertRotationCtrlName)

	managed, err := state.IsManaged(ctx, req, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !managed {
		ll.Info("skipping reconciliation for unmanaged LokiStack resource")
		// Stop requeueing for unmanaged LokiStack custom resources
		return ctrl.Result{}, nil
	}

	rt, err := certrotation.ParseRotation(r.FeatureGates.BuiltInCertManagement)
	if err != nil {
		return ctrl.Result{}, err
	}

	checkExpiryAfter := expiryRetryAfter(rt.TargetCertRefresh)
	ll.Info("checking if certificates expired", "interval", checkExpiryAfter.String())

	var expired *certrotation.CertExpiredError

	err = handlers.CheckCertExpiry(ctx, ll, req, r.Client, r.FeatureGates)
	switch {
	case errors.As(err, &expired):
		ll.Info("certificate expired", "msg", expired.Error())
	case err != nil:
		return ctrl.Result{}, err
	default:
		ll.Info("skipping rotation, all certificates still valid")
		return ctrl.Result{
			RequeueAfter: checkExpiryAfter,
		}, nil
	}

	ll.Error(err, "certificates expired")
	err = lokistack.AnnotateForRequiredCertRotation(ctx, r.Client, req.Name, req.Namespace)
	if err != nil {
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
		WithLogConstructor(lokiStackLogConstructor(r.Log, CertRotationCtrlName)).
		Complete(r)
}

func expiryRetryAfter(certRefresh time.Duration) time.Duration {
	day := 24 * time.Hour
	if certRefresh > day {
		return 12 * time.Hour
	}

	return certRefresh / 4
}
