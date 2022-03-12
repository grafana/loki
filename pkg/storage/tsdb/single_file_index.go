package tsdb

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/storage/tsdb/index"
)

// nolint
type TSDBIndex struct {
	reader IndexReader
}

func NewTSDBIndex(reader IndexReader) *TSDBIndex {
	return &TSDBIndex{
		reader: reader,
	}
}

func (i *TSDBIndex) Bounds() (model.Time, model.Time) {
	from, through := i.reader.Bounds()
	return model.Time(from), model.Time(through)
}

func (i *TSDBIndex) forSeries(
	shard *astmapper.ShardAnnotation,
	fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta),
	matchers ...*labels.Matcher,
) error {
	p, err := PostingsForMatchers(i.reader, matchers...)
	if err != nil {
		return err
	}

	var (
		ls   labels.Labels
		chks []index.ChunkMeta
	)

	for p.Next() {
		if err := i.reader.Series(p.At(), &ls, &chks); err != nil {
			return err
		}

		hash := ls.Hash()
		// skip series that belong to different shards
		if shard != nil && !shard.Match(model.Fingerprint(hash)) {
			continue
		}

		fn(ls, model.Fingerprint(hash), chks)
	}
	return p.Err()
}

func (i *TSDBIndex) GetChunkRefs(_ context.Context, userID string, from, through model.Time, shard *astmapper.ShardAnnotation, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	queryBounds := newBounds(from, through)
	var res []ChunkRef // TODO(owen-d): pool, reduce allocs

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

func (i *TSDBIndex) Series(_ context.Context, _ string, from, through model.Time, shard *astmapper.ShardAnnotation, matchers ...*labels.Matcher) ([]Series, error) {
	queryBounds := newBounds(from, through)
	var res []Series // TODO(owen-d): pool, reduce allocs

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
