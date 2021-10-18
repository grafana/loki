package gateway

import (
	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/ViaQ/loki-operator/internal/manifests/openshift"
)

// Options is used to render the rbac.yaml and tenants.yaml file template
type Options struct {
	Stack lokiv1beta1.LokiStackSpec

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
