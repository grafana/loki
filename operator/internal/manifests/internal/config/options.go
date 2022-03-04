package config

import (
	"fmt"
	"math"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

// Options is used to render the loki-config.yaml file template
type Options struct {
	Stack lokiv1beta1.LokiStackSpec

	Namespace        string
	Name             string
	FrontendWorker   Address
	GossipRing       Address
	Querier          Address
	IndexGateway     Address
	StorageDirectory string
	QueryParallelism Parallelism
	WriteAheadLog    WriteAheadLog

	ObjectStorage storage.Options
}

// Address FQDN and port for a k8s service.
type Address struct {
	// FQDN is required
	FQDN string
	// Port is required
	Port int
}

// Parallelism for query processing parallelism
// and rate limiting.
type Parallelism struct {
	QuerierCPULimits      int64
	QueryFrontendReplicas int32
}

// WriteAheadLog for ingester processing
type WriteAheadLog struct {
	Directory             string
	IngesterMemoryRequest int64
}

// Value calculates the floor of the division of
// querier cpu limits to the query frontend replicas
// available.
func (p Parallelism) Value() int32 {
	return int32(math.Floor(float64(p.QuerierCPULimits) / float64(p.QueryFrontendReplicas)))
}

// ReplayMemoryCeiling calculates 50% of the ingester memory
// for the ingester to use for the write-ahead-log capbability.
func (w WriteAheadLog) ReplayMemoryCeiling() string {
	value := int64(math.Ceil(float64(w.IngesterMemoryRequest) * float64(0.5)))
	return fmt.Sprintf("%d", value)
}
