package gateway

import (
	"context"

	"github.com/ViaQ/loki-operator/internal/manifests/openshift"

	"github.com/ViaQ/logerr/log"

	"github.com/ViaQ/logerr/kverrors"
	"github.com/ViaQ/loki-operator/internal/manifests"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/yaml"

	"github.com/ViaQ/loki-operator/internal/external/k8s"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// GetTenantConfigMapData returns the tenantName, tenantId, cookieSecret
// clusters to auto-create redirect URLs for OpenShift Auth or an error.
func GetTenantConfigMapData(ctx context.Context, k k8s.Client, req ctrl.Request) map[string]openshift.TenantData {
	var tenantConfigMap corev1.ConfigMap
	key := client.ObjectKey{Name: manifests.LabelGatewayComponent, Namespace: req.Namespace}
	if err := k.Get(ctx, key, &tenantConfigMap); err != nil {
		log.Error(err, "couldn't find")
		return nil
	}

	tcm, err := extractTenantConfigMap(&tenantConfigMap)
	if err != nil {
		log.Error(err, "error occurred in extracting tenants.yaml configMap.")
		return nil
	}

	tcmMap := make(map[string]openshift.TenantData)
	for _, tenant := range tcm.Tenants {
		tcmMap[tenant.Name] = openshift.TenantData{
			TenantID:     tenant.ID,
			CookieSecret: tenant.OpenShift.CookieSecret,
		}
	}

	return tcmMap
}

// extractTenantConfigMap extracts tenants.yaml data if valid.
// This is to be used to configure tenant's authentication spec when exists.
func extractTenantConfigMap(cm *corev1.ConfigMap) (*tenantsConfigJSON, error) {
	// Extract required fields from tenants.yaml
	tenantConfigYAML, ok := cm.BinaryData[LokiGatewayTenantFileName]
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
