package xcap_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/xcap"
	"github.com/grafana/loki/v3/pkg/xcap/xcaptest"
)

func TestRunIsolated(t *testing.T) {
	requests := xcap.NewStatisticInt64("runisolated.requests", xcap.AggregationTypeSum)
	bytes := xcap.NewStatisticInt64("runisolated.bytes", xcap.AggregationTypeSum)

	t.Run("returns the result and the error of fn", func(t *testing.T) {
		ctx, _ := xcaptest.NewRegion(t)
		wantErr := errors.New("read failed")

		got, err := xcap.RunIsolated(ctx, "isolated", func(context.Context) (string, error) { return "result", wantErr }, nil)

		require.Equal(t, "result", got)
		require.ErrorIs(t, err, wantErr)
	})

	t.Run("keeps the statistics of fn out of the region of ctx until merge hands them over", func(t *testing.T) {
		ctx, recorded := xcaptest.NewRegion(t)
		var recordedBeforeMerge int64

		_, err := xcap.RunIsolated(ctx, "isolated",
			func(isolatedCtx context.Context) (struct{}, error) {
				xcap.RegionFromContext(isolatedCtx).Record(requests.Observe(2))
				return struct{}{}, nil
			},
			func(parent, isolated *xcap.Region, _ error) {
				recordedBeforeMerge = recorded(requests)
				parent.MergeObservations(isolated)
			})

		require.NoError(t, err)
		require.Zero(t, recordedBeforeMerge)
		require.Equal(t, int64(2), recorded(requests))
	})

	t.Run("keeps the statistics that concurrent work records in the region of ctx out of the isolated region", func(t *testing.T) {
		ctx, recorded := xcaptest.NewRegion(t)
		xcap.RegionFromContext(ctx).Record(requests.Observe(5))
		var isolatedRequests int64

		_, err := xcap.RunIsolated(ctx, "isolated",
			func(isolatedCtx context.Context) (struct{}, error) {
				xcap.RegionFromContext(ctx).Record(requests.Observe(3))
				xcap.RegionFromContext(isolatedCtx).Record(requests.Observe(2))
				return struct{}{}, nil
			},
			func(parent, isolated *xcap.Region, _ error) {
				isolatedRequests = isolated.Int64Value(requests)
				parent.MergeObservations(isolated)
			})

		require.NoError(t, err)
		require.Equal(t, int64(2), isolatedRequests)
		require.Equal(t, int64(5+3+2), recorded(requests))
	})

	t.Run("lets merge hand over only some statistics, and derive others", func(t *testing.T) {
		ctx, recorded := xcaptest.NewRegion(t)

		_, err := xcap.RunIsolated(ctx, "isolated",
			func(isolatedCtx context.Context) (struct{}, error) {
				xcap.RegionFromContext(isolatedCtx).Record(requests.Observe(2))
				xcap.RegionFromContext(isolatedCtx).Record(bytes.Observe(7))
				return struct{}{}, nil
			},
			func(parent, isolated *xcap.Region, _ error) {
				parent.Record(bytes.Observe(isolated.Int64Value(requests) * 100))
			})

		require.NoError(t, err)
		require.Zero(t, recorded(requests))
		require.Equal(t, int64(200), recorded(bytes))
	})

	t.Run("passes the error of fn to merge", func(t *testing.T) {
		ctx, _ := xcaptest.NewRegion(t)
		wantErr := errors.New("read failed")
		var mergeErr error

		_, err := xcap.RunIsolated(ctx, "isolated",
			func(context.Context) (struct{}, error) { return struct{}{}, wantErr },
			func(_, _ *xcap.Region, err error) { mergeErr = err })

		require.ErrorIs(t, err, wantErr)
		require.ErrorIs(t, mergeErr, wantErr)
	})

	t.Run("hands every statistic of the isolated region to the region of ctx when merge is nil", func(t *testing.T) {
		ctx, recorded := xcaptest.NewRegion(t)

		_, err := xcap.RunIsolated(ctx, "isolated", func(isolatedCtx context.Context) (struct{}, error) {
			xcap.RegionFromContext(isolatedCtx).Record(requests.Observe(1))
			xcap.RegionFromContext(isolatedCtx).Record(bytes.Observe(7))
			return struct{}{}, nil
		}, nil)

		require.NoError(t, err)
		require.Equal(t, int64(1), recorded(requests))
		require.Equal(t, int64(7), recorded(bytes))
	})

	t.Run("calls fn with ctx and does not call merge when ctx holds no region", func(t *testing.T) {
		ctx := t.Context()
		var fnCtx context.Context
		merged := false

		got, err := xcap.RunIsolated(ctx, "isolated",
			func(ctx context.Context) (string, error) {
				fnCtx = ctx
				return "result", nil
			},
			func(_, _ *xcap.Region, _ error) { merged = true })

		require.NoError(t, err)
		require.Equal(t, "result", got)
		require.Equal(t, ctx, fnCtx)
		require.False(t, merged)
	})
}

