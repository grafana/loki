package gateway

import (
	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
)

// Options is used to render the rbac.yaml and tenants.yaml file template
type Options struct {
	Stack lokiv1beta1.LokiStackSpec

	Namespace        string
	Name             string
	StorageDirectory string
}
