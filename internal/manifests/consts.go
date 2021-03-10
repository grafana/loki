package manifests

const (
	ContainerImage = "docker.io/grafana/loki:2.1.0"

	GossipPort = 7946
	MetricsPort = 3100
	GRPCPort = 9095

	ServiceNameDistributorHTTP = "loki-distributor-http"
	ServiceNameDistributorGRPC = "loki-distributor-grpc"

	ServiceNameGossipRing = "loki-gossip-ring"
)
