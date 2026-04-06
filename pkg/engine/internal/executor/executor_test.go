package executor

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestExecutor(t *testing.T) {
	t.Run("pipeline fails if plan is nil", func(t *testing.T) {
		ctx := t.Context()
		pipeline := Run(ctx, Config{}, nil, log.NewNopLogger())
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "failed to execute pipeline: plan is nil")
	})

	t.Run("pipeline fails if plan has no root node", func(t *testing.T) {
		ctx := t.Context()
		pipeline := Run(ctx, Config{}, &physical.Plan{}, log.NewNopLogger())
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "failed to execute pipeline: plan has no root node")
	})
}

func TestExecutor_Limit(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeLimit(ctx, &physical.Limit{}, nil)
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, EOF.Error())
	})

	t.Run("multiple inputs result in error", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeLimit(ctx, &physical.Limit{}, []Pipeline{emptyPipeline(), emptyPipeline()})
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "limit expects exactly one input, got 2")
	})
}

func TestExecutor_Filter(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeFilter(ctx, &physical.Filter{}, nil)
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, EOF.Error())
	})

	t.Run("multiple inputs result in error", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeFilter(ctx, &physical.Filter{}, []Pipeline{emptyPipeline(), emptyPipeline()})
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "filter expects exactly one input, got 2")
	})
}

func TestExecutor_Projection(t *testing.T) {
	t.Run("no inputs result in empty pipeline", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeProjection(ctx, &physical.Projection{}, nil)
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, EOF.Error())
	})

	t.Run("missing column expression results in error", func(t *testing.T) {
		ctx := t.Context()
		cols := []physical.Expression{}
		c := &Context{}
		pipeline := c.executeProjection(ctx, &physical.Projection{Expressions: cols}, []Pipeline{emptyPipeline()})
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "projection expects at least one expression, got 0")
	})

	t.Run("multiple inputs result in error", func(t *testing.T) {
		ctx := t.Context()
		c := &Context{}
		pipeline := c.executeProjection(ctx, &physical.Projection{}, []Pipeline{emptyPipeline(), emptyPipeline()})
		_, err := pipeline.Read(ctx)
		require.ErrorContains(t, err, "projection expects exactly one input, got 2")
	})
}

func Test_filterStreamsByLabels(t *testing.T) {
	// Build data object with two streams: one allowed, one filtered
	obj := buildDataobj(t, []logproto.Stream{
		{
			Labels: `{service="loki", env="prod"}`,
			Entries: []logproto.Entry{
				{Timestamp: time.Unix(5, 0), Line: "allowed"},
			},
		},
		{
			Labels: `{service="blocked", env="staging"}`,
			Entries: []logproto.Entry{
				{Timestamp: time.Unix(2, 0), Line: "blocked"},
			},
		},
	})

	// Open streams section
	var streamsSection *streams.Section
	for _, sec := range obj.Sections() {
		if streams.CheckSection(sec) {
			var err error
			streamsSection, err = streams.Open(t.Context(), sec)
			require.NoError(t, err)
			break
		}
	}

	tests := []struct {
		name          string
		filterFunc    func(labels.Labels) bool
		inputIDs      []int64
		expectedCount int
	}{
		{
			name: "filter blocks staging env",
			filterFunc: func(lbls labels.Labels) bool {
				return lbls.Get("env") == "staging"
			},
			inputIDs:      []int64{1, 2},
			expectedCount: 1,
		},
		{
			name: "no filter allows all",
			filterFunc: func(_ labels.Labels) bool {
				return false // never filter
			},
			inputIDs:      []int64{1, 2},
			expectedCount: 2,
		},
		{
			name: "filter blocks all",
			filterFunc: func(_ labels.Labels) bool {
				return true // always filter
			},
			inputIDs:      []int64{1, 2},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &Context{
				batchSize: 512,
				logger:    log.NewNopLogger(),
			}

			filterer := &mockStreamFilterer{shouldFilter: tt.filterFunc}
			filtered := ctx.filterStreamsByLabels(t.Context(), tt.inputIDs, streamsSection, filterer)

			require.Len(t, filtered, tt.expectedCount)
		})
	}
}

// mockStreamFilterer for testing
type mockStreamFilterer struct {
	shouldFilter func(labels.Labels) bool
}

func (m *mockStreamFilterer) ShouldFilter(lbls labels.Labels) bool {
	if m.shouldFilter != nil {
		return m.shouldFilter(lbls)
	}
	return false
}
