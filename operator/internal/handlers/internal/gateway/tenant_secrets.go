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
		caConfigMap   corev1.ConfigMap
	)

	for _, tenant := range stack.Spec.Tenants.Authentication {
		switch {
		case tenant.OIDC != nil:
			key := client.ObjectKey{Name: tenant.OIDC.Secret.Name, Namespace: req.Namespace}
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
			tenantSecrets = append(tenantSecrets, &manifests.TenantSecrets{
				TenantName: tenant.TenantName,
				OIDCSecret: oidcSecret,
			})
		case tenant.MTLS != nil:
			key := client.ObjectKey{Name: tenant.MTLS.CA.CA, Namespace: req.Namespace}
			if err := k.Get(ctx, key, &caConfigMap); err != nil {
				if apierrors.IsNotFound(err) {
					return nil, &status.DegradedError{
						Message: fmt.Sprintf("Missing configmap for tenant %s", tenant.TenantName),
						Reason:  lokiv1.ReasonMissingGatewayTenantConfigMap,
						Requeue: true,
					}
				}
				return nil, kverrors.Wrap(err, "failed to lookup lokistack gateway tenant configMap",
					"name", key)
			}
			// Default key if the user doesn't specify it
			cmKey := "service-ca.crt"
			if tenant.MTLS.CA.CAKey != "" {
				cmKey = tenant.MTLS.CA.CAKey
			}
			err := checkKeyIsPresent(&caConfigMap, cmKey)
			if err != nil {
				return nil, &status.DegradedError{
					Message: "Invalid gateway tenant configmap contents",
					Reason:  lokiv1.ReasonInvalidGatewayTenantConfigMap,
					Requeue: true,
				}
			}
			tenantSecrets = append(tenantSecrets, &manifests.TenantSecrets{
				TenantName: tenant.TenantName,
				MTLSSecret: &manifests.MTLSSecret{
					CAPath: manifests.TenantMTLSCAPath(tenant.TenantName, cmKey),
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
	issuerCAPath := s.Data["issuerCAPath"]

	return &manifests.OIDCSecret{
		ClientID:     string(clientID),
		ClientSecret: string(clientSecret),
		IssuerCAPath: string(issuerCAPath),
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
