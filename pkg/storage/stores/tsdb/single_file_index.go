package tsdb

import (
	"context"
	"io"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/chunk"
	index_shipper "github.com/grafana/loki/pkg/storage/stores/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/grafana/loki/pkg/util/math"
)

// GetRawFileReaderFunc returns an io.ReadSeeker for reading raw tsdb file from disk
type GetRawFileReaderFunc func() (io.ReadSeeker, error)

func OpenShippableTSDB(p string) (index_shipper.Index, error) {
	id, err := identifierFromPath(p)
	if err != nil {
		return nil, err
	}

	return NewShippableTSDBFile(id)
}

// nolint
// TSDBFile is backed by an actual file and implements the indexshipper/index.Index interface
type TSDBFile struct {
	// reuse Identifier for resolving locations
	Identifier

	// reuse TSDBIndex for reading
	Index

	// to sastisfy Reader() and Close() methods
	getRawFileReader GetRawFileReaderFunc
}

func NewShippableTSDBFile(id Identifier) (*TSDBFile, error) {
	idx, getRawFileReader, err := NewTSDBIndexFromFile(id.Path())
	if err != nil {
		return nil, err
	}

	return &TSDBFile{
		Identifier:       id,
		Index:            idx,
		getRawFileReader: getRawFileReader,
	}, err
}

func (f *TSDBFile) Close() error {
	return f.Index.Close()
}

func (f *TSDBFile) Reader() (io.ReadSeeker, error) {
	return f.getRawFileReader()
}

// nolint
// TSDBIndex is backed by an IndexReader
// and translates the IndexReader to an Index implementation
// It loads the file into memory and doesn't keep a file descriptor open
type TSDBIndex struct {
	reader      IndexReader
	chunkFilter chunk.RequestChunkFilterer
}

// Return the index as well as the underlying raw file reader which isn't exposed as an index
// method but is helpful for building an io.reader for the index shipper
func NewTSDBIndexFromFile(location string) (*TSDBIndex, GetRawFileReaderFunc, error) {
	reader, err := index.NewFileReader(location)
	if err != nil {
		return nil, nil, err
	}

	return NewTSDBIndex(reader), func() (io.ReadSeeker, error) {
		return reader.RawFileReader()
	}, nil
}

func NewTSDBIndex(reader IndexReader) *TSDBIndex {
	return &TSDBIndex{
		reader: reader,
	}
}

func (i *TSDBIndex) Close() error {
	return i.reader.Close()
}

func (i *TSDBIndex) Bounds() (model.Time, model.Time) {
	from, through := i.reader.Bounds()
	return model.Time(from), model.Time(through)
}

func (i *TSDBIndex) SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer) {
	i.chunkFilter = chunkFilter
}

// fn must NOT capture it's arguments. They're reused across series iterations and returned to
// a pool after completion.
func (i *TSDBIndex) forSeries(ctx context.Context, shard *index.ShardAnnotation, from model.Time, through model.Time, fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta), matchers ...*labels.Matcher) error {
	p, err := PostingsForMatchers(i.reader, shard, matchers...)
	if err != nil {
		return err
	}

	var ls labels.Labels
	chks := ChunkMetasPool.Get()
	defer ChunkMetasPool.Put(chks)

	var filterer chunk.Filterer
	if i.chunkFilter != nil {
		filterer = i.chunkFilter.ForRequest(ctx)
	}

	for p.Next() {
		hash, err := i.reader.Series(p.At(), int64(from), int64(through), &ls, &chks)
		if err != nil {
			return err
		}

		// skip series that belong to different shards
		if shard != nil && !shard.Match(model.Fingerprint(hash)) {
			continue
		}

		if filterer != nil && filterer.ShouldFilter(ls) {
			continue
		}

		fn(ls, model.Fingerprint(hash), chks)
	}
	return p.Err()
}

