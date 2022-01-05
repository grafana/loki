package gateway

import (
	lokiv1beta1 "github.com/grafana/loki-operator/api/v1beta1"
	"github.com/grafana/loki-operator/internal/manifests/openshift"
)

// Options is used to render the rbac.yaml and tenants.yaml file template
type Options struct {
	Stack lokiv1beta1.LokiStackSpec

	Namespace        string
	Name             string
	StorageDirectory string

	OpenShiftOptions openshift.Options
	TenantSecrets    []*Secret
	TenantConfigMap  map[string]TenantData
}

// Secret for clientID, clientSecret and issuerCAPath for tenant's authentication.
type Secret struct {
	TenantName   string
	ClientID     string
	ClientSecret string
	IssuerCAPath string
}

// TenantData defines the existing tenantID and cookieSecret for lokistack reconcile.
type TenantData struct {
	TenantID     string
	CookieSecret string
}
