package openshift

import (
	"fmt"
	"math/rand"
	"time"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/config"
)

// Options is the set of internal template options for rendering
// the lokistack-gateway tenants configuration file when mode openshift-logging or openshift-network.
type Options struct {
	BuildOpts      BuildOptions
	Authentication []AuthenticationSpec
	Authorization  AuthorizationSpec
	TokenCCOAuth   *config.TokenCCOAuthConfig
}

// AuthenticationSpec describes the authentication specification
// for a single tenant to authenticate it's subjects through OpenShift Auth.
type AuthenticationSpec struct {
	TenantName     string
	TenantID       string
	ServiceAccount string
	RedirectURL    string
	CookieSecret   string
}

// AuthorizationSpec describes the authorization specification
// for all tenants to authorize access for it's subjects through the
// opa-openshift sidecar.
type AuthorizationSpec struct {
	OPAUrl string
}

// BuildOptions represents the set of options required to build
// extra lokistack gateway k8s objects (e.g. ServiceAccount, Route, RBAC)
// on openshift.
type BuildOptions struct {
	LokiStackName                   string
	LokiStackNamespace              string
	GatewayName                     string
	GatewaySvcName                  string
	GatewaySvcTargetPort            string
	GatewayRouteTimeout             time.Duration
	RulerName                       string
	Labels                          map[string]string
	AlertManagerEnabled             bool
	UserWorkloadAlertManagerEnabled bool
}

// TenantData defines the existing cookieSecret for lokistack reconcile.
type TenantData struct {
	CookieSecret string
}

// NewOptions returns an openshift options struct.
func NewOptions(
	stackName, stackNamespace string,
	gwName, gwSvcName, gwPortName string,
	gwWriteTimeout time.Duration,
	gwLabels map[string]string,
	rulerName string,
) *Options {
	return &Options{
		BuildOpts: BuildOptions{
			LokiStackName:        stackName,
			LokiStackNamespace:   stackNamespace,
			GatewayName:          gwName,
			GatewaySvcName:       gwSvcName,
			GatewaySvcTargetPort: gwPortName,
			GatewayRouteTimeout:  gwWriteTimeout + gatewayRouteTimeoutExtension,
			Labels:               gwLabels,
			RulerName:            rulerName,
		},
	}
}

func (o *Options) WithTenantsForMode(mode lokiv1.ModeType, gwBaseDomain string, tenantConfigMap map[string]TenantData) *Options {
	var (
		authn []AuthenticationSpec
		authz AuthorizationSpec
		host  = ingressHost(o.BuildOpts.LokiStackName, o.BuildOpts.LokiStackNamespace, gwBaseDomain)
	)

	tenants := GetTenants(mode)
	for _, name := range tenants {
		cookieSecret := tenantConfigMap[name].CookieSecret
		if cookieSecret == "" {
			cookieSecret = newCookieSecret()
		}

		authn = append(authn, AuthenticationSpec{
			TenantName:     name,
			TenantID:       name,
			ServiceAccount: o.BuildOpts.GatewayName,
			RedirectURL:    fmt.Sprintf("https://%s/openshift/%s/callback", host, name),
			CookieSecret:   cookieSecret,
		})
	}

	if len(tenants) > 0 {
		authz = AuthorizationSpec{
			OPAUrl: fmt.Sprintf("http://localhost:%d/v1/data/%s/allow", GatewayOPAHTTPPort, opaDefaultPackage),
		}
	}

	o.Authentication = authn
	o.Authorization = authz

	return o
}

func newCookieSecret() string {
	b := make([]rune, cookieSecretLength)
	for i := range b {
		b[i] = allowedRunes[rand.Intn(len(allowedRunes))]
	}

	return string(b)
}