func (i *TSDBIndex) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []ChunkRef, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	if res == nil {
		res = ChunkRefsPool.Get()
	}
	res = res[:0]

	if err := i.forSeries(ctx, shard, from, through, func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {
		for _, chk := range chks {

			res = append(res, ChunkRef{
				User:        userID, // assumed to be the same, will be enforced by caller.
				Fingerprint: fp,
				Start:       chk.From(),
				End:         chk.Through(),
				Checksum:    chk.Checksum,
			})
		}
	}, matchers...); err != nil {
		return nil, err
	}

	return res, nil
}

func (i *TSDBIndex) Series(ctx context.Context, _ string, from, through model.Time, res []Series, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]Series, error) {
	if res == nil {
		res = SeriesPool.Get()
	}
	res = res[:0]

	if err := i.forSeries(ctx, shard, from, through, func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {
		if len(chks) == 0 {
			return
		}
		res = append(res, Series{
			Labels:      ls.Copy(),
			Fingerprint: fp,
		})
	}, matchers...); err != nil {
		return nil, err
	}

	return res, nil
}

func (i *TSDBIndex) LabelNames(_ context.Context, _ string, _, _ model.Time, matchers ...*labels.Matcher) ([]string, error) {
	if len(matchers) == 0 {
		return i.reader.LabelNames()
	}

	return labelNamesWithMatchers(i.reader, matchers...)
}

func (i *TSDBIndex) LabelValues(_ context.Context, _ string, _, _ model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	if len(matchers) == 0 {
		return i.reader.LabelValues(name)
	}
	return labelValuesWithMatchers(i.reader, name, matchers...)
}

func (i *TSDBIndex) Checksum() uint32 {
	return i.reader.Checksum()
}

func (i *TSDBIndex) Identifier(string) SingleTenantTSDBIdentifier {
	lower, upper := i.Bounds()
	return SingleTenantTSDBIdentifier{
		TS:       time.Now(),
		From:     lower,
		Through:  upper,
		Checksum: i.Checksum(),
	}
}

func (i *TSDBIndex) Stats(ctx context.Context, userID string, from, through model.Time, acc IndexStatsAccumulator, shard *index.ShardAnnotation, shouldIncludeChunk shouldIncludeChunk, matchers ...*labels.Matcher) error {
	if err := i.forSeries(ctx, shard, from, through, func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {
		var addedStream bool
		for _, chk := range chks {
			if shouldIncludeChunk != nil && !shouldIncludeChunk(chk) {
				continue
			}

			if !addedStream {
				acc.AddStream(fp)
				addedStream = true
			}

			// Assuming entries and bytes are evenly distributed in the chunk,
			// We will take the proportional number of entries and number of bytes
			// if (chk.MinTime < from) and/or (chk.MaxTime > through).
			//
			//       MinTime  From              Through  MaxTime
			//       ┌────────┬─────────────────┬────────┐
			//       │        *      Chunk      *        │
			//       └────────┴─────────────────┴────────┘
			//       ▲   A    |        C        |   B    ▲
			//       └───────────────────────────────────┘
			//               T = MinTime - MaxTime
			//
			// We want to get the percentage of time that fits into C
			// to use it as a factor to get the amount of bytes and entries
			// factor = C = (T - (A + B)) / T = (chunkTime - (leadingTime + trailingTime)) / chunkTime
			chunkTime := chk.MaxTime - chk.MinTime
			leadingTime := math.Max64(0, int64(from)-chk.MinTime)
			trailingTime := math.Max64(0, chk.MaxTime-int64(through))
			factor := float32(chunkTime-(leadingTime+trailingTime)) / float32(chunkTime)

			adjustedChunkMeta := index.ChunkMeta{
				Checksum: chk.Checksum,
				MinTime:  chk.MinTime + leadingTime,
				MaxTime:  chk.MinTime + trailingTime,
				KB:       uint32(float32(chk.KB) * factor),
				Entries:  uint32(float32(chk.Entries) * factor),
			}

			acc.AddChunk(fp, adjustedChunkMeta)
		}
	}, matchers...); err != nil {
		return err
	}

	return nil
}
