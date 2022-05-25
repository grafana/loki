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

	Namespace             string
	Name                  string
	FrontendWorker        Address
	GossipRing            Address
	Querier               Address
	IndexGateway          Address
	Ruler                 Ruler
	StorageDirectory      string
	MaxConcurrent         MaxConcurrent
	WriteAheadLog         WriteAheadLog
	EnableRemoteReporting bool

	ObjectStorage storage.Options
}

// Address FQDN and port for a k8s service.
type Address struct {
	// FQDN is required
	FQDN string
	// Port is required
	Port int
}

// Ruler configuration
type Ruler struct {
	Enabled               bool
	RulesStorageDirectory string
}

// MaxConcurrent for concurrent query processing.
type MaxConcurrent struct {
	AvailableQuerierCPUCores int32
}

// WriteAheadLog for ingester processing
type WriteAheadLog struct {
	Directory             string
	IngesterMemoryRequest int64
}

// ReplayMemoryCeiling calculates 50% of the ingester memory
// for the ingester to use for the write-ahead-log capbability.
func (w WriteAheadLog) ReplayMemoryCeiling() string {
	value := int64(math.Ceil(float64(w.IngesterMemoryRequest) * float64(0.5)))
	return fmt.Sprintf("%d", value)
}
