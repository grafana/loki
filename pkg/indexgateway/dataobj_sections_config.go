package indexgateway

import (
	"errors"
	"flag"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

// DataObjSectionsCacheType identifies the section resolution cache for stats grouping.
const DataObjSectionsCacheType = stats.CacheType("dataobj-sections")

// TableOfContentsWarmerConfig turns on the background ToC warmer and configures the resolver behind it.
type TableOfContentsWarmerConfig struct {
	// Enabled turns on the background warmer. It has no effect unless the section resolution API
	// (DataObjectSectionsConfig.Enabled) is also on; DataObjectSectionsConfig.Validate rejects the
	// contradictory combination.
	Enabled bool `yaml:"enabled"`

	metastore.TableOfContentsWarmResolverConfig `yaml:",inline"`
}

func (cfg *TableOfContentsWarmerConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, "index-gateway.dataobject-toc-warmer.enabled", false,
		"Experimental: keep recent data-object Table-of-Contents windows warm in memory so section resolution serves them from memory instead of reading them from object storage on the query path.")
	cfg.TableOfContentsWarmResolverConfig.RegisterFlagsWithPrefix("index-gateway.dataobject-toc-warmer.", f)
}

func (cfg *TableOfContentsWarmerConfig) Validate() error {
	if !cfg.Enabled {
		return nil
	}
	return cfg.TableOfContentsWarmResolverConfig.Validate()
}

// DataObjectSectionsConfig configures the ResolveDataObjectSections API and its cache.
type DataObjectSectionsConfig struct {
	// Enabled turns on the data-object section resolution API on this index-gateway. It also
	// requires the data-object storage to be configured so a metastore can be built.
	Enabled bool `yaml:"enabled"`

	// Cache configures the resolution cache. cache.New builds a tiered cache from it: the embedded
	// (in-process, memory-bounded) cache is L1 and memcached, if configured, is L2 with asynchronous
	// writes. The embedded cache is on by default; disable it under `embedded_cache`.
	Cache cache.Config `yaml:"cache"`

	// Metastore is injected at runtime by the module wiring (initIndexGatewayMetastore), not parsed from
	// YAML. NewIndexGateway builds the resolver over it. The metastore holds the section cache. It is nil
	// (and the resolver disabled) unless the index-gateway was wired with data-object storage.
	Metastore metastore.Metastore `yaml:"-"`

	// Warmer configures the background ToC warmer.
	Warmer TableOfContentsWarmerConfig `yaml:"toc_warmer"`
}

func (cfg *DataObjectSectionsConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, "index-gateway.dataobject-sections.enabled", false,
		"Enable the data-object section resolution API (ResolveDataObjectSections) on the index-gateway. Requires data-object storage to be configured.")
	cfg.Cache.RegisterFlagsWithPrefix("index-gateway.dataobject-sections.cache.", "", f)
	// Enable the in-memory cache by default.
	cfg.Cache.EmbeddedCache.Enabled = true
	cfg.Warmer.RegisterFlags(f)
}

// Validate checks the section-resolution settings. The warmer only warms ToCs for this API, so enabling it
// while the API is off is a misconfiguration that would silently warm nothing.
func (cfg *DataObjectSectionsConfig) Validate() error {
	if cfg.Warmer.Enabled && !cfg.Enabled {
		return errors.New("index-gateway.dataobject-toc-warmer.enabled requires index-gateway.dataobject-sections.enabled")
	}
	return cfg.Warmer.Validate()
}
