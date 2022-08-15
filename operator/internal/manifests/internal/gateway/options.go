package gateway

import (
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
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

// Secret for clientID, clientSecret and issuerCAPath for tenant's authentication.
type Secret struct {
	TenantName   string
	ClientID     string
	ClientSecret string
	IssuerCAPath string
}
