package retention

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"

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
	Cleanup(seriesID []byte, userID []byte) error
}

type seriesCleaner struct {
	bucketTimestamps []string
	shards           map[uint32]string
	cursor           *bbolt.Cursor
	config           chunk.PeriodConfig

	buf []byte
}

func newSeriesCleaner(bucket *bbolt.Bucket, config chunk.PeriodConfig) *seriesCleaner {
	var (
		fromDay          = config.From.Time.Unix() / int64(config.IndexTables.Period/time.Second)
		throughDay       = config.From.Add(config.IndexTables.Period).Unix() / int64(config.IndexTables.Period/time.Second)
		bucketTimestamps = []string{}
	)
	for i := fromDay; i <= throughDay; i++ {
		bucketTimestamps = append(bucketTimestamps, fmt.Sprintf("d%d", i))
	}
	var shards map[uint32]string
	if config.RowShards != 0 {
		shards = map[uint32]string{}
		for s := uint32(0); s <= config.RowShards; s++ {
			shards[s] = fmt.Sprintf("%02d", s)
		}
	}
	return &seriesCleaner{
		bucketTimestamps: bucketTimestamps,
		cursor:           bucket.Cursor(),
		buf:              make([]byte, 0, 1024),
		config:           config,
		shards:           shards,
	}
}

func (s *seriesCleaner) Cleanup(seriesID []byte, userID []byte) error {
	for _, timestamp := range s.bucketTimestamps {
		// build the chunk ref prefix
		s.buf = s.buf[:0]
		if s.config.Schema != "v9" {
			shard := binary.BigEndian.Uint32(seriesID) % s.config.RowShards
			s.buf = append(s.buf, unsafeGetBytes(s.shards[shard])...)
			s.buf = append(s.buf, ':')
		}
		s.buf = append(s.buf, userID...)
		s.buf = append(s.buf, ':')
		s.buf = append(s.buf, unsafeGetBytes(timestamp)...)
		s.buf = append(s.buf, ':')
		s.buf = append(s.buf, seriesID...)

		if key, _ := s.cursor.Seek(s.buf); key != nil && bytes.HasPrefix(key, s.buf) {
			// this series still have chunk entries we can't cleanup
			continue
		}
		// we don't have any chunk ref for that series let's delete all label index entries
		s.buf = s.buf[:0]
		if s.config.Schema != "v9" {
			shard := binary.BigEndian.Uint32(seriesID) % s.config.RowShards
			s.buf = append(s.buf, unsafeGetBytes(s.shards[shard])...)
			s.buf = append(s.buf, ':')
		}
		s.buf = append(s.buf, userID...)
		s.buf = append(s.buf, ':')
		s.buf = append(s.buf, unsafeGetBytes(timestamp)...)
		s.buf = append(s.buf, ':')
		s.buf = append(s.buf, unsafeGetBytes(logMetricName)...)

		// delete all seriesRangeKeyV1 and labelSeriesRangeKeyV1 via prefix
		// todo(cyriltovena) we might be able to encode index key instead of parsing all label entries for faster delete.
		for key, _ := s.cursor.Seek(s.buf); key != nil && bytes.HasPrefix(key, s.buf); key, _ = s.cursor.Next() {

			parsedSeriesID, ok, err := parseLabelIndexSeriesID(decodeKey(key))
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			if !bytes.Equal(seriesID, parsedSeriesID) {
				continue
			}
			if err := s.cursor.Delete(); err != nil {
				return err
			}
		}
	}
	return nil
}
