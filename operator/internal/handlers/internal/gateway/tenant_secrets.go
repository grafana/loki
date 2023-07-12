package gateway

import (
	"context"
	"fmt"

	"github.com/ViaQ/logerr/v2/kverrors"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/status"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetTenantSecrets returns the list to gateway tenant secrets for a tenant mode.
// For modes static and dynamic the secrets are fetched from external provided
// secrets. For modes openshift-logging and openshift-network a secret per default tenants are created.
// All secrets live in the same namespace as the lokistack request.
func GetTenantSecrets(
	ctx context.Context,
	k k8s.Client,
	req ctrl.Request,
	stack *lokiv1.LokiStack,
) ([]*manifests.TenantSecrets, error) {
	var (
		tenantSecrets []*manifests.TenantSecrets
		gatewaySecret corev1.Secret
	)

	for _, tenant := range stack.Spec.Tenants.Authentication {
		switch {
		case tenant.OIDC != nil:
			key := client.ObjectKey{Name: tenant.OIDC.Secret.Name, Namespace: req.Namespace}
			if err := getSecret(ctx, k, tenant.TenantName, key, &gatewaySecret); err != nil {
				return nil, err
			}
			oidcSecret, err := extractOIDCSecret(&gatewaySecret)
			if err != nil {
				return nil, &status.DegradedError{
					Message: "Invalid gateway tenant secret contents",
					Reason:  lokiv1.ReasonInvalidGatewayTenantSecret,
					Requeue: true,
				}
			}
			tenantSecrets = append(tenantSecrets, &manifests.TenantSecrets{
				OIDCSecret: oidcSecret,
			})
		case tenant.MTLS != nil:
			key := client.ObjectKey{Name: tenant.MTLS.CertSecret.Name, Namespace: req.Namespace}
			if err := getSecret(ctx, k, tenant.TenantName, key, &gatewaySecret); err != nil {
				return nil, err
			}
			cert, err := extractCert(&gatewaySecret)
			if err != nil {
				return nil, &status.DegradedError{
					Message: "Invalid gateway tenant secret contents",
					Reason:  lokiv1.ReasonInvalidGatewayTenantSecret,
					Requeue: true,
				}
			}

			var configMap corev1.ConfigMap
			key = client.ObjectKey{Name: tenant.MTLS.CASpec.CA, Namespace: req.Namespace}
			if err = getConfigMap(ctx, k, tenant.TenantName, key, &configMap); err != nil {
				return nil, err
			}
			ca, err := extractCA(&configMap, tenant.MTLS.CASpec.CAKey)
			if err != nil {
				return nil, &status.DegradedError{
					Message: "Invalid gateway tenant secret contents",
					Reason:  lokiv1.ReasonInvalidGatewayTenantSecret,
					Requeue: true,
				}
			}
			tenantSecrets = append(tenantSecrets, &manifests.TenantSecrets{
				TenantName: tenant.TenantName,
				MTLSSecret: &manifests.MTLSSecret{
					Cert: cert,
					CA:   ca,
				},
			})
		default:
			return nil, &status.DegradedError{
				Message: "No gateway tenant authentication method provided",
				Reason:  lokiv1.ReasonInvalidGatewayTenantSecret,
				Requeue: true,
			}
		}
	}

	return tenantSecrets, nil
}

func getSecret(ctx context.Context, k k8s.Client, tenantName string, key client.ObjectKey, s *corev1.Secret) error {
	if err := k.Get(ctx, key, s); err != nil {
		if apierrors.IsNotFound(err) {
			return &status.DegradedError{
				Message: fmt.Sprintf("Missing secrets for tenant %s", tenantName),
				Reason:  lokiv1.ReasonMissingGatewayTenantSecret,
				Requeue: true,
			}
		}
		return kverrors.Wrap(err, "failed to lookup lokistack gateway tenant secret",
			"name", key)
	}
	return nil
}

func getConfigMap(ctx context.Context, k k8s.Client, tenantName string, key client.ObjectKey, cm *corev1.ConfigMap) error {
	if err := k.Get(ctx, key, cm); err != nil {
		if apierrors.IsNotFound(err) {
			return &status.DegradedError{
				Message: fmt.Sprintf("Missing configmap for tenant %s", tenantName),
				Reason:  lokiv1.ReasonMissingGatewayTenantConfigMap,
				Requeue: true,
			}
		}
		return kverrors.Wrap(err, "failed to lookup lokistack gateway tenant configMap",
			"name", key)
	}
	return nil
}

// extractOIDCSecret reads a k8s secret into a manifest tenant secret struct if valid.
func extractOIDCSecret(s *corev1.Secret) (*manifests.OIDCSecret, error) {
	// Extract and validate mandatory fields
	clientID := s.Data["clientID"]
	if len(clientID) == 0 {
		return nil, kverrors.New("missing clientID field", "field", "clientID")
	}
	clientSecret := s.Data["clientSecret"]
	issuerCAPath := s.Data["issuerCAPath"]

	return &manifests.OIDCSecret{
		ClientID:     string(clientID),
		ClientSecret: string(clientSecret),
		IssuerCAPath: string(issuerCAPath),
	}, nil
}

// extractCert reads the TLSCertKey from a k8s secret of type SecretTypeTLS
func extractCert(s *corev1.Secret) (string, error) {
	switch s.Type {
	case corev1.SecretTypeTLS:
		return string(s.Data[corev1.TLSCertKey]), nil
	default:
		return "", kverrors.New("missing fields, has to contain fields \"tls.key\" and \"tls.crt\"")
	}
}

// extractCA reads the CA from a k8s configmap
func extractCA(cm *corev1.ConfigMap, key string) (string, error) {
	// Extract and validate mandatory fields
	ca := cm.Data[key]
	if len(ca) == 0 {
		return "", kverrors.New(fmt.Sprintf("missing %s field", key), "field", key)
	}
	return string(ca), nil
}
