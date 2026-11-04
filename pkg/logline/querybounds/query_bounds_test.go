package querybounds

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

func TestGetQueryBounds_MetricRangeAlignment(t *testing.T) {
	// 02:00:10 and 02:05:10 are not on a one-minute step. With alignment on, both
	// ends floor and the following ceil leaves the end at 02:05:00. With alignment
	// off, the splitter floors the start and ceils the end to 02:06:00.
	expr := mustExpr(t, `sum(count_over_time({job="api"} |= "timeout" [5m]))`)
	step := time.Minute.Milliseconds()
	offStart := time.Date(2026, 2, 26, 2, 0, 10, 0, time.UTC)
	offEnd := time.Date(2026, 2, 26, 2, 5, 10, 0, time.UTC)
	onStart := time.Date(2026, 2, 26, 2, 0, 0, 0, time.UTC)
	onEnd := time.Date(2026, 2, 26, 2, 5, 0, 0, time.UTC)

	tests := []struct {
		name                       string
		start, end                 time.Time
		step                       int64
		align                      bool
		wantReadStart, wantReadEnd time.Time
	}{
		{
			name:          "alignment off ceils an off-minute end",
			start:         offStart,
			end:           offEnd,
			step:          step,
			align:         false,
			wantReadStart: time.Date(2026, 2, 26, 1, 55, 0, 0, time.UTC),
			wantReadEnd:   time.Date(2026, 2, 26, 2, 6, 0, 0, time.UTC),
		},
		{
			name:          "alignment on floors an off-minute end",
			start:         offStart,
			end:           offEnd,
			step:          step,
			align:         true,
			wantReadStart: time.Date(2026, 2, 26, 1, 55, 0, 0, time.UTC),
			wantReadEnd:   time.Date(2026, 2, 26, 2, 5, 0, 0, time.UTC),
		},
		{
			name:          "alignment off ceils only the end when the start is on a step",
			start:         onStart,
			end:           offEnd,
			step:          step,
			align:         false,
			wantReadStart: time.Date(2026, 2, 26, 1, 55, 0, 0, time.UTC),
			wantReadEnd:   time.Date(2026, 2, 26, 2, 6, 0, 0, time.UTC),
		},
		{
			name:          "alignment on leaves an on-step start and floors the end",
			start:         onStart,
			end:           offEnd,
			step:          step,
			align:         true,
			wantReadStart: time.Date(2026, 2, 26, 1, 55, 0, 0, time.UTC),
			wantReadEnd:   time.Date(2026, 2, 26, 2, 5, 0, 0, time.UTC),
		},
		{
			name:          "both flags agree when the window is already on the step",
			start:         onStart,
			end:           onEnd,
			step:          step,
			align:         false,
			wantReadStart: time.Date(2026, 2, 26, 1, 55, 0, 0, time.UTC),
			wantReadEnd:   onEnd,
		},
		{
			name:          "alignment on does not move a window that is already on the step",
			start:         onStart,
			end:           onEnd,
			step:          step,
			align:         true,
			wantReadStart: time.Date(2026, 2, 26, 1, 55, 0, 0, time.UTC),
			wantReadEnd:   onEnd,
		},
		{
			name:          "a zero step is widened and not aligned",
			start:         offStart,
			end:           offEnd,
			step:          0,
			align:         true,
			wantReadStart: time.Date(2026, 2, 26, 1, 55, 10, 0, time.UTC),
			wantReadEnd:   offEnd,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qb := GetQueryBounds(tt.start, tt.end, tt.step, expr, QueryTypeMetricRange, tt.align)
			require.Equal(t, tt.start, qb.RequestedStart)
			require.Equal(t, tt.end, qb.RequestedEnd)
			require.Equal(t, tt.wantReadStart, qb.ReadStart)
			require.Equal(t, tt.wantReadEnd, qb.ReadEnd)
		})
	}
}

func TestGetQueryBounds_WidenFollowsAlignment(t *testing.T) {
	// offset 30s is not a multiple of the step, so the widened end stays off the
	// minute that alignment just produced.
	expr := mustExpr(t, `sum(count_over_time({job="api"} |= "timeout" [5m] offset 30s))`)
	start := time.Date(2026, 2, 26, 2, 0, 10, 0, time.UTC)
	end := time.Date(2026, 2, 26, 2, 5, 10, 0, time.UTC)
	step := time.Minute.Milliseconds()

	off := GetQueryBounds(start, end, step, expr, QueryTypeMetricRange, false)
	require.Equal(t, time.Date(2026, 2, 26, 1, 54, 30, 0, time.UTC), off.ReadStart)
	require.Equal(t, time.Date(2026, 2, 26, 2, 5, 30, 0, time.UTC), off.ReadEnd)

	on := GetQueryBounds(start, end, step, expr, QueryTypeMetricRange, true)
	require.Equal(t, time.Date(2026, 2, 26, 1, 54, 30, 0, time.UTC), on.ReadStart)
	require.Equal(t, time.Date(2026, 2, 26, 2, 4, 30, 0, time.UTC), on.ReadEnd)
}

