package manifests

import (
	"strings"
	"time"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
	"github.com/grafana/loki/operator/internal/manifests/storage"
	"github.com/imdario/mergo"
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

	AlertingRules       []lokiv1.AlertingRule
	RecordingRules      []lokiv1.RecordingRule
	RulesConfigMapNames []string
	Ruler               Ruler

	ObjectStorage storage.Options

	OpenShiftOptions openshift.Options

	Server ServerConfig

	Tenants Tenants

	TLSProfile TLSProfileSpec
}

// HTTPConfig contains the http server configuration options for all Loki components.
type HTTPConfig struct {
	IdleTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	GatewayReadTimeout          time.Duration
	GatewayWriteTimeout         time.Duration
	GatewayUpstreamWriteTimeout time.Duration
}

// ServerConfig contains the server configuration options for all Loki components
type ServerConfig struct {
	HTTP *HTTPConfig
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

// NewServerConfig transforms the Loki LimitsSpec.Server options
// to a NewServerConfig by applying defaults and parsing durations.
func NewServerConfig(s *lokiv1.LimitsSpec) (ServerConfig, error) {
	defaults := ServerConfig{
		HTTP: &HTTPConfig{
			IdleTimeout:                 lokiDefaultHTTPIdleTimeout,
			ReadTimeout:                 lokiDefaultHTTPReadTimeout,
			WriteTimeout:                lokiDefaultHTTPWriteTimeout,
			GatewayReadTimeout:          gatewayDefaultReadTimeout,
			GatewayWriteTimeout:         gatewayDefaultWriteTimeout,
			GatewayUpstreamWriteTimeout: lokiDefaultHTTPWriteTimeout,
		},
	}

	if s == nil || s.Server == nil || s.Server.HTTP == nil {
		return defaults, nil
	}

	customCfg := ServerConfig{}

	if s.Server.HTTP.IdleTimeout != "" {
		idleTimeout, err := time.ParseDuration(s.Server.HTTP.IdleTimeout)
		if err != nil {
			return defaults, err
		}

		if customCfg.HTTP == nil {
			customCfg.HTTP = &HTTPConfig{}
		}

		customCfg.HTTP.IdleTimeout = idleTimeout
	}

	if s.Server.HTTP.ReadTimeout != "" {
		readTimeout, err := time.ParseDuration(s.Server.HTTP.ReadTimeout)
		if err != nil {
			return defaults, err
		}

		if customCfg.HTTP == nil {
			customCfg.HTTP = &HTTPConfig{}
		}

		customCfg.HTTP.ReadTimeout = readTimeout
		customCfg.HTTP.GatewayReadTimeout = readTimeout + gatewayReadWiggleRoom
	}

	if s.Server.HTTP.WriteTimeout != "" {
		writeTimeout, err := time.ParseDuration(s.Server.HTTP.WriteTimeout)
		if err != nil {
			return defaults, err
		}

		if customCfg.HTTP == nil {
			customCfg.HTTP = &HTTPConfig{}
		}

		customCfg.HTTP.WriteTimeout = writeTimeout
		customCfg.HTTP.GatewayWriteTimeout = writeTimeout + gatewayWriteWiggleRoom
		customCfg.HTTP.GatewayUpstreamWriteTimeout = writeTimeout
	}

	if err := mergo.Merge(&defaults, &customCfg, mergo.WithOverride); err != nil {
		return defaults, err
	}

	return defaults, nil
}
