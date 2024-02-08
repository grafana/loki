package bloomcompactor

import (
	"context"
	"math"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// TSDBStore is an interface for interacting with the TSDB,
// modeled off a relevant subset of the `tsdb.TSDBIndex` struct
type forSeries interface {
	ForSeries(
		ctx context.Context,
		fpFilter index.FingerprintFilter,
		from model.Time,
		through model.Time,
		fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta),
		matchers ...*labels.Matcher,
	) error
	Close() error
}

type TSDBSeriesIter struct {
	f      forSeries
	bounds v1.FingerprintBounds
	ctx    context.Context

	ch          chan *v1.Series
	initialized bool
	next        *v1.Series
	err         error
}

func NewTSDBSeriesIter(ctx context.Context, f forSeries, bounds v1.FingerprintBounds) *TSDBSeriesIter {
	return &TSDBSeriesIter{
		f:      f,
		bounds: bounds,
		ctx:    ctx,
		ch:     make(chan *v1.Series),
	}
}

func (t *TSDBSeriesIter) Next() bool {
	if !t.initialized {
		t.initialized = true
		t.background()
	}

	select {
	case <-t.ctx.Done():
		return false
	case next, ok := <-t.ch:
		t.next = next
		return ok
	}
}

func (t *TSDBSeriesIter) At() *v1.Series {
	return t.next
}

func (t *TSDBSeriesIter) Err() error {
	if t.err != nil {
		return t.err
	}

	return t.ctx.Err()
}

func (t *TSDBSeriesIter) Close() error {
	return t.f.Close()
}

// background iterates over the tsdb file, populating the next
// value via a channel to handle backpressure
func (t *TSDBSeriesIter) background() {
	go func() {
		t.err = t.f.ForSeries(
			t.ctx,
			t.bounds,
			0, math.MaxInt64,
			func(_ labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {

				res := &v1.Series{
					Fingerprint: fp,
					Chunks:      make(v1.ChunkRefs, 0, len(chks)),
				}
				for _, chk := range chks {
					res.Chunks = append(res.Chunks, v1.ChunkRef{
						Start:    model.Time(chk.MinTime),
						End:      model.Time(chk.MaxTime),
						Checksum: chk.Checksum,
					})
				}

				select {
				case <-t.ctx.Done():
					return
				case t.ch <- res:
				}
			},
			labels.MustNewMatcher(labels.MatchEqual, "", ""),
		)
		close(t.ch)
	}()
}
