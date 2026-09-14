package loki

import (
	"errors"

	"github.com/grafana/loki/v3/pkg/ingester/index"
	frontend "github.com/grafana/loki/v3/pkg/lokifrontend/frontend/v2"
	"github.com/grafana/loki/v3/pkg/storage/types"
)

func ValidateConfigCompatibility(c Config) []error {
	var errs []error
	for _, fn := range []func(Config) error{
		ensureInvertedIndexShardingCompatibility,
		ensureProtobufEncodingForAggregationSharding,
	} {
		if err := fn(c); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func ensureInvertedIndexShardingCompatibility(c Config) error {

	// TSDB is the only supported index type; other types are rejected at store
	// construction.
	for _, sc := range c.SchemaConfig.Configs {
		if sc.IndexType != types.IndexTypeTSDB {
			continue
		}
		if err := index.ValidateBitPrefixShardFactor(uint32(c.Ingester.IndexShards)); err != nil {
			return err
		}
	}
	return nil
}

func ensureProtobufEncodingForAggregationSharding(c Config) error {
	if len(c.QueryRange.ShardAggregations) > 0 && c.Frontend.FrontendV2.Encoding != frontend.EncodingProtobuf {
		return errors.New("shard_aggregation requires frontend.encoding=protobuf")
	}
	return nil
}
