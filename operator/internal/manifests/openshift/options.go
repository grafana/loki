package openshift

import (
	"fmt"
	"math/rand"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
)

// Options is the set of internal template options for rendering
// the lokistack-gateway tenants configuration file when mode openshift-logging or openshift-network.
type Options struct {
	BuildOpts      BuildOptions
	Authentication []AuthenticationSpec
	Authorization  AuthorizationSpec
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
	LokiStackName        string
	LokiStackNamespace   string
	GatewayName          string
	GatewaySvcName       string
	GatewaySvcTargetPort string
	RulerName            string
	Labels               map[string]string
	AlertManagerEnabled  bool
}

// TenantData defines the existing cookieSecret for lokistack reconcile.
type TenantData struct {
	CookieSecret string
}

// NewOptions returns an openshift options struct.
func NewOptions(
	mode lokiv1.ModeType,
	stackName, stackNamespace string,
	gwName, gwBaseDomain, gwSvcName, gwPortName string,
	gwLabels map[string]string,
	tenantConfigMap map[string]TenantData,
	rulerName string,
) Options {
	host := ingressHost(stackName, stackNamespace, gwBaseDomain)

	var authn []AuthenticationSpec

	tenants := GetTenants(mode)
	for _, name := range tenants {
		cookieSecret := tenantConfigMap[name].CookieSecret
		if cookieSecret == "" {
			cookieSecret = newCookieSecret()
		}

		authn = append(authn, AuthenticationSpec{
			TenantName:     name,
			TenantID:       name,
			ServiceAccount: gwName,
			RedirectURL:    fmt.Sprintf("https://%s/openshift/%s/callback", host, name),
			CookieSecret:   cookieSecret,
		})
	}

	return Options{
		BuildOpts: BuildOptions{
			LokiStackName:        stackName,
			LokiStackNamespace:   stackNamespace,
			GatewayName:          gwName,
			GatewaySvcName:       gwSvcName,
			GatewaySvcTargetPort: gwPortName,
			Labels:               gwLabels,
			RulerName:            rulerName,
		},
		Authentication: authn,
		Authorization: AuthorizationSpec{
			OPAUrl: fmt.Sprintf("http://localhost:%d/v1/data/%s/allow", GatewayOPAHTTPPort, opaDefaultPackage),
		},
	}
}

func newCookieSecret() string {
	b := make([]rune, cookieSecretLength)
	for i := range b {
		b[i] = allowedRunes[rand.Intn(len(allowedRunes))]
	}

	return string(b)
}
