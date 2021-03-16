package config

// lokiConfigOptions is used to render the loki-config.yaml file template
type Options struct {
	// FrontendWorker is required
	FrontendWorker FrontendWorker
	// GossipRing is required
	GossipRing GossipRing
	// Storage is required
	StorageDirectory string

	// Namespace of the stack
	Namespace string
}

type FrontendWorker struct {
	// FQDN is the required name of the service or fqdn WITHOUT the port
	FQDN string

	// Port is the required service port
	Port int
}

type GossipRing struct {
	// FQDN is the required name of the gossip ring service or fqdn WITHOUT the port
	FQDN string

	// Port is the required gossip ring service port
	Port int
}
