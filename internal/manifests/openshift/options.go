package openshift

import (
	"fmt"
	"math/rand"

	"github.com/ViaQ/loki-operator/internal/manifests/gateway"
	"github.com/google/uuid"
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
	LokiStackName        string
	GatewayName          string
	GatewayNamespace     string
	GatewaySvcName       string
	GatewaySvcTargetPort string
	Labels               map[string]string
}

// NewOptions returns an openshift options struct.
func NewOptions(stackName, gwName, gwNamespace, gwHost, gwSvcName, gwPortName string, gwLabels map[string]string) (Options, error) {
	host, err := gateway.IngressHost(stackName, gwNamespace, gwHost)
	if err != nil {
		return Options{}, err
	}

	var authn []AuthenticationSpec
	for _, name := range defaultTenants {
		authn = append(authn, AuthenticationSpec{
			TenantName:     name,
			TenantID:       uuid.New().String(),
			ServiceAccount: gwName,
			RedirectURL:    fmt.Sprintf("http://%s/openshift/%s/callback", host, name),
			CookieSecret:   newCookieSecret(),
		})
	}

	return Options{
		BuildOpts: BuildOptions{
			LokiStackName:        stackName,
			GatewayName:          gwName,
			GatewayNamespace:     gwNamespace,
			GatewaySvcName:       gwSvcName,
			GatewaySvcTargetPort: gwPortName,
			Labels:               gwLabels,
		},
		Authentication: authn,
		Authorization: AuthorizationSpec{
			OPAUrl: fmt.Sprintf("http://localhost:%d/v1/data/%s/allow", GatewayOPAHTTPPort, opaDefaultPackage),
		},
	}, nil
}

func newCookieSecret() string {
	b := make([]rune, cookieSecretLength)
	for i := range b {
		b[i] = allowedRunes[rand.Intn(len(allowedRunes))]
	}

	return string(b)
}
