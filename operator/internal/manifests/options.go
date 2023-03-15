package manifests

import (
	"strings"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

// Options is a set of configuration values to use when building manifests such as resource sizes, etc.
// Most of this should be provided - either directly or indirectly - by the user.
type Options struct {
	Name                   string
	Namespace              string
	Image                  string
	GatewayImage           string
	GatewayBaseDomain      string
	ConfigSHA1             string
	CertRotationRequiredAt string

	Gates                configv1.FeatureGates
	Stack                lokiv1.LokiStackSpec
	ResourceRequirements internal.ComponentResources

	AlertingRules  []lokiv1.AlertingRule
	RecordingRules []lokiv1.RecordingRule
	Ruler          Ruler

	ObjectStorage storage.Options

	OpenShiftOptions openshift.Options

	Tenants Tenants

	TLSProfile TLSProfileSpec
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
	Spec   *lokiv1.RulerConfigSpec
	Secret *RulerSecret
}

// RulerSecret defines the ruler secret for remote write client auth
type RulerSecret struct {
	// Username for basic authentication only.
	Username string
	// Password for basic authentication only.
	Password string
	// BearerToken contains the token used for bearer authentication.
	BearerToken string
}

// TLSProfileSpec is the desired behavior of a TLSProfileType.
type TLSProfileSpec struct {
	// Ciphers is used to specify the cipher algorithms that are negotiated
	// during the TLS handshake.
	Ciphers []string
	// MinTLSVersion is used to specify the minimal version of the TLS protocol
	// that is negotiated during the TLS handshake.
	MinTLSVersion string
}

// TLSCipherSuites transforms TLSProfileSpec.Ciphers from a slice
// to a string of elements joined with a comma.
func (o Options) TLSCipherSuites() string {
	return strings.Join(o.TLSProfile.Ciphers, ",")
}