func TestGetQueryBounds_MultiGroupUsesTheWidestLookback(t *testing.T) {
	expr := mustExpr(t, `count_over_time({job="a"}[5m] offset 1m) + count_over_time({job="b"}[10m])`)
	start := time.Date(2026, 2, 26, 2, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 26, 2, 10, 0, 0, time.UTC)

	qb := GetQueryBounds(start, end, time.Minute.Milliseconds(), expr, QueryTypeMetricRange, false)
	require.Equal(t, time.Date(2026, 2, 26, 1, 50, 0, 0, time.UTC), qb.ReadStart)
	require.Equal(t, end, qb.ReadEnd)
}

func TestGetQueryBounds_NonRangeTypesSkipAlignment(t *testing.T) {
	start := time.Date(2026, 2, 26, 2, 0, 10, 0, time.UTC)
	end := time.Date(2026, 2, 26, 2, 5, 10, 0, time.UTC)
	step := time.Minute.Milliseconds()

	logExpr := mustExpr(t, `{job="api"} |= "timeout"`)
	logBounds := GetQueryBounds(start, end, step, logExpr, QueryTypeLog, true)
	require.Equal(t, start, logBounds.ReadStart)
	require.Equal(t, end, logBounds.ReadEnd)

	detected := GetQueryBounds(start, end, step, logExpr, QueryTypeDetectedFields, true)
	require.Equal(t, start, detected.ReadStart)
	require.Equal(t, end, detected.ReadEnd)

	// An instant request is one timestamp. A non-zero step must not floor it.
	instantExpr := mustExpr(t, `sum(count_over_time({job="api"}[5m]))`)
	instant := GetQueryBounds(end, end, step, instantExpr, QueryTypeMetricInstant, true)
	require.Equal(t, end, instant.RequestedStart)
	require.Equal(t, end, instant.RequestedEnd)
	require.Equal(t, time.Date(2026, 2, 26, 2, 0, 10, 0, time.UTC), instant.ReadStart)
	require.Equal(t, end, instant.ReadEnd)
}

func TestGetQueryBounds_NormalizesToUTC(t *testing.T) {
	loc := time.FixedZone("UTC-7", -7*60*60)
	start := time.Date(2026, 2, 26, 2, 0, 10, 0, loc)
	end := time.Date(2026, 2, 26, 2, 5, 10, 0, loc)
	expr := mustExpr(t, `{job="api"}`)

	qb := GetQueryBounds(start, end, 0, expr, QueryTypeLog, false)
	require.Equal(t, time.UTC, qb.RequestedStart.Location())
	require.Equal(t, time.UTC, qb.ReadEnd.Location())
	require.True(t, qb.RequestedStart.Equal(start))
	require.True(t, qb.RequestedEnd.Equal(end))
}

func TestGetQueryType(t *testing.T) {
	start := time.Date(2026, 2, 26, 2, 0, 10, 0, time.UTC)
	end := time.Date(2026, 2, 26, 2, 5, 10, 0, time.UTC)
	metric := mustExpr(t, `sum(count_over_time({job="api"}[5m]))`)
	logExpr := mustExpr(t, `{job="api"} |= "timeout"`)

	tests := []struct {
		name string
		req  queryrangebase.Request
		expr syntax.Expr
		want QueryType
	}{
		{
			name: "range metric",
			req:  &queryrange.LokiRequest{StartTs: start, EndTs: end},
			expr: metric,
			want: QueryTypeMetricRange,
		},
		{
			name: "range log",
			req:  &queryrange.LokiRequest{StartTs: start, EndTs: end},
			expr: logExpr,
			want: QueryTypeLog,
		},
		{
			name: "instant metric",
			req:  &queryrange.LokiInstantRequest{TimeTs: end},
			expr: metric,
			want: QueryTypeMetricInstant,
		},
		{
			name: "instant log",
			req:  &queryrange.LokiInstantRequest{TimeTs: end},
			expr: logExpr,
			want: QueryTypeLog,
		},
		{
			name: "detected fields wins over a metric expression",
			req:  queryrange.NewDetectedFieldsRequest(start, end, 10, 10, time.Minute.Milliseconds(), `sum(count_over_time({job="api"}[5m]))`, ""),
			expr: metric,
			want: QueryTypeDetectedFields,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, GetQueryType(tt.req, tt.expr))
		})
	}
}

func mustExpr(t *testing.T, query string) syntax.Expr {
	t.Helper()
	expr, err := syntax.ParseExpr(query)
	require.NoError(t, err)
	return expr
}
