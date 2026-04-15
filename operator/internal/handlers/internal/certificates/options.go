package certificates

import (
	"context"
	"regexp"

	"github.com/ViaQ/logerr/v2/kverrors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/certrotation"
	"github.com/grafana/loki/operator/internal/external/k8s"
)

var serviceCAnnotationsRe = regexp.MustCompile(`^service.(?:alpha|beta)\.openshift\.io\/.+`)

// GetOptions return a certrotation options struct filled with all found client and serving certificate secrets if any found.
// Return an error only if either the k8s client returns any other error except IsNotFound or if merging options fails.
func GetOptions(ctx context.Context, k k8s.Client, req ctrl.Request, mode lokiv1.ModeType) (certrotation.Options, error) {
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
	configureCABundleForTenantMode(bundle, mode)

	certs, err := getCertificateOptions(ctx, k, req)
	if err != nil {
		return certrotation.Options{}, err
	}
	configureCertificatesForTenantMode(certs, mode)

	return certrotation.Options{
		StackName:      req.Name,
		StackNamespace: req.Namespace,
		Signer: certrotation.SigningCA{
			Secret: ca,
		},
		CABundle:     bundle,
		Certificates: certs,
	}, nil
}

func getCertificateOptions(ctx context.Context, k k8s.Client, req ctrl.Request) (certrotation.ComponentCertificates, error) {
	cs := certrotation.ComponentCertSecretNames(req.Name)
	certs := make(certrotation.ComponentCertificates, len(cs))

	for _, name := range cs {
		s, err := getSecret(ctx, k, name, req.Namespace)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return nil, kverrors.Wrap(err, "failed to get secret", "name", name)
			}
			continue
		}

		certs[name] = certrotation.SelfSignedCertKey{Secret: s}
	}

	return certs, nil
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

func configureCertificatesForTenantMode(certs certrotation.ComponentCertificates, mode lokiv1.ModeType) {
	switch mode {
	case "", lokiv1.Dynamic, lokiv1.Static:
		return
	case lokiv1.OpenshiftLogging, lokiv1.OpenshiftNetwork:
		// Remove serviceCA annotations for existing secrets to
		// enable upgrading secrets to built-in cert management
		for name := range certs {
			for key := range certs[name].Secret.Annotations {
				if serviceCAnnotationsRe.MatchString(key) {
					delete(certs[name].Secret.Annotations, key)
				}
			}
		}
	}
}

func configureCABundleForTenantMode(cm *corev1.ConfigMap, mode lokiv1.ModeType) {
	if cm == nil {
		return
	}

	switch mode {
	case "", lokiv1.Dynamic, lokiv1.Static:
		return
	case lokiv1.OpenshiftLogging, lokiv1.OpenshiftNetwork:
		// Remove serviceCA annotations for existing ConfigMap to
		// enable upgrading CABundle from built-in cert management
		for key := range cm.Annotations {
			if serviceCAnnotationsRe.MatchString(key) {
				delete(cm.Annotations, key)
			}
		}
	}
}
