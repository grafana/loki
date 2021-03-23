package config

// Options is used to render the loki-config.yaml file template
type Options struct {
	// FrontendWorker is required
	FrontendWorker Address
	// GossipRing is required
	GossipRing Address
	// Querier is required
	Querier Address
	// Storage is required
	StorageDirectory string

	// Namespace of the stack
	Namespace string
}

// Address FQDN and port for a k8s service.
type Address struct {
	// FQDN is required
	FQDN string
	// Port is required
	Port int
}
