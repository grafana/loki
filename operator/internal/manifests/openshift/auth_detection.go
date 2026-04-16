package openshift

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"

	"github.com/go-logr/logr"
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

type ExternalOIDCConfig struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	CA           string
}

func DetectExternalOIDC(ctx context.Context, c k8s.Client, opts *lokiv1.LokiStackSpec, namespace string, log logr.Logger) (*ExternalOIDCConfig, error) {

	auth := &configv1.Authentication{}
	key := types.NamespacedName{Name: "cluster"}

	if err := c.Get(ctx, key, auth); err != nil {
		// TODO: handle error
		return nil, err
	}

	if auth.Spec.Type != configv1.AuthenticationTypeOIDC || len(auth.Spec.OIDCProviders) == 0 {
		// TODO: handle error
		return nil, nil
	}

	if opts.Tenants.Openshift == nil ||
		opts.Tenants.Openshift.OIDC == nil ||
		opts.Tenants.Openshift.OIDC.Secret == nil {
		return nil, kverrors.New("OpenShift OIDC secret name must be configured in the LokiStack CR")
	}

	oidcSpec := opts.Tenants.Openshift.OIDC

	oidcSecret := &corev1.Secret{}
	key = types.NamespacedName{
		Name:      oidcSpec.Secret.Name,
		Namespace: namespace,
	}

	if err := c.Get(ctx, key, oidcSecret); err != nil {
		// TODO: handle errorr
		return nil, err
	}

	clientID := string(oidcSecret.Data["clientID"])
	clientSecret := string(oidcSecret.Data["clientSecret"])

	if clientID == "" || clientSecret == "" {
		// TODO: this should return an error
		return nil, nil
	}

	provider := auth.Spec.OIDCProviders[0]

	issuerURL := oidcSpec.IssuerURL
	if issuerURL == "" {
		issuerURL = provider.Issuer.URL
	}

	config := &ExternalOIDCConfig{
		IssuerURL:    issuerURL,
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}

	var caConfigMapName, caNamespace string

	// Handle CA certificate if it's provided in the LokiStack CR
	if oidcSpec.IssuerCA != nil {
		caConfigMapName = oidcSpec.IssuerCA.CA
		caNamespace = namespace
	} else if provider.Issuer.CertificateAuthority.Name != "" {
		// Use CA certificate from the Authentication CR
		caConfigMapName = provider.Issuer.CertificateAuthority.Name
		caNamespace = "openshift-config"
	}

	if caConfigMapName != "" {
		caConfigMap := &corev1.ConfigMap{}
		key = types.NamespacedName{
			Name:      caConfigMapName,
			Namespace: caNamespace,
		}
		if err := c.Get(ctx, key, caConfigMap); err != nil {
			if apierrors.IsNotFound(err) {
				if oidcSpec.IssuerCA != nil {
					return nil, kverrors.Wrap(err, "CA ConfigMap %s/%s not found", caNamespace, caConfigMapName)
				}
				log.Info("Cluster OIDC ConfigMap not found, OIDC provider must provide publicly-trusted certificate",
					"configmap", caConfigMapName,
					"namespace", caNamespace,
				)
				return config, nil
			}
			return nil, kverrors.Wrap(err, "Failed to read CA ConfigMap %s/%s", caNamespace, caConfigMapName)
		}
		if caCert, ok := caConfigMap.Data["ca-bundle.crt"]; ok {
			config.CA = caCert
		} else {
			return nil, kverrors.New("CA ConfigMap exists but missing CA bundle key",
				"configMap", caConfigMapName,
				"namespace", caNamespace)
		}
	}

	return config, nil
}
