package tsdb

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
)

type delayedIdx time.Duration

func (t delayedIdx) Bounds() (model.Time, model.Time) { return 0, math.MaxInt64 }
func (t delayedIdx) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	<-time.After(time.Duration(t))
	return nil, nil
}
func (t delayedIdx) Series(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]Series, error) {
	<-time.After(time.Duration(t))
	return nil, nil
}
func (t delayedIdx) LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	<-time.After(time.Duration(t))
	return nil, nil
}
func (t delayedIdx) LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	<-time.After(time.Duration(t))
	return nil, nil
}

func TestParallelIndex(t *testing.T) {
	duration := 10 * time.Millisecond
	p := 5
	idx := NewParallelIndex(NewBoundedParallelism(p), delayedIdx(duration))

	for _, tc := range []struct {
		desc string
		fn   func(context.Context) error
	}{
		{
			desc: "GetChunkRefs",
			fn: func(ctx context.Context) error {
				_, err := idx.GetChunkRefs(ctx, "", 1, 2)
				return err
			},
		},
		{
			desc: "Series",
			fn: func(ctx context.Context) error {
				_, err := idx.Series(ctx, "", 1, 2)
				return err
			},
		},
		{
			desc: "LabelNames",
			fn: func(ctx context.Context) error {
				_, err := idx.LabelNames(ctx, "", 1, 2)
				return err
			},
		},
		{
			desc: "LabelValues",
			fn: func(ctx context.Context) error {
				_, err := idx.LabelValues(ctx, "", 1, 2, "foo")
				return err
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			var completions atomic.Int32

			// cancel after 150% of the delay duration in our index
			// this should ensure we only finish one round of parallelism
			ctx, _ := context.WithTimeout(context.Background(), duration*3/2)

			// try to run double the parallelism
			for i := 0; i < 2*p; i++ {

				go func() {
					if err := tc.fn(ctx); err == nil {
						completions.Inc()
					}
				}()
			}
			<-ctx.Done()
			// realistically, due to machine/runtime scheduling, it's not unlikely to see less
			// than one entire round complete.
			require.LessOrEqual(t, int(completions.Load()), p)
		})
	}
}
