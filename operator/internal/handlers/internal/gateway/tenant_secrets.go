package gateway

import (
	"context"
	"fmt"

	"github.com/ViaQ/logerr/v2/kverrors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/status"
)

// getTenantSecrets returns the list to gateway tenant secrets for a tenant mode.
// For modes static and dynamic the secrets are fetched from external provided
// secrets. For modes openshift-logging and openshift-network a secret per default tenants are created.
// All secrets live in the same namespace as the lokistack request.
func getTenantSecrets(
	ctx context.Context,
	k k8s.Client,
	stack *lokiv1.LokiStack,
) ([]*manifests.TenantSecrets, error) {
	var (
		tenantSecrets []*manifests.TenantSecrets
		gatewaySecret corev1.Secret
	)

	for _, tenant := range stack.Spec.Tenants.Authentication {
		switch {
		case tenant.OIDC != nil:
			key := client.ObjectKey{Name: tenant.OIDC.Secret.Name, Namespace: stack.Namespace}
			if err := k.Get(ctx, key, &gatewaySecret); err != nil {
				if apierrors.IsNotFound(err) {
					return nil, &status.DegradedError{
						Message: fmt.Sprintf("Missing secrets for tenant %s", tenant.TenantName),
						Reason:  lokiv1.ReasonMissingGatewayTenantSecret,
						Requeue: true,
					}
				}
				return nil, kverrors.Wrap(err, "failed to lookup lokistack gateway tenant secret",
					"name", key)
			}

			oidcSecret, err := extractOIDCSecret(&gatewaySecret)
			if err != nil {
				return nil, &status.DegradedError{
					Message: "Invalid gateway tenant secret contents",
					Reason:  lokiv1.ReasonInvalidGatewayTenantSecret,
					Requeue: true,
				}
			}
			tennantSecret := &manifests.TenantSecrets{
				TenantName: tenant.TenantName,
				OIDCSecret: oidcSecret,
			}
			if tenant.OIDC.IssuerCA != nil {
				caPath, err := extractCAPath(ctx, k, stack.Namespace, tenant.TenantName, tenant.OIDC.IssuerCA)
				if err != nil {
					return nil, err
				}
				tennantSecret.OIDCSecret.IssuerCAPath = caPath
			}
			tenantSecrets = append(tenantSecrets, tennantSecret)
		case tenant.MTLS != nil:
			caPath, err := extractCAPath(ctx, k, stack.Namespace, tenant.TenantName, tenant.MTLS.CA)
			if err != nil {
				return nil, err
			}
			tenantSecrets = append(tenantSecrets, &manifests.TenantSecrets{
				TenantName: tenant.TenantName,
				MTLSSecret: &manifests.MTLSSecret{
					CAPath: caPath,
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

// extractOIDCSecret reads a k8s secret into a manifest tenant secret struct if valid.
func extractOIDCSecret(s *corev1.Secret) (*manifests.OIDCSecret, error) {
	// Extract and validate mandatory fields
	clientID := s.Data["clientID"]
	if len(clientID) == 0 {
		return nil, kverrors.New("missing clientID field", "field", "clientID")
	}
	clientSecret := s.Data["clientSecret"]

	return &manifests.OIDCSecret{
		ClientID:     string(clientID),
		ClientSecret: string(clientSecret),
	}, nil
}

// checkKeyIsPresent checks if key is present in the configmap
func checkKeyIsPresent(cm *corev1.ConfigMap, key string) error {
	ca := cm.Data[key]
	if len(ca) == 0 {
		return kverrors.New(fmt.Sprintf("missing %s field", key), "field", key)
	}
	return nil
}

func extractCAPath(ctx context.Context, k k8s.Client, namespace string, tennantName string, caSpec *lokiv1.CASpec) (string, error) {
	var caConfigMap corev1.ConfigMap
	key := client.ObjectKey{Name: caSpec.CA, Namespace: namespace}
	if err := k.Get(ctx, key, &caConfigMap); err != nil {
		if apierrors.IsNotFound(err) {
			return "", &status.DegradedError{
				Message: fmt.Sprintf("Missing configmap for tenant %s", tennantName),
				Reason:  lokiv1.ReasonMissingGatewayTenantConfigMap,
				Requeue: true,
			}
		}
		return "", kverrors.Wrap(err, "failed to lookup lokistack gateway tenant configMap",
			"name", key)
	}
	// Default key if the user doesn't specify it
	cmKey := "service-ca.crt"
	if caSpec.CAKey != "" {
		cmKey = caSpec.CAKey
	}
	err := checkKeyIsPresent(&caConfigMap, cmKey)
	if err != nil {
		return "", &status.DegradedError{
			Message: "Invalid gateway tenant configmap contents",
			Reason:  lokiv1.ReasonInvalidGatewayTenantConfigMap,
			Requeue: true,
		}
	}
	return manifests.TenantCAPath(tennantName, cmKey), nil
}
