package loki

import (
	"fmt"
	"strings"

	"github.com/grafana/loki/v3/pkg/storage/bucket"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util"
)

func validateSchemaRequirements(c *Config) []error {
	var errs []error
	p := config.ActivePeriodConfig(c.SchemaConfig.Configs)

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
	if c.LimitsConfig.AllowStructuredMetadata && c.SchemaConfig.Configs[p].IndexType != types.IndexTypeTSDB {
		errs = append(errs, fmt.Errorf("CONFIG ERROR: `tsdb` index type is required to store Structured Metadata and use native OTLP ingestion, your index type is `%s` (defined in the `store` parameter of the schema_config). Set `allow_structured_metadata: false` in the `limits_config` section or set the command line argument `-validation.allow-structured-metadata=false` and restart Loki. Then proceed to update the schema to use index type `tsdb` before re-enabling this config, search for 'Storage Schema' in the docs for the schema update procedure", c.SchemaConfig.Configs[p].IndexType))
	}
	return errs
}

func validateDirectoriesExist(c *Config) []error {
	var errs []error
	// If TSDB index exists in any index period, make sure the storage locations are configured
	for _, s := range c.SchemaConfig.Configs {
		if s.IndexType == types.IndexTypeTSDB {
			if c.StorageConfig.TSDBShipperConfig.ActiveIndexDirectory == "" {
				errs = append(errs, fmt.Errorf("CONFIG ERROR: `tsdb` index type is configured in at least one schema period, however, `storage_config`, `tsdb_shipper`, `active_index_directory` is not set, please set this directly or set `path_prefix:` in the `common:` section"))
			}
			if c.StorageConfig.TSDBShipperConfig.CacheLocation == "" {
				errs = append(errs, fmt.Errorf("CONFIG ERROR: `tsdb` index type is configured in at least one schema period, however, `storage_config`, `tsdb_shipper`, `cache_location` is not set, please set this directly or set `path_prefix:` in the `common:` section"))
			}
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
		if c.StorageConfig.UseThanosObjstore {
			if !util.StringsContain(bucket.SupportedBackends, cfg.ObjectType) && !c.StorageConfig.ObjectStore.NamedStores.Exists(cfg.ObjectType) {
				errs = append(errs, fmt.Errorf("unrecognized `object_store` type `%s`, which also does not match any named_stores. Choose one of: %s. Or choose a named_store", cfg.ObjectType, strings.Join(bucket.SupportedBackends, ", ")))
			}
		} else if !util.StringsContain(types.SupportedStorageTypes, cfg.ObjectType) {
			if !c.StorageConfig.NamedStores.Exists(cfg.ObjectType) {
				errs = append(errs, fmt.Errorf("unrecognized `object_store` type `%s`, which also does not match any named_stores. Choose one of: %s. Or choose a named_store", cfg.ObjectType, strings.Join(types.SupportedStorageTypes, ", ")))
			}
		}
	}
	return errs
}
