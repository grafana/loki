package tsdb

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

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

func (i *TSDBIndex) GetChunkRefs(_ context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]ChunkRef, error) {

	p, err := PostingsForMatchers(i.reader, matchers...)
	if err != nil {
		return nil, err
	}

	var (
		res  []ChunkRef // TODO(owen-d): pool, reduce allocs
		ls   labels.Labels
		chks []index.ChunkMeta
	)

	for p.Next() {
		if err := i.reader.Series(p.At(), &ls, &chks); err != nil {
			return nil, err
		}

		// cache hash calculation across chunks of the same series
		// TODO(owen-d): Should we store this in the index? in an in-mem cache?
		var hash uint64

		// TODO(owen-d): use logarithmic approach
		for _, chk := range chks {

			// current chunk is outside the range of this request
			if chk.From() > through || chk.Through() < from {
				continue
			}

			if hash == 0 {
				hash = ls.Hash()
			}

			res = append(res, ChunkRef{
				User:        userID, // assumed to be the same, will be enforced by caller.
				Fingerprint: model.Fingerprint(hash),
				Start:       chk.From(),
				End:         chk.Through(),
				Checksum:    chk.Checksum,
			})
		}
	}
	return res, p.Err()
}

func (i *TSDBIndex) Series(_ context.Context, _ string, from, through model.Time, matchers ...*labels.Matcher) ([]Series, error) {
	p, err := PostingsForMatchers(i.reader, matchers...)
	if err != nil {
		return nil, err
	}

	var (
		res  []Series // TODO(owen-d): reduce allocs
		ls   labels.Labels
		chks []index.ChunkMeta
	)

	for p.Next() {
		if err := i.reader.Series(p.At(), &ls, &chks); err != nil {
			return nil, err
		}

		// TODO(owen-d): use logarithmic approach
		for _, chk := range chks {

			if chk.From() < through && chk.Through() >= from {
				// this series has at least one chunk in the desired range
				res = append(res, Series{
					Labels: ls.Copy(),
				})
				break
			}
		}
	}
	return res, p.Err()
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
