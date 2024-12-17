package gateway

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests"
)

const (
	// LokiGatewayTenantFileName is the name of the tenant config file in the configmap
	LokiGatewayTenantFileName = "tenants.yaml"
)

type tenantsConfigJSON struct {
	Tenants []tenantsSpec `json:"tenants,omitempty"`
}

type tenantsSpec struct {
	Name      string         `json:"name"`
	ID        string         `json:"id"`
	OpenShift *openShiftSpec `json:"openshift"`
}

type openShiftSpec struct {
	ServiceAccount string `json:"serviceAccount"`
	RedirectURL    string `json:"redirectURL"`
	CookieSecret   string `json:"cookieSecret"`
}

// getTenantConfigFromSecret returns the tenantName, tenantId, cookieSecret
// clusters to auto-create redirect URLs for OpenShift Auth or an error.
func getTenantConfigFromSecret(ctx context.Context, k k8s.Client, stack *lokiv1.LokiStack) (map[string]manifests.TenantConfig, error) {
	var tenantSecret corev1.Secret
	key := client.ObjectKey{Name: manifests.GatewayName(stack.Name), Namespace: stack.Namespace}
	if err := k.Get(ctx, key, &tenantSecret); err != nil {
		return nil, kverrors.Wrap(err, "couldn't find tenant secret.")
	}

	ts, err := extractTenantConfigMap(&tenantSecret)
	if err != nil {
		return nil, kverrors.Wrap(err, "error occurred in extracting tenants.yaml secret.")
	}

	tsMap := make(map[string]manifests.TenantConfig)
	for _, tenant := range ts.Tenants {
		tc := manifests.TenantConfig{}
		if tenant.OpenShift != nil {
			tc.OpenShift = &manifests.TenantOpenShiftSpec{
				CookieSecret: tenant.OpenShift.CookieSecret,
			}
		}

		tsMap[tenant.Name] = tc
	}

	return tsMap, nil
}

// extractTenantConfigMap extracts tenants.yaml data if valid.
// This is to be used to configure tenant's authentication spec when exists.
func extractTenantConfigMap(s *corev1.Secret) (*tenantsConfigJSON, error) {
	// Extract required fields from tenants.yaml
	tenantConfigYAML, ok := s.Data[LokiGatewayTenantFileName]
	if !ok {
		return nil, kverrors.New("missing tenants.yaml file in configMap.")
	}

	tenantConfigJSON, err := yaml.YAMLToJSON(tenantConfigYAML)
	if err != nil {
		return nil, kverrors.New("error in converting tenant config yaml to json.")
	}

	var tenantConfig tenantsConfigJSON
	err = json.Unmarshal(tenantConfigJSON, &tenantConfig)
	if err != nil {
		return nil, kverrors.New("error in unmarshalling tenant config to struct.")
	}

	return &tenantConfig, nil
}
