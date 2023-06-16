package tsdb

import (
	"context"
	"errors"
	"io"
	"math"
	"path/filepath"
	"time"

	"github.com/opentracing/opentracing-go"

	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/chunk"
	index_shipper "github.com/grafana/loki/pkg/storage/stores/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	util_log "github.com/grafana/loki/pkg/util/log"
)

var ErrAlreadyOnDesiredVersion = errors.New("tsdb file already on desired version")

// GetRawFileReaderFunc returns an io.ReadSeeker for reading raw tsdb file from disk
type GetRawFileReaderFunc func() (io.ReadSeeker, error)

func OpenShippableTSDB(p string) (index_shipper.Index, error) {
	id, err := identifierFromPath(p)
	if err != nil {
		return nil, err
	}

	return NewShippableTSDBFile(id)
}

func RebuildWithVersion(ctx context.Context, path string, desiredVer int) (index_shipper.Index, error) {
	indexFile, err := OpenShippableTSDB(path)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err := indexFile.Close(); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to close index file", "err", err)
		}
	}()

	currVer := indexFile.(*TSDBFile).Index.(*TSDBIndex).reader.(*index.Reader).Version()
	if currVer == desiredVer {
		return nil, ErrAlreadyOnDesiredVersion
	}

	builder := NewBuilder()
	err = indexFile.(*TSDBFile).Index.(*TSDBIndex).ForSeries(ctx, nil, 0, math.MaxInt64, func(lbls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {
		builder.AddSeries(lbls.Copy(), fp, chks)
	}, labels.MustNewMatcher(labels.MatchEqual, "", ""))
	if err != nil {
		return nil, err
	}

	parentDir := filepath.Dir(path)

	id, err := builder.BuildWithVersion(ctx, desiredVer, parentDir, func(from, through model.Time, checksum uint32) Identifier {
		id := SingleTenantTSDBIdentifier{
			TS:       time.Now(),
			From:     from,
			Through:  through,
			Checksum: checksum,
		}
		return NewPrefixedIdentifier(id, parentDir, "")
	})

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
func (i *TSDBIndex) ForSeries(ctx context.Context, shard *index.ShardAnnotation, from model.Time, through model.Time, fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta), matchers ...*labels.Matcher) error {
	// TODO(owen-d): use pool

	var ls labels.Labels
	chks := ChunkMetasPool.Get()
	defer ChunkMetasPool.Put(chks)

	var filterer chunk.Filterer
	if i.chunkFilter != nil {
		filterer = i.chunkFilter.ForRequest(ctx)
	}

	return i.forPostings(ctx, shard, from, through, matchers, func(p index.Postings) error {
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
	})

}

func (i *TSDBIndex) forPostings(
	_ context.Context,
	shard *index.ShardAnnotation,
	_, _ model.Time,
	matchers []*labels.Matcher,
	fn func(index.Postings) error,
) error {
	p, err := PostingsForMatchers(i.reader, shard, matchers...)
	if err != nil {
		return err
	}
	return fn(p)
}

func (i *TSDBIndex) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []ChunkRef, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	if res == nil {
		res = ChunkRefsPool.Get()
	}
	res = res[:0]

	if err := i.ForSeries(ctx, shard, from, through, func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {
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

	if err := i.ForSeries(ctx, shard, from, through, func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {
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

func (i *TSDBIndex) Stats(ctx context.Context, _ string, from, through model.Time, acc IndexStatsAccumulator, shard *index.ShardAnnotation, _ shouldIncludeChunk, matchers ...*labels.Matcher) error {
	return i.forPostings(ctx, shard, from, through, matchers, func(p index.Postings) error {
		// TODO(owen-d): use pool
		var ls labels.Labels
		var filterer chunk.Filterer
		if i.chunkFilter != nil {
			filterer = i.chunkFilter.ForRequest(ctx)
		}

		for p.Next() {
			fp, stats, err := i.reader.ChunkStats(p.At(), int64(from), int64(through), &ls)
			if err != nil {
				return err
			}

			// skip series that belong to different shards
			if shard != nil && !shard.Match(model.Fingerprint(fp)) {
				continue
			}

			if filterer != nil && filterer.ShouldFilter(ls) {
				continue
			}

			if stats.Entries > 0 {
				// need to add stream
				acc.AddStream(model.Fingerprint(fp))
				acc.AddChunkStats(stats)
			}
		}
		return p.Err()
	})
}

// SeriesVolume returns the volumes of the series described by the passed
// matchers by the names of the passed matchers. All non-requested labels are
// aggregated into the requested series.
//
// ex: Imagine we have two labels: 'foo' and 'fizz' each with two values 'a'
// and 'b'. Called with the matcher `{foo="a"}`, SeriesVolume returns the
// aggregated size of the series `{foo="a"}`. If SeriesVolume with
// `{foo=~".+", fizz=~".+"}, it returns the series volumes aggregated as follows:
//
// {foo="a", fizz="a"}
// {foo="a", fizz="b"}
// {foo="b", fizz="a"}
// {foo="b", fizz="b"}
func (i *TSDBIndex) SeriesVolume(ctx context.Context, _ string, from, through model.Time, acc SeriesVolumeAccumulator, shard *index.ShardAnnotation, _ shouldIncludeChunk, matchers ...*labels.Matcher) error {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "Index.SeriesVolume")
	defer sp.Finish()

	var matchAll bool
	labelsToMatch := make(map[string]struct{})
	for _, m := range matchers {
		if m.Name == "" {
			matchAll = true
			continue
		}

		if m.Name == TenantLabel {
			continue
		}

		labelsToMatch[m.Name] = struct{}{}
	}

	seriesNames := make(map[uint64]string)
	seriesLabels := labels.Labels(make([]labels.Label, 0, len(labelsToMatch)))

	volumes := make(map[string]uint64)
	err := i.forPostings(ctx, shard, from, through, matchers, func(p index.Postings) error {
		var ls labels.Labels
		var filterer chunk.Filterer
		if i.chunkFilter != nil {
			filterer = i.chunkFilter.ForRequest(ctx)
		}

		for p.Next() {
			fp, stats, err := i.reader.ChunkStats(p.At(), int64(from), int64(through), &ls)
			if err != nil {
				return err
			}

			// skip series that belong to different shards
			if shard != nil && !shard.Match(model.Fingerprint(fp)) {
				continue
			}

			if filterer != nil && filterer.ShouldFilter(ls) {
				continue
			}

			if stats.Entries > 0 {
				seriesLabels = seriesLabels[:0]
				for _, l := range ls {
					if _, ok := labelsToMatch[l.Name]; l.Name != TenantLabel && matchAll || ok {
						seriesLabels = append(seriesLabels, l)
					}
				}

				// If the labels are < 1k, this does not alloc
				// https://github.com/prometheus/prometheus/pull/8025
				hash := seriesLabels.Hash()
				if _, ok := seriesNames[hash]; !ok {
					seriesNames[hash] = seriesLabels.String()
				}

				volumes[seriesNames[hash]] += stats.KB << 10 // Return bytes
			}
		}
		return p.Err()
	})
	if err != nil {
		return err
	}
	acc.AddVolumes(volumes)
	return nil
}
