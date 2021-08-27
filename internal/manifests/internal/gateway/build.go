package gateway

import (
	"bytes"
	"embed"
	"io/ioutil"
	"text/template"

	"github.com/ViaQ/logerr/kverrors"
)

const (
	// LokiGatewayTenantFileName is the name of the tenant config file in the configmap
	LokiGatewayTenantFileName = "tenants.yaml"
	// LokiGatewayRbacFileName is the name of the rbac config file in the configmap
	LokiGatewayRbacFileName = "rbac.yaml"
	// LokiGatewayMountDir is the path that is mounted from the configmap
	LokiGatewayMountDir = "/etc/lokistack-gateway"
	// LokiGatewayTLSDir is the path that is mounted from the configmap for TLS
	LokiGatewayTLSDir = "/var/run/tls"
)

var (
	//go:embed gateway-rbac.yaml
	lokiGatewayRbacYAMLTmplFile embed.FS

	//go:embed gateway-tenants.yaml
	lokiGatewayTenantsYAMLTmplFile embed.FS

	lokiGatewayRbacYAMLTmpl = template.Must(template.ParseFS(lokiGatewayRbacYAMLTmplFile, "gateway-rbac.yaml"))

	lokiGatewayTenantsYAMLTmpl = template.Must(template.ParseFS(lokiGatewayTenantsYAMLTmplFile, "gateway-tenants.yaml"))
)

// Build builds a loki gateway configuration files
func Build(opts Options) ([]byte, []byte, error) {
	// Build loki gateway rbac yaml
	w := bytes.NewBuffer(nil)
	err := lokiGatewayRbacYAMLTmpl.Execute(w, opts)
	if err != nil {
		return nil, nil, kverrors.Wrap(err, "failed to create loki gateway rbac configuration")
	}
	rbacCfg, err := ioutil.ReadAll(w)
	if err != nil {
		return nil, nil, kverrors.Wrap(err, "failed to read configuration from buffer")
	}
	// Build loki gateway tenants yaml
	w = bytes.NewBuffer(nil)
	err = lokiGatewayTenantsYAMLTmpl.Execute(w, opts)
	if err != nil {
		return nil, nil, kverrors.Wrap(err, "failed to create loki gateway tenants configuration")
	}
	tenantsCfg, err := ioutil.ReadAll(w)
	if err != nil {
		return nil, nil, kverrors.Wrap(err, "failed to read configuration from buffer")
	}
	return rbacCfg, tenantsCfg, nil
}
