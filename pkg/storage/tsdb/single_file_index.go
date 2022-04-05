package tsdb

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/tsdb/index"
)

// Identifier has all the information needed to resolve a TSDB index
// Notably this abstracts away OS path separators, etc.
type Identifier struct {
	Tenant        string
	From, Through model.Time
	Checksum      uint32
}

func (i Identifier) String() string {
	return filepath.Join(
		i.Tenant,
		fmt.Sprintf(
			"%s-%d-%d-%x.tsdb",
			index.IndexFilename,
			i.From,
			i.Through,
			i.Checksum,
		),
	)
}

// nolint
type TSDBIndex struct {
	reader IndexReader
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

// fn must NOT capture it's arguments. They're reused across series iterations and returned to
// a pool after completion.
func (i *TSDBIndex) forSeries(
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

	for p.Next() {
		hash, err := i.reader.Series(p.At(), &ls, &chks)
		if err != nil {
			return err
		}

		// skip series that belong to different shards
		if shard != nil && !shard.Match(model.Fingerprint(hash)) {
			continue
		}

		fn(ls, model.Fingerprint(hash), chks)
	}
	return p.Err()
}

func (i *TSDBIndex) GetChunkRefs(_ context.Context, userID string, from, through model.Time, res []ChunkRef, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	queryBounds := newBounds(from, through)
	if res == nil {
		res = ChunkRefsPool.Get()
	}
	res = res[:0]

	if err := i.forSeries(shard,
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

func (i *TSDBIndex) Series(_ context.Context, _ string, from, through model.Time, res []Series, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]Series, error) {
	queryBounds := newBounds(from, through)
	if res == nil {
		res = SeriesPool.Get()
	}
	res = res[:0]

	if err := i.forSeries(shard,
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

func (i *TSDBIndex) Identifier(tenant string) Identifier {
	lower, upper := i.Bounds()
	return Identifier{
		Tenant:   tenant,
		From:     lower,
		Through:  upper,
		Checksum: i.Checksum(),
	}
}
