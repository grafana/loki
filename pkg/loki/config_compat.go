package loki

import (
	"errors"
	"fmt"

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

	for i, sc := range c.SchemaConfig.Configs {
		switch sc.IndexType {
		case types.TSDBType:
			if err := index.ValidateBitPrefixShardFactor(uint32(c.Ingester.IndexShards)); err != nil {
				return err
			}
		default:
			if sc.RowShards > 0 && c.Ingester.IndexShards%int(sc.RowShards) > 0 {
				return fmt.Errorf(
					"incompatible ingester index shards (%d) and period config row shard factor (%d) for period config at index (%d). The ingester factor must be evenly divisible by all period config factors",
					c.Ingester.IndexShards,
					sc.RowShards,
					i,
				)
			}
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
