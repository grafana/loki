package loki

import (
	"fmt"
	"strings"

	"github.com/grafana/loki/v3/pkg/storage/bucket"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util"
)

func validateBackendAndLegacyReadMode(c *Config) []error {
	var errs []error
	// Honor the legacy scalable deployment topology
	if c.LegacyReadTarget {
		if c.isTarget(Backend) {
			errs = append(errs, fmt.Errorf("CONFIG ERROR: invalid target, cannot run backend target with legacy read mode"))
		}
	}
	return errs
}

func validateSchemaRequirements(c *Config) []error {
	var errs []error
	p := config.ActivePeriodConfig(c.SchemaConfig.Configs)

	// If the active index type is not TSDB (which does not use an index cache)
	// and the index queries cache is configured
	// and the chunk retain period is less than the validity period of the index cache
	// throw an error.
	if c.SchemaConfig.Configs[p].IndexType != types.TSDBType &&
		cache.IsCacheConfigured(c.StorageConfig.IndexQueriesCacheConfig) &&
		c.Ingester.RetainPeriod < c.StorageConfig.IndexCacheValidity {
		errs = append(errs, fmt.Errorf("CONFIG ERROR: the active index is %s which is configured to use an `index_cache_validity` (TTL) of %s, however the chunk_retain_period is %s which is LESS than the `index_cache_validity`. This can lead to query gaps, please configure the `chunk_retain_period` to be greater than the `index_cache_validity`", c.SchemaConfig.Configs[p].IndexType, c.StorageConfig.IndexCacheValidity, c.Ingester.RetainPeriod))
	}

	// Schema version 13 is required to use structured metadata
	version, err := c.SchemaConfig.Configs[p].VersionAsInt()
	if err != nil {
		errs = append(errs, fmt.Errorf("CONFIG ERROR: invalid schema version at schema position %d", p))
		// We can't continue if we can't parse the schema version
		return errs
	}
	if c.LimitsConfig.AllowStructuredMetadata && version < 13 {
		errs = append(errs, fmt.Errorf("CONFIG ERROR: schema v13 is required to store Structured Metadata and use native OTLP ingestion, your schema version is %s. Set `allow_structured_metadata: false` in the `limits_config` section or set the command line argument `-validation.allow-structured-metadata=false` and restart Loki. Then proceed to update to schema v13 or newer before re-enabling this config, search for 'Storage Schema' in the docs for the schema update procedure", c.SchemaConfig.Configs[p].Schema))
	}
	// TSDB index is required to use structured metadata
	if c.LimitsConfig.AllowStructuredMetadata && c.SchemaConfig.Configs[p].IndexType != types.TSDBType {
		errs = append(errs, fmt.Errorf("CONFIG ERROR: `tsdb` index type is required to store Structured Metadata and use native OTLP ingestion, your index type is `%s` (defined in the `store` parameter of the schema_config). Set `allow_structured_metadata: false` in the `limits_config` section or set the command line argument `-validation.allow-structured-metadata=false` and restart Loki. Then proceed to update the schema to use index type `tsdb` before re-enabling this config, search for 'Storage Schema' in the docs for the schema update procedure", c.SchemaConfig.Configs[p].IndexType))
	}
	return errs
}

func validateDirectoriesExist(c *Config) []error {
	var errs []error
	// If TSDB index exists in any index period, make sure the storage locations are configured, when folks upgrade from boltdb-shipper than can hit this and it fails with a confusing stack trace
	for _, s := range c.SchemaConfig.Configs {
		if s.IndexType == types.TSDBType {
			if c.StorageConfig.TSDBShipperConfig.ActiveIndexDirectory == "" {
				errs = append(errs, fmt.Errorf("CONFIG ERROR: `tsdb` index type is configured in at least one schema period, however, `storage_config`, `tsdb_shipper`, `active_index_directory` is not set, please set this directly or set `path_prefix:` in the `common:` section"))
			}
			if c.StorageConfig.TSDBShipperConfig.CacheLocation == "" {
				errs = append(errs, fmt.Errorf("CONFIG ERROR: `tsdb` index type is configured in at least one schema period, however, `storage_config`, `tsdb_shipper`, `cache_location` is not set, please set this directly or set `path_prefix:` in the `common:` section"))
			}
		}
	}

	// If boltdb-shipper index exists in any index period, make sure the storage locations are configured, when folks upgrade from boltdb-shipper than can hit this and it fails with a confusing stack trace
	for _, s := range c.SchemaConfig.Configs {
		if s.IndexType == types.BoltDBShipperType {
			if c.StorageConfig.BoltDBShipperConfig.ActiveIndexDirectory == "" {
				errs = append(errs, fmt.Errorf("CONFIG ERROR: `boltdb-shipper` index type is configured in at least one schema period, however, `storage_config`, `boltdb_shipper`, `active_index_directory` is not set, please set this directly or set `path_prefix:` in the `common:` section"))
			}
			if c.StorageConfig.BoltDBShipperConfig.CacheLocation == "" {
				errs = append(errs, fmt.Errorf("CONFIG ERROR: `boltdb-shipper` index type is configured in at least one schema period, however, `storage_config`, `boltdb_shipper`, `cache_location` is not set, please set this directly or set `path_prefix:` in the `common:` section"))
			}
		}
	}

	for _, s := range c.SchemaConfig.Configs {
		if s.IndexType == types.TSDBType || s.IndexType == types.BoltDBShipperType {
			if c.CompactorConfig.WorkingDirectory == "" {
				errs = append(errs, fmt.Errorf("CONFIG ERROR: `compactor:` `working_directory:` is empty, please set a valid directory or set `path_prefix:` in the `common:` section"))
			}
		}
	}
	return errs
}

func validateSchemaValues(c *Config) []error {
	var errs []error
	for _, cfg := range c.SchemaConfig.Configs {
		if !util.StringsContain(types.TestingStorageTypes, cfg.IndexType) &&
			!util.StringsContain(types.SupportedIndexTypes, cfg.IndexType) &&
			!util.StringsContain(types.DeprecatedIndexTypes, cfg.IndexType) {
			errs = append(errs, fmt.Errorf("unrecognized `store` (index) type `%s`, choose one of: %s", cfg.IndexType, strings.Join(types.SupportedIndexTypes, ", ")))
		}

		if c.StorageConfig.UseThanosObjstore {
			if !util.StringsContain(bucket.SupportedBackends, cfg.ObjectType) && !c.StorageConfig.ObjectStore.NamedStores.Exists(cfg.ObjectType) {
				errs = append(errs, fmt.Errorf("unrecognized `object_store` type `%s`, which also does not match any named_stores. Choose one of: %s. Or choose a named_store", cfg.ObjectType, strings.Join(bucket.SupportedBackends, ", ")))
			}
		} else if !util.StringsContain(types.TestingStorageTypes, cfg.ObjectType) &&
			!util.StringsContain(types.SupportedStorageTypes, cfg.ObjectType) &&
			!util.StringsContain(types.DeprecatedStorageTypes, cfg.ObjectType) {
			if !c.StorageConfig.NamedStores.Exists(cfg.ObjectType) {
				errs = append(errs, fmt.Errorf("unrecognized `object_store` type `%s`, which also does not match any named_stores. Choose one of: %s. Or choose a named_store", cfg.ObjectType, strings.Join(types.SupportedStorageTypes, ", ")))
			}
		}
	}
	return errs
}
