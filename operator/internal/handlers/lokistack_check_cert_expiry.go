package handlers

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/certrotation"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/handlers/internal/certificates"
)

// CheckCertExpiry handles the case if the LokiStack managed signing CA, client and/or serving
// certificates expired. Returns true if any of those expired and an error representing the reason
// of expiry.
func CheckCertExpiry(ctx context.Context, log logr.Logger, req ctrl.Request, k k8s.Client, fg configv1.FeatureGates) error {
	ll := log.WithValues("lokistack", req.String(), "event", "checkCertExpiry")

	var stack lokiv1.LokiStack
	if err := k.Get(ctx, req.NamespacedName, &stack); err != nil {
		if apierrors.IsNotFound(err) {
			// maybe the user deleted it before we could react? Either way this isn't an issue
			ll.Error(err, "could not find the requested loki stack", "name", req.String())
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup lokistack", "name", req.String())
	}

	var mode lokiv1.ModeType
	if stack.Spec.Tenants != nil {
		mode = stack.Spec.Tenants.Mode
	}

	opts, err := certificates.GetOptions(ctx, k, req, mode)
	if err != nil {
		return kverrors.Wrap(err, "failed to lookup certificates secrets", "name", req.String())
	}

	if optErr := certrotation.ApplyDefaultSettings(&opts, fg.BuiltInCertManagement); optErr != nil {
		ll.Error(optErr, "failed to conform options to build settings")
		return optErr
	}

	if err := certrotation.SigningCAExpired(opts); err != nil {
		return err
	}

	if err := certrotation.CertificatesExpired(opts); err != nil {
		return err
	}

	return nil
}
