package gateway

import (
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

// Options is used to render the rbac.yaml and tenants.yaml file template
type Options struct {
	Stack lokiv1.LokiStackSpec

	Namespace        string
	Name             string
	StorageDirectory string

	OpenShiftOptions openshift.Options
	TenantSecrets    []*Secret
}

// Secret for tenant's authentication.
type Secret struct {
	TenantName string
	OIDC       *OIDC
	MTLS       *MTLS
}

// OIDC secret for tenant's authentication.
type OIDC struct {
	ClientID     string
	ClientSecret string
	IssuerCAPath string
}

// MTLS config for tenant's authentication.
type MTLS struct {
	CAPath string
}
