package compactor

import (
	"context"
	"fmt"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/storage/config"
	series_index "github.com/grafana/loki/v3/pkg/storage/stores/series/index"
)

const (
	logMetricName = "logs"
	separator     = "\000"
)

var (
	_ retention.SeriesCleaner = &seriesCleaner{}
)

func ForEachChunk(ctx context.Context, bucket *bbolt.Bucket, config config.PeriodConfig, callback retention.ChunkEntryCallback) error {
	labelsMapper, err := newSeriesLabelsMapper(bucket, config)
	if err != nil {
		return err
	}

	cursor := bucket.Cursor()
	var current retention.ChunkEntry

	for key, _ := cursor.First(); key != nil && ctx.Err() == nil; key, _ = cursor.Next() {
		ref, ok, err := parseChunkRef(decodeKey(key))
		if err != nil {
			return err
		}
		// skips anything else than chunk index entries.
		if !ok {
			continue
		}
		current.ChunkRef = ref
		current.Labels = labelsMapper.Get(ref.SeriesID, ref.UserID)
		deleteChunk, err := callback(current)
		if err != nil {
			return err
		}

		if deleteChunk {
			if err := cursor.Delete(); err != nil {
				return err
			}
		}
	}

	return ctx.Err()
}

type seriesCleaner struct {
	tableInterval model.Interval
	shards        map[uint32]string
	bucket        *bbolt.Bucket
	config        config.PeriodConfig
	schema        series_index.SeriesStoreSchema

	buf []byte
}

func newSeriesCleaner(bucket *bbolt.Bucket, config config.PeriodConfig, tableName string) *seriesCleaner {
	schema, _ := series_index.CreateSchema(config)
	var shards map[uint32]string

	if config.RowShards != 0 {
		shards = map[uint32]string{}
		for s := uint32(0); s <= config.RowShards; s++ {
			shards[s] = fmt.Sprintf("%02d", s)
		}
	}

	return &seriesCleaner{
		tableInterval: retention.ExtractIntervalFromTableName(tableName),
		schema:        schema,
		bucket:        bucket,
		buf:           make([]byte, 0, 1024),
		config:        config,
		shards:        shards,
	}
}

func (s *seriesCleaner) CleanupSeries(userID []byte, lbls labels.Labels) error {
	// We need to add metric name label as well if it is missing since the series ids are calculated including that.
	if lbls.Get(labels.MetricName) == "" {
		lbls = append(lbls, labels.Label{
			Name:  labels.MetricName,
			Value: logMetricName,
		})
	}
	_, indexEntries, err := s.schema.GetCacheKeysAndLabelWriteEntries(s.tableInterval.Start, s.tableInterval.End, string(userID), logMetricName, lbls, "")
	if err != nil {
		return err
	}

	for i := range indexEntries {
		for _, indexEntry := range indexEntries[i] {
			key := make([]byte, 0, len(indexEntry.HashValue)+len(separator)+len(indexEntry.RangeValue))
			key = append(key, []byte(indexEntry.HashValue)...)
			key = append(key, []byte(separator)...)
			key = append(key, indexEntry.RangeValue...)

			err := s.bucket.Delete(key)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
