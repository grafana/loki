package compactor

import (
	"bytes"
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
	_ retention.IndexCleaner = &seriesCleaner{}
)

func ForEachSeries(ctx context.Context, bucket *bbolt.Bucket, config config.PeriodConfig, callback retention.SeriesCallback) error {
	labelsMapper, err := newSeriesLabelsMapper(bucket, config)
	if err != nil {
		return err
	}

	cursor := bucket.Cursor()
	var current retention.Series

	for key, _ := cursor.First(); key != nil && ctx.Err() == nil; key, _ = cursor.Next() {
		ref, ok, err := parseChunkRef(decodeKey(key))
		if err != nil {
			return err
		}
		// skips anything else than chunk index entries.
		if !ok {
			continue
		}

		if len(current.Chunks()) == 0 {
			current.Reset(ref.SeriesID, ref.UserID, labelsMapper.Get(ref.SeriesID, ref.UserID))
		} else if !bytes.Equal(current.UserID(), ref.UserID) || !bytes.Equal(current.SeriesID(), ref.SeriesID) {
			err = callback(current)
			if err != nil {
				return err
			}

			current.Reset(ref.SeriesID, ref.UserID, labelsMapper.Get(ref.SeriesID, ref.UserID))
		}

		current.AppendChunks(retention.Chunk{
			ChunkID: ref.ChunkID,
			From:    ref.From,
			Through: ref.Through,
		})
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if len(current.Chunks()) != 0 {
		err = callback(current)
		if err != nil {
			return err
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

func (s *seriesCleaner) RemoveChunk(from, through model.Time, userID []byte, lbls labels.Labels, chunkID []byte) error {
	// We need to add metric name label as well if it is missing since the series ids are calculated including that.
	if lbls.Get(labels.MetricName) == "" {
		lbls = append(lbls, labels.Label{
			Name:  labels.MetricName,
			Value: logMetricName,
		})
	}

	indexEntries, err := s.schema.GetChunkWriteEntries(from, through, string(userID), logMetricName, lbls, string(chunkID))
	if err != nil {
		return err
	}

	for _, indexEntry := range indexEntries {
		key := make([]byte, 0, len(indexEntry.HashValue)+len(separator)+len(indexEntry.RangeValue))
		key = append(key, []byte(indexEntry.HashValue)...)
		key = append(key, []byte(separator)...)
		key = append(key, indexEntry.RangeValue...)

		err := s.bucket.Delete(key)
		if err != nil {
			return err
		}
	}

	return nil
}
