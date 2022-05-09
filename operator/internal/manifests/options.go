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

	AlertingRules  []lokiv1beta1.AlertingRule
	RecordingRules []lokiv1beta1.RecordingRule
	Ruler          Ruler

	ObjectStorage storage.Options

	OpenShiftOptions openshift.Options

	Tenants Tenants
}

// FeatureFlags contains flags that activate various features
type FeatureFlags struct {
	EnableCertificateSigningService bool
	EnableServiceMonitors           bool
	EnableTLSServiceMonitorConfig   bool
	EnablePrometheusAlerts          bool
	EnableGateway                   bool
	EnableGatewayRoute              bool
	EnableGrafanaLabsStats          bool
}

// Tenants contains the configuration per tenant and secrets for authn/authz.
// Secrets are required only for modes static and dynamic to reconcile the OIDC provider.
// Configs are required only for all modes to reconcile rules and gateway configuration.
type Tenants struct {
	Secrets []*TenantSecrets
	Configs map[string]TenantConfig
}

// TenantSecrets for clientID, clientSecret and issuerCAPath for tenant's authentication.
type TenantSecrets struct {
	TenantName   string
	ClientID     string
	ClientSecret string
	IssuerCAPath string
}

// TenantConfig for tenant authorizationconfig
type TenantConfig struct {
	OIDC      *TenantOIDCSpec
	OPA       *TenantOPASpec
	OpenShift *TenantOpenShiftSpec
	RuleFiles []string
}

// TenantOIDCSpec stub config for OIDC configuration options (e.g. used in static or dynamic mode)
type TenantOIDCSpec struct{}

// TenantOPASpec stub config for OPA configuration options (e.g. used in dynamic mode)
type TenantOPASpec struct{}

// TenantOpenShiftSpec config for OpenShift authentication options (e.g. used in openshift-logging mode)
type TenantOpenShiftSpec struct {
	CookieSecret string
}

// Ruler configuration for manifests generation.
type Ruler struct {
	Spec   *lokiv1beta1.RulerConfigSpec
	Secret *RulerSecret
}

// RulerSecret defines the ruler secret for remote write client auth
type RulerSecret struct {
	// Username, password for BasicAuth only
	Username string
	Password string

	// Credentials for Bearer Token
	BearerToken string
}
