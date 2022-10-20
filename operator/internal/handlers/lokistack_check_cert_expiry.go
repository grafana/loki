package handlers

import (
	"context"
	"errors"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/certrotation"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/handlers/internal/certificates"
	"github.com/openshift/library-go/pkg/crypto"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
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

	opts, err := certificates.GetOptions(ctx, k, req)
	if err != nil {
		return kverrors.Wrap(err, "failed to lookup certificates secrets", "name", req.String())
	}

	ll.Info("begin building certificate manifests")

	if optErr := certrotation.ApplyDefaultSettings(&opts, fg.BuiltInCertManagement); optErr != nil {
		ll.Error(optErr, "failed to conform options to build settings")
		return optErr
	}

	// Skip checks if we haven't reconciled a PKI once.
	if opts.Signer.Secret == nil || opts.CABundle == nil {
		return nil
	}

	var incomplete bool
	for _, cert := range opts.Certificates {
		if cert.Secret == nil {
			incomplete = true
			break
		}
	}

	// Skip checks if we haven't completed PKI reconciliation yet.
	if incomplete {
		return nil
	}

	var expired *certrotation.CertExpiredError
	err = certrotation.SigningCAExpired(opts)
	if errors.As(err, &expired) {
		return err
	}

	rawCA, err := crypto.GetCAFromBytes(opts.Signer.Secret.Data[corev1.TLSCertKey], opts.Signer.Secret.Data[corev1.TLSPrivateKeyKey])
	if err != nil {
		return kverrors.Wrap(err, "failed to get signing CA from secret", "name", req.String())
	}
	opts.Signer.RawCA = rawCA

	caBundle := opts.CABundle.Data[certrotation.CAFile]
	caCerts, err := crypto.CertsFromPEM([]byte(caBundle))
	if err != nil {
		return kverrors.Wrap(err, "failed to get ca bundle certificates from configmap", "name", req.String())
	}
	opts.RawCACerts = caCerts

	err = certrotation.CertificatesExpired(opts)
	if errors.As(err, &expired) {
		return err
	}

	return nil
}
