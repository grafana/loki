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
	case lokiv1.Passthrough:
		if degradedErr := validatePassthroughCA(fg.HTTPEncryption, stack); degradedErr != nil {
			return "", tenants, degradedErr
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

func validatePassthroughCA(httpEncryption bool, stack *lokiv1.LokiStack) *status.DegradedError {
	if !httpEncryption {
		// TODO(JoaoBraveCoding): Discuss with @xperimental if this makes sense or if we should always require
		// mTLS with the client
		return nil // If HTTP encryption is not enabled, we do not require clients to provide a certificate
	}

	if stack.Spec.Tenants.Passthrough == nil || stack.Spec.Tenants.Passthrough.CA == nil {
		return &status.DegradedError{
			Message: "Invalid passthrough configuration: missing CA configuration",
			Reason:  lokiv1.ReasonInvalidPassthroughConfiguration,
			Requeue: false,
		}
	}

	// TODO(JoaoBraveCoding): Once we merge https://github.com/grafana/loki/pull/20325 we should rebase
	// and add a call to validateConfigRef to validate that the CA actually exists in the cluster
	return nil // CEL rules on ValueReference handle the rest
}
