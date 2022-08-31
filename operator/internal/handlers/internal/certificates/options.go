package certificates

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/grafana/loki/operator/internal/certrotation"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/imdario/mergo"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetOptions return a certrotation options struct filled with all found client and serving certificate secrets if any found.
// Return an error only if either the k8s client returns any other error except IsNotFound or if merging options fails.
func GetOptions(ctx context.Context, k k8s.Client, req ctrl.Request) (certrotation.Options, error) {
	name := certrotation.SigningCASecretName(req.Name)
	ca, err := getSecret(ctx, k, name, req.Namespace)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return certrotation.Options{}, kverrors.Wrap(err, "failed to get signing ca secret", "name", name)
		}
	}

	name = certrotation.CABundleName(req.Name)
	bundle, err := getConfigMap(ctx, k, name, req.Namespace)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return certrotation.Options{}, kverrors.Wrap(err, "failed to get ca bundle secret", "name", name)
		}
	}

	opts := &certrotation.Options{
		StackName:      req.Name,
		StackNamespace: req.Namespace,
		Signer: certrotation.SigningCA{
			Secret: ca,
		},
		CABundle: bundle,
	}

	certOpts, err := getCertificateOptions(ctx, k, req)
	if err != nil {
		return certrotation.Options{}, err
	}

	if err := mergo.Merge(opts, certOpts); err != nil {
		return *opts, kverrors.Wrap(err, "failed to merge certificate options")
	}

	return *opts, nil
}

func getCertificateOptions(ctx context.Context, k k8s.Client, req ctrl.Request) (certrotation.Options, error) {
	name := certrotation.GatewayClientSecretName(req.Name)
	gws, err := getSecret(ctx, k, name, req.Namespace)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return certrotation.Options{}, kverrors.Wrap(err, "failed to get client secret", "name", name)
		}
	}

	certs := map[string]certrotation.SelfSignedCertKey{}
	cs := certrotation.ComponentCertSecretNames(req.Name)
	for _, name := range cs {
		s, err := getSecret(ctx, k, name, req.Namespace)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return certrotation.Options{}, kverrors.Wrap(err, "failed to get secret", "name", name)
			}
		}

		certs[name] = certrotation.SelfSignedCertKey{Secret: s}
	}

	return certrotation.Options{
		GatewayClientCertificate: certrotation.SelfSignedCertKey{Secret: gws},
		Certificates:             certs,
	}, nil
}

func getSecret(ctx context.Context, k k8s.Client, name, ns string) (*corev1.Secret, error) {
	key := client.ObjectKey{Name: name, Namespace: ns}
	s := &corev1.Secret{}
	err := k.Get(ctx, key, s)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func getConfigMap(ctx context.Context, k k8s.Client, name, ns string) (*corev1.ConfigMap, error) {
	key := client.ObjectKey{Name: name, Namespace: ns}
	s := &corev1.ConfigMap{}
	err := k.Get(ctx, key, s)
	if err != nil {
		return nil, err
	}

	return s, nil
}
