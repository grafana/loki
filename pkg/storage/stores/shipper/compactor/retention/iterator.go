package retention

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/prometheus/prometheus/pkg/labels"
	"go.etcd.io/bbolt"
)

var (
	_ ChunkEntryIterator = &chunkIndexIterator{}
	_ Series             = &series{}
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

type Series interface {
	Buckets(userID []byte) []Bucket
}

type Bucket interface {
	ContainsChunkFor(seriesID []byte) bool
	LabelEntries(seriesID []byte) LabelEntryIterator
}

type LabelEntryIterator interface {
	Next() bool
	Entry() *LabelIndexRef
	// Delete deletes the current entry.
	Delete() error
	Err() error
}

type series struct {
	bucket *bbolt.Bucket
	config chunk.PeriodConfig
}

func newSeries(bucket *bbolt.Bucket, config chunk.PeriodConfig) *series {
	return &series{
		bucket: bucket,
		config: config,
	}
}

func (s *series) Buckets(userID []byte) []Bucket {
	bucketHashes := allBucketsHashes(s.config, unsafeGetString(userID))
	res := make([]Bucket, 0, len(bucketHashes))
	for _, h := range bucketHashes {
		res = append(res, newBucket(s.bucket.Cursor(), h, s.config))
	}
	return res
}

type bucket struct {
	cursor *bbolt.Cursor
	hash   string
	config chunk.PeriodConfig
}

func newBucket(cursor *bbolt.Cursor, hash string, config chunk.PeriodConfig) *bucket {
	return &bucket{
		cursor: cursor,
		hash:   hash,
		config: config,
	}
}

func (b *bucket) ContainsChunkFor(seriesID []byte) bool {
	if key, _ := b.cursor.Seek([]byte(b.hash + ":" + string(seriesID))); key != nil {
		return true
	}
	return false
}

func (b *bucket) LabelEntries(seriesID []byte) LabelEntryIterator {
	return newLabelEntryIterator(b, b.keyPrefix(seriesID))
}

func (b *bucket) keyPrefix(series []byte) (prefix []byte) {
	switch b.config.Schema {
	case "v11", "v10":
		shard := binary.BigEndian.Uint32(series) % b.config.RowShards
		prefix = unsafeGetBytes(fmt.Sprintf("%02d:%s:%s", shard, b.hash, logMetricName))
	default:
		prefix = unsafeGetBytes(fmt.Sprintf("%s:%s", b.hash, logMetricName))
	}
	return
}

type labelEntryIterator struct {
	*bucket

	current *LabelIndexRef
	first   bool
	err     error
	prefix  []byte
}

func newLabelEntryIterator(b *bucket, prefix []byte) *labelEntryIterator {
	return &labelEntryIterator{
		bucket: b,
		first:  true,
		prefix: prefix,
	}
}

func (it *labelEntryIterator) Err() error {
	return it.err
}

func (it *labelEntryIterator) Entry() *LabelIndexRef {
	return it.current
}

func (it *labelEntryIterator) Delete() error {
	return it.cursor.Delete()
}

func (it *labelEntryIterator) Next() bool {
	var key []byte
	if it.first {
		key, _ = it.cursor.Seek(it.prefix)
		it.first = false
	} else {
		key, _ = it.cursor.Next()
	}
	for key != nil && bytes.HasPrefix(key, it.prefix) {
		ref, ok, err := parseLabelIndexRef(decodeKey(key))
		if err != nil {
			it.err = err
			return false
		}
		// skips anything else than labels index entries.
		if !ok {
			key, _ = it.cursor.Next()
			continue
		}
		it.current = ref
		return true
	}
	return false
}
