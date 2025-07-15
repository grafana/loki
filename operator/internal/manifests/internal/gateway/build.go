package gateway

import (
	"bytes"
	"embed"
	"io"
	"text/template"

	"github.com/ViaQ/logerr/v2/kverrors"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

const (
	// LokiGatewayTenantFileName is the name of the tenant config file in the configmap
	LokiGatewayTenantFileName = "tenants.yaml"
	// LokiGatewayRbacFileName is the name of the rbac config file in the configmap
	LokiGatewayRbacFileName = "rbac.yaml"
	// LokiGatewayRegoFileName is the name of the lokistack-gateway rego config file in the configmap
	LokiGatewayRegoFileName = "lokistack-gateway.rego"
	// LokiGatewayMountDir is the path that is mounted from the configmap
	LokiGatewayMountDir = "/etc/lokistack-gateway"
)

var (
	//go:embed gateway-rbac.yaml
	lokiGatewayRbacYAMLTmplFile embed.FS

	//go:embed gateway-tenants.yaml
	lokiGatewayTenantsYAMLTmplFile embed.FS

	//go:embed lokistack-gateway.rego
	lokiStackGatewayRegoTmplFile embed.FS

	lokiGatewayRbacYAMLTmpl = template.Must(template.ParseFS(lokiGatewayRbacYAMLTmplFile, "gateway-rbac.yaml"))

	lokiGatewayTenantsYAMLTmpl = template.Must(template.New("gateway-tenants.yaml").Funcs(template.FuncMap{
		"make_array": func(els ...any) []any { return els },
	}).ParseFS(lokiGatewayTenantsYAMLTmplFile, "gateway-tenants.yaml"))

	lokiStackGatewayRegoTmpl = template.Must(template.ParseFS(lokiStackGatewayRegoTmplFile, "lokistack-gateway.rego"))
)

// Build builds a loki gateway configuration files
func Build(opts Options) (rbacCfg []byte, tenantsCfg []byte, regoCfg []byte, err error) {
	// Build loki gateway rbac yaml
	w := bytes.NewBuffer(nil)
	err = lokiGatewayRbacYAMLTmpl.Execute(w, opts)
	if err != nil {
		return nil, nil, nil, kverrors.Wrap(err, "failed to create loki gateway rbac configuration")
	}
	rbacCfg, err = io.ReadAll(w)
	if err != nil {
		return nil, nil, nil, kverrors.Wrap(err, "failed to read configuration from buffer")
	}
	// Build loki gateway tenants yaml
	w = bytes.NewBuffer(nil)
	err = lokiGatewayTenantsYAMLTmpl.Execute(w, opts)
	if err != nil {
		return nil, nil, nil, kverrors.Wrap(err, "failed to create loki gateway tenants configuration")
	}
	tenantsCfg, err = io.ReadAll(w)
	if err != nil {
		return nil, nil, nil, kverrors.Wrap(err, "failed to read configuration from buffer")
	}
	// Build loki gateway observatorium rego for static mode
	if opts.Stack.Tenants.Mode == lokiv1.Static {
		w = bytes.NewBuffer(nil)
		err = lokiStackGatewayRegoTmpl.Execute(w, opts)
		if err != nil {
			return nil, nil, nil, kverrors.Wrap(err, "failed to create lokistack gateway rego configuration")
		}
		regoCfg, err = io.ReadAll(w)
		if err != nil {
			return nil, nil, nil, kverrors.Wrap(err, "failed to read configuration from buffer")
		}
		return rbacCfg, tenantsCfg, regoCfg, nil
	}
	return rbacCfg, tenantsCfg, nil, nil
}
