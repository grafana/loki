package loki

import (
	"errors"
	"fmt"

	"github.com/grafana/loki/pkg/ingester/index"
	frontend "github.com/grafana/loki/pkg/lokifrontend/frontend/v2"
	"github.com/grafana/loki/pkg/storage/config"
)

func ValidateConfigCompatibility(c Config) error {
	for _, fn := range []func(Config) error{
		ensureInvertedIndexShardingCompatibility,
		ensureProtobufEncodingForQuantileOverTimeSharding,
	} {
		if err := fn(c); err != nil {
			return err
		}
	}
	return nil
}

func ensureInvertedIndexShardingCompatibility(c Config) error {

	for i, sc := range c.SchemaConfig.Configs {
		switch sc.IndexType {
		case config.TSDBType:
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

func ensureProtobufEncodingForQuantileOverTimeSharding(c Config) error {
	if c.QueryRange.ShardQuantileOverTime && c.Frontend.FrontendV2.Encoding != frontend.EncodingProtobuf {
		return errors.New("quantile_over_time_sharding=true requires frontend.encoding=protobuf")
	}
	return nil
}
