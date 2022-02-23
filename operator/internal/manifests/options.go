package manifests

import (
	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests/internal"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

// Options is a set of configuration values to use when building manifests such as resource sizes, etc.
// Most of this should be provided - either directly or indirectly - by the user.
type Options struct {
	Name              string
	Namespace         string
	Image             string
	GatewayImage      string
	GatewayBaseDomain string
	ConfigSHA1        string

	Flags FeatureFlags

	Stack                lokiv1beta1.LokiStackSpec
	ResourceRequirements internal.ComponentResources

	ObjectStorage storage.Options

	OpenShiftOptions openshift.Options
	TenantSecrets    []*TenantSecrets
	TenantConfigMap  map[string]openshift.TenantData
}

// FeatureFlags contains flags that activate various features
type FeatureFlags struct {
	EnableCertificateSigningService bool
	EnableServiceMonitors           bool
	EnableTLSServiceMonitorConfig   bool
	EnableGateway                   bool
	EnableGatewayRoute              bool
}

// TenantSecrets for clientID, clientSecret and issuerCAPath for tenant's authentication.
type TenantSecrets struct {
	TenantName   string
	ClientID     string
	ClientSecret string
	IssuerCAPath string
}
