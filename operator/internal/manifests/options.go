package manifests

import (
	"strings"
	"time"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal"
	"github.com/grafana/loki/operator/internal/manifests/internal/config"
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

	AlertingRules       []lokiv1.AlertingRule
	RecordingRules      []lokiv1.RecordingRule
	RulesConfigMapNames []string
	Ruler               Ruler

	ObjectStorage storage.Options

	OpenShiftOptions openshift.Options

	Timeouts TimeoutConfig

	Tenants Tenants

	TLSProfile TLSProfileSpec
}

// GatewayTimeoutConfig contains the http server configuration options for all Loki components.
type GatewayTimeoutConfig struct {
	ReadTimeout          time.Duration
	WriteTimeout         time.Duration
	UpstreamWriteTimeout time.Duration
}

// TimeoutConfig contains the server configuration options for all Loki components
type TimeoutConfig struct {
	Loki    config.HTTPTimeoutConfig
	Gateway GatewayTimeoutConfig
}

// Tenants contains the configuration per tenant and secrets for authn/authz.
// Secrets are required only for modes static and dynamic to reconcile the OIDC provider.
// Configs are required only for all modes to reconcile rules and gateway configuration.
type Tenants struct {
	Secrets []*TenantSecrets
	Configs map[string]TenantConfig
}

// TenantSecrets for tenant's authentication.
type TenantSecrets struct {
	TenantName string
	OIDCSecret *OIDCSecret
	MTLSSecret *MTLSSecret
}

type OIDCSecret struct {
	ClientID     string
	ClientSecret string
	IssuerCAPath string
}

type MTLSSecret struct {
	CAPath string
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

// NewTimeoutConfig creates a TimeoutConfig from the QueryTimeout values in the spec's limits.
func NewTimeoutConfig(s *lokiv1.LimitsSpec) (TimeoutConfig, error) {
	if s == nil {
		return defaultTimeoutConfig, nil
	}

	if s.Global == nil && s.Tenants == nil {
		return defaultTimeoutConfig, nil
	}

	queryTimeout := lokiDefaultQueryTimeout
	if s.Global != nil && s.Global.QueryLimits != nil && s.Global.QueryLimits.QueryTimeout != "" {
		var err error
		globalQueryTimeout, err := time.ParseDuration(s.Global.QueryLimits.QueryTimeout)
		if err != nil {
			return TimeoutConfig{}, err
		}

		if globalQueryTimeout > queryTimeout {
			queryTimeout = globalQueryTimeout
		}
	}

	for _, tLimit := range s.Tenants {
		if tLimit.QueryLimits == nil || tLimit.QueryLimits.QueryTimeout == "" {
			continue
		}

		tenantQueryTimeout, err := time.ParseDuration(tLimit.QueryLimits.QueryTimeout)
		if err != nil {
			return TimeoutConfig{}, err
		}

		if tenantQueryTimeout > queryTimeout {
			queryTimeout = tenantQueryTimeout
		}
	}

	return calculateHTTPTimeouts(queryTimeout), nil
}

func calculateHTTPTimeouts(queryTimeout time.Duration) TimeoutConfig {
	idleTimeout := lokiDefaultHTTPIdleTimeout
	if queryTimeout < idleTimeout {
		idleTimeout = queryTimeout
	}

	readTimeout := queryTimeout / 10
	writeTimeout := queryTimeout + lokiQueryWriteDuration

	return TimeoutConfig{
		Loki: config.HTTPTimeoutConfig{
			IdleTimeout:  idleTimeout,
			ReadTimeout:  readTimeout,
			WriteTimeout: writeTimeout,
		},
		Gateway: GatewayTimeoutConfig{
			ReadTimeout:          readTimeout + gatewayReadDuration,
			WriteTimeout:         writeTimeout + gatewayWriteDuration,
			UpstreamWriteTimeout: writeTimeout,
		},
	}
}
