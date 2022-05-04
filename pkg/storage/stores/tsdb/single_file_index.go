package tsdb

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"strings"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/storage/chunk"
	index_shipper "github.com/grafana/loki/pkg/storage/stores/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

const (
	gzipSuffix = ".gz"
)

func OpenShippableTSDB(p string) (index_shipper.Index, error) {
	var gz bool
	trimmed := strings.TrimSuffix(p, gzipSuffix)
	if trimmed != p {
		gz = true
	}

	id, err := identifierFromPath(trimmed)
	if err != nil {
		return nil, err
	}

	return NewShippableTSDBFile(id, gz)
}

// nolint
// TSDBFile is backed by an actual file and implements the indexshipper/index.Index interface
type TSDBFile struct {
	// reuse Identifier for resolving locations
	Identifier

	// reuse TSDBIndex for reading
	Index

	// to sastisfy Reader() and Close() methods
	r io.ReadSeeker
}

func NewShippableTSDBFile(id Identifier, gzip bool) (*TSDBFile, error) {
	if gzip {
		id = newSuffixedIdentifier(id, gzipSuffix)
	}

	idx, b, err := NewTSDBIndexFromFile(id.Path(), gzip)
	if err != nil {
		return nil, err
	}

	return &TSDBFile{
		Identifier: id,
		Index:      idx,
		r:          bytes.NewReader(b),
	}, err
}

func (f *TSDBFile) Close() error {
	return f.Index.Close()
}

func (f *TSDBFile) Reader() (io.ReadSeeker, error) {
	return f.r, nil
}

// nolint
// TSDBIndex is backed by an IndexReader
// and translates the IndexReader to an Index implementation
// It loads the file into memory and doesn't keep a file descriptor open
type TSDBIndex struct {
	reader      IndexReader
	chunkFilter chunk.RequestChunkFilterer
}

// Return the index as well as the underlying []byte which isn't exposed as an index
// method but is helpful for building an io.reader for the index shipper
func NewTSDBIndexFromFile(location string, gzip bool) (*TSDBIndex, []byte, error) {
	raw, err := ioutil.ReadFile(location)
	if err != nil {
		return nil, nil, err
	}

	cleaned := raw

	// decompress if needed
	if gzip {
		r := chunkenc.Gzip.GetReader(bytes.NewReader(raw))
		defer chunkenc.Gzip.PutReader(r)

		var err error
		cleaned, err = io.ReadAll(r)
		if err != nil {
			return nil, nil, err
		}
	}

	reader, err := index.NewReader(index.RealByteSlice(cleaned))
	if err != nil {
		return nil, nil, err
	}
	return NewTSDBIndex(reader), cleaned, nil
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
func (i *TSDBIndex) forSeries(
	ctx context.Context,
	shard *index.ShardAnnotation,
	fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta),
	matchers ...*labels.Matcher,
) error {
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
		hash, err := i.reader.Series(p.At(), &ls, &chks)
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
	queryBounds := newBounds(from, through)
	if res == nil {
		res = ChunkRefsPool.Get()
	}
	res = res[:0]

	if err := i.forSeries(ctx, shard,
		func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {
			// TODO(owen-d): use logarithmic approach
			for _, chk := range chks {

				// current chunk is outside the range of this request
				if !Overlap(queryBounds, chk) {
					continue
				}

				res = append(res, ChunkRef{
					User:        userID, // assumed to be the same, will be enforced by caller.
					Fingerprint: fp,
					Start:       chk.From(),
					End:         chk.Through(),
					Checksum:    chk.Checksum,
				})
			}
		},
		matchers...); err != nil {
		return nil, err
	}

	return res, nil
}

func (i *TSDBIndex) Series(ctx context.Context, _ string, from, through model.Time, res []Series, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]Series, error) {
	queryBounds := newBounds(from, through)
	if res == nil {
		res = SeriesPool.Get()
	}
	res = res[:0]

	if err := i.forSeries(ctx, shard,
		func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {
			// TODO(owen-d): use logarithmic approach
			for _, chk := range chks {
				if Overlap(queryBounds, chk) {
					// this series has at least one chunk in the desired range
					res = append(res, Series{
						Labels:      ls.Copy(),
						Fingerprint: fp,
					})
					break
				}
			}
		},
		matchers...); err != nil {
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

func (i *TSDBIndex) Identifier(tenant string) SingleTenantTSDBIdentifier {
	lower, upper := i.Bounds()
	return SingleTenantTSDBIdentifier{
		Tenant:   tenant,
		From:     lower,
		Through:  upper,
		Checksum: i.Checksum(),
	}
}
