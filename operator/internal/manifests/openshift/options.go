package openshift

import (
	"fmt"
	"math/rand"
)

// Options is the set of internal template options for rendering
// the lokistack-gateway tenants configuration file when mode openshift-logging.
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
	LokiStackName                   string
	LokiStackNamespace              string
	GatewayName                     string
	GatewaySvcName                  string
	GatewaySvcTargetPort            string
	Labels                          map[string]string
	EnableServiceMonitors           bool
	EnableCertificateSigningService bool
}

// TenantData defines the existing cookieSecret for lokistack reconcile.
type TenantData struct {
	CookieSecret string
}

// NewOptions returns an openshift options struct.
func NewOptions(
	stackName, stackNamespace string,
	gwName, gwBaseDomain, gwSvcName, gwPortName string,
	gwLabels map[string]string,
	enableServiceMonitors bool,
	enableCertSigningService bool,
	tenantConfigMap map[string]TenantData,
) Options {
	host := ingressHost(stackName, stackNamespace, gwBaseDomain)

	var authn []AuthenticationSpec
	for _, name := range defaultTenants {
		cookieSecret := tenantConfigMap[name].CookieSecret
		if cookieSecret == "" {
			cookieSecret = newCookieSecret()
		}

		authn = append(authn, AuthenticationSpec{
			TenantName:     name,
			TenantID:       name,
			ServiceAccount: gwName,
			RedirectURL:    fmt.Sprintf("http://%s/openshift/%s/callback", host, name),
			CookieSecret:   cookieSecret,
		})
	}

	return Options{
		BuildOpts: BuildOptions{
			LokiStackName:                   stackName,
			LokiStackNamespace:              stackNamespace,
			GatewayName:                     gwName,
			GatewaySvcName:                  gwSvcName,
			GatewaySvcTargetPort:            gwPortName,
			Labels:                          gwLabels,
			EnableServiceMonitors:           enableServiceMonitors,
			EnableCertificateSigningService: enableCertSigningService,
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
