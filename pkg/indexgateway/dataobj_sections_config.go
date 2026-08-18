package indexgateway

import (
	"flag"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

const dataObjSectionsCacheType = stats.CacheType("dataobj-sections")

// DataObjectSectionsConfig configures the ResolveDataObjectSections API and its cache.
type DataObjectSectionsConfig struct {
	// Enabled turns on the data-object section resolution API on this index-gateway. It also
	// requires the data-object storage to be configured so a metastore can be built.
	Enabled bool `yaml:"enabled"`

	// Cache configures the resolution cache. cache.New builds a tiered cache from it: the embedded
	// (in-process, memory-bounded) cache is L1 and memcached, if configured, is L2 with asynchronous
	// writes. The embedded cache is on by default; disable it under `embedded_cache`.
	Cache cache.Config `yaml:"cache"`

	// Metastore is injected at runtime by the module wiring (initIndexGateway), not parsed from YAML.
	// NewIndexGateway builds the resolver over it. It is nil (and the resolver disabled) unless the
	// index-gateway was wired with data-object storage.
	Metastore metastore.Metastore `yaml:"-"`
}

func (cfg *DataObjectSectionsConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, "index-gateway.dataobject-sections.enabled", false,
		"Enable the data-object section resolution API (ResolveDataObjectSections) on the index-gateway. Requires data-object storage to be configured.")
	cfg.Cache.RegisterFlagsWithPrefix("index-gateway.dataobject-sections.cache.", "", f)
	// Enable the in-memory cache by default.
	cfg.Cache.EmbeddedCache.Enabled = true
}
