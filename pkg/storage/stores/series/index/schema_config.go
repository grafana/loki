package index

import (
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/storage/config"
)

const (
	secondsInDay      = int64(24 * time.Hour / time.Second)
	millisecondsInDay = int64(24 * time.Hour / time.Millisecond)
)

var (
	errInvalidSchemaVersion = errors.New("invalid schema version")
	errInvalidTablePeriod   = errors.New("the table period must be a multiple of 24h (1h for schema v1)")
)

// CreateSchema returns the schema defined by the PeriodConfig
func CreateSchema(cfg config.PeriodConfig) (SeriesStoreSchema, error) {
	buckets, bucketsPeriod := dailyBuckets(cfg), 24*time.Hour

	// Ensure the tables period is a multiple of the bucket period
	if cfg.IndexTables.Period > 0 && cfg.IndexTables.Period%bucketsPeriod != 0 {
		return nil, errInvalidTablePeriod
	}

	if cfg.ChunkTables.Period > 0 && cfg.ChunkTables.Period%bucketsPeriod != 0 {
		return nil, errInvalidTablePeriod
	}

	v, err := cfg.VersionAsInt()
	if err != nil {
		return nil, err
	}

	if v == 9 {
		return newSeriesStoreSchema(buckets, v9Entries{}), nil
	}
	if v >= 10 {
		if cfg.RowShards == 0 {
			return nil, fmt.Errorf("must have row_shards > 0 (current: %d) for schema (%s)", cfg.RowShards, cfg.Schema)
		}
		v10 := v10Entries{rowShards: cfg.RowShards}
		switch cfg.Schema {
		case "v10":
			return newSeriesStoreSchema(buckets, v10), nil
		case "v11":
			return newSeriesStoreSchema(buckets, v11Entries{v10}), nil
		case "v12":
			return newSeriesStoreSchema(buckets, v12Entries{v11Entries{v10}}), nil
		case "v13":
			return newSeriesStoreSchema(buckets, v13Entries{v12Entries{v11Entries{v10}}}), nil
		}
	}
	return nil, errInvalidSchemaVersion
}

// Bucket describes a range of time with a tableName and hashKey
type Bucket struct {
	from       uint32
	through    uint32
	tableName  string
	hashKey    string
	bucketSize uint32 // helps with deletion of series ids in series store. Size in milliseconds.
}

func dailyBuckets(cfg config.PeriodConfig) schemaBucketsFunc {
	return func(from, through model.Time, userID string) []Bucket {
		var (
			fromDay    = from.Unix() / secondsInDay
			throughDay = through.Unix() / secondsInDay
			result     = []Bucket{}
		)

		for i := fromDay; i <= throughDay; i++ {
			// The idea here is that the hash key contains the bucket start time (rounded to
			// the nearest day).  The range key can contain the offset from that, to the
			// (start/end) of the chunk. For chunks that span multiple buckets, these
			// offsets will be capped to the bucket boundaries, i.e. start will be
			// positive in the first bucket, then zero in the next etc.
			//
			// The reason for doing all this is to reduce the size of the time stamps we
			// include in the range keys - we use a uint32 - as we then have to base 32
			// encode it.

			relativeFrom := max(0, int64(from)-(i*millisecondsInDay))
			relativeThrough := min(millisecondsInDay, int64(through)-(i*millisecondsInDay))
			result = append(result, Bucket{
				from:       uint32(relativeFrom),
				through:    uint32(relativeThrough),
				tableName:  cfg.IndexTables.TableFor(model.TimeFromUnix(i * secondsInDay)),
				hashKey:    fmt.Sprintf("%s:d%d", userID, i),
				bucketSize: uint32(millisecondsInDay), // helps with deletion of series ids in series store
			})
		}
		return result
	}
}
