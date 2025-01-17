package gateway

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/handlers/internal/openshift"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/status"
)

// BuildOptions returns the options needed to generate Kubernetes resource
// manifests for the lokistack-gateway.
// The returned error can be a status.DegradedError in the following cases:
//   - The tenants spec is missing.
//   - The tenants spec is invalid.
func BuildOptions(ctx context.Context, log logr.Logger, k k8s.Client, stack *lokiv1.LokiStack, fg configv1.FeatureGates) (string, manifests.Tenants, error) {
	var (
		err        error
		baseDomain string
		secrets    []*manifests.TenantSecrets
		configs    map[string]manifests.TenantConfig
		tenants    manifests.Tenants
	)

	if !fg.LokiStackGateway {
		return "", tenants, nil
	}

	if stack.Spec.Tenants == nil {
		return "", tenants, &status.DegradedError{
			Message: "Invalid tenants configuration: TenantsSpec cannot be nil when gateway flag is enabled",
			Reason:  lokiv1.ReasonInvalidTenantsConfiguration,
			Requeue: false,
		}
	}

	if err = validateModes(stack); err != nil {
		return "", tenants, &status.DegradedError{
			Message: fmt.Sprintf("Invalid tenants configuration: %s", err),
			Reason:  lokiv1.ReasonInvalidTenantsConfiguration,
			Requeue: false,
		}
	}

	switch stack.Spec.Tenants.Mode {
	case lokiv1.OpenshiftLogging, lokiv1.OpenshiftNetwork:
		baseDomain, err = getOpenShiftBaseDomain(ctx, k)
		if err != nil {
			return "", tenants, err
		}

		if stack.Spec.Proxy == nil {
			// If the LokiStack has no proxy set but there is a cluster-wide proxy setting,
			// set the LokiStack proxy to that.
			ocpProxy, proxyErr := openshift.GetProxy(ctx, k)
			if proxyErr != nil {
				return "", tenants, proxyErr
			}

			stack.Spec.Proxy = ocpProxy
		}
	default:
		secrets, err = getTenantSecrets(ctx, k, stack)
		if err != nil {
			return "", tenants, err
		}
	}

	// extract the existing tenant's id, cookieSecret if exists, otherwise create new.
	configs, err = getTenantConfigFromSecret(ctx, k, stack)
	if err != nil {
		log.Error(err, "error in getting tenant secret data")
	}

	tenants = manifests.Tenants{
		Secrets: secrets,
		Configs: configs,
	}

	return baseDomain, tenants, nil
}
