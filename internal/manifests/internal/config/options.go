package config

import (
	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
)

// Options is used to render the loki-config.yaml file template
type Options struct {
	Stack lokiv1beta1.LokiStackSpec

	Namespace        string
	Name             string
	FrontendWorker   Address
	GossipRing       Address
	Querier          Address
	StorageDirectory string
	ObjectStorage    ObjectStorage
}

// Address FQDN and port for a k8s service.
type Address struct {
	// FQDN is required
	FQDN string
	// Port is required
	Port int
}

// ObjectStorage for storage config.
type ObjectStorage struct {
	Endpoint        string
	Region          string
	Buckets         string
	AccessKeyID     string
	AccessKeySecret string
}
