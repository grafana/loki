package gateway

import (
	"context"
	"fmt"

	"github.com/ViaQ/logerr/v2/kverrors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/status"
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

		var ts *manifests.TenantSecrets
		ts, err := extractSecret(&gatewaySecret, tenant.TenantName)
		if err != nil {
			return nil, &status.DegradedError{
				Message: "Invalid gateway tenant secret contents",
				Reason:  lokiv1.ReasonInvalidGatewayTenantSecret,
				Requeue: true,
			}
		}
		tenantSecrets = append(tenantSecrets, ts)
	}

	return tenantSecrets, nil
}

// extractSecret reads a k8s secret into a manifest tenant secret struct if valid.
func extractSecret(s *corev1.Secret, tenantName string) (*manifests.TenantSecrets, error) {
	// Extract and validate mandatory fields
	clientID := s.Data["clientID"]
	if len(clientID) == 0 {
		return nil, kverrors.New("missing clientID field", "field", "clientID")
	}
	clientSecret := s.Data["clientSecret"]
	issuerCAPath := s.Data["issuerCAPath"]

	return &manifests.TenantSecrets{
		TenantName:   tenantName,
		ClientID:     string(clientID),
		ClientSecret: string(clientSecret),
		IssuerCAPath: string(issuerCAPath),
	}, nil
}