func TestMergeDerived(t *testing.T) {
	var (
		requests     = xcap.NewStatisticInt64("mergederived.requests", xcap.AggregationTypeSum)
		bytes        = xcap.NewStatisticInt64("mergederived.bytes", xcap.AggregationTypeSum)
		readRequests = xcap.NewStatisticInt64("mergederived.read.requests", xcap.AggregationTypeSum)
		readBytes    = xcap.NewStatisticInt64("mergederived.read.bytes", xcap.AggregationTypeSum)
		unrecorded   = xcap.NewStatisticInt64("mergederived.unrecorded", xcap.AggregationTypeSum)
		derived      = xcap.NewStatisticInt64("mergederived.derived", xcap.AggregationTypeSum)
	)

	// run runs an operation in an isolated region. It reads 3 requests and 40 bytes.
	run := func(ctx context.Context, merge func(parent, isolated *xcap.Region, err error)) {
		_, err := xcap.RunIsolated(ctx, "isolated", func(isolatedCtx context.Context) (struct{}, error) {
			region := xcap.RegionFromContext(isolatedCtx)
			region.Record(readRequests.Observe(3))
			region.Record(readBytes.Observe(40))
			return struct{}{}, nil
		}, merge)
		require.NoError(t, err)
	}

	t.Run("records the value of each source as its target, and hands over every statistic", func(t *testing.T) {
		ctx, recorded := xcaptest.NewRegion(t)

		run(ctx, xcap.MergeDerived(xcap.Derive(requests, readRequests), xcap.Derive(bytes, readBytes)))

		require.Equal(t, int64(3), recorded(requests))
		require.Equal(t, int64(40), recorded(bytes))
		require.Equal(t, int64(3), recorded(readRequests), "the sources reach the region of ctx too")
		require.Equal(t, int64(40), recorded(readBytes))
	})

	t.Run("records zero for a source that the isolated region did not record", func(t *testing.T) {
		ctx, recorded := xcaptest.NewRegion(t)

		run(ctx, xcap.MergeDerived(xcap.Derive(derived, unrecorded)))

		require.Zero(t, recorded(derived))
	})

	t.Run("hands over every statistic when it has no derivation", func(t *testing.T) {
		ctx, recorded := xcaptest.NewRegion(t)

		run(ctx, xcap.MergeDerived())

		require.Equal(t, int64(3), recorded(readRequests))
		require.Equal(t, int64(40), recorded(readBytes))
	})

	t.Run("derives only from the isolated region, and not from concurrent work in the region of ctx", func(t *testing.T) {
		ctx, recorded := xcaptest.NewRegion(t)
		xcap.RegionFromContext(ctx).Record(readRequests.Observe(100))

		run(ctx, xcap.MergeDerived(xcap.Derive(requests, readRequests)))

		require.Equal(t, int64(3), recorded(requests), "the derived value counts only the isolated operation")
		require.Equal(t, int64(103), recorded(readRequests))
	})
}
