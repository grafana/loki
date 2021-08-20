package retention

import (
	"fmt"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/chunk"
)

var (
	_ ChunkEntryIterator = &chunkIndexIterator{}
	_ SeriesCleaner      = &seriesCleaner{}
)

type ChunkEntry struct {
	ChunkRef
	Labels labels.Labels
}

type ChunkEntryIterator interface {
	Next() bool
	Entry() ChunkEntry
	// Delete deletes the current entry.
	Delete() error
	Err() error
}

type chunkIndexIterator struct {
	cursor  *bbolt.Cursor
	current ChunkEntry
	first   bool
	err     error

	labelsMapper *seriesLabelsMapper
}

func newChunkIndexIterator(bucket *bbolt.Bucket, config chunk.PeriodConfig) (*chunkIndexIterator, error) {
	labelsMapper, err := newSeriesLabelsMapper(bucket, config)
	if err != nil {
		return nil, err
	}
	return &chunkIndexIterator{
		cursor:       bucket.Cursor(),
		first:        true,
		labelsMapper: labelsMapper,
		current:      ChunkEntry{},
	}, nil
}

func (b *chunkIndexIterator) Err() error {
	return b.err
}

func (b *chunkIndexIterator) Entry() ChunkEntry {
	return b.current
}

func (b *chunkIndexIterator) Delete() error {
	return b.cursor.Delete()
}

func (b *chunkIndexIterator) Next() bool {
	var key []byte
	if b.first {
		key, _ = b.cursor.First()
		b.first = false
	} else {
		key, _ = b.cursor.Next()
	}
	for key != nil {
		ref, ok, err := parseChunkRef(decodeKey(key))
		if err != nil {
			b.err = err
			return false
		}
		// skips anything else than chunk index entries.
		if !ok {
			key, _ = b.cursor.Next()
			continue
		}
		b.current.ChunkRef = ref
		b.current.Labels = b.labelsMapper.Get(ref.SeriesID, ref.UserID)
		return true
	}
	return false
}

type SeriesCleaner interface {
	Cleanup(userID []byte, lbls labels.Labels) error
}

type seriesCleaner struct {
	tableInterval model.Interval
	shards        map[uint32]string
	bucket        *bbolt.Bucket
	config        chunk.PeriodConfig
	schema        chunk.SeriesStoreSchema

	buf []byte
}

func newSeriesCleaner(bucket *bbolt.Bucket, config chunk.PeriodConfig, tableName string) *seriesCleaner {
	baseSchema, _ := config.CreateSchema()
	schema := baseSchema.(chunk.SeriesStoreSchema)
	var shards map[uint32]string

	if config.RowShards != 0 {
		shards = map[uint32]string{}
		for s := uint32(0); s <= config.RowShards; s++ {
			shards[s] = fmt.Sprintf("%02d", s)
		}
	}

	return &seriesCleaner{
		tableInterval: ExtractIntervalFromTableName(tableName),
		schema:        schema,
		bucket:        bucket,
		buf:           make([]byte, 0, 1024),
		config:        config,
		shards:        shards,
	}
}

func (s *seriesCleaner) Cleanup(userID []byte, lbls labels.Labels) error {
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
