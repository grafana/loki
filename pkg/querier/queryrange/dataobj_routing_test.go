package queryrange

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

var (
	dataObjRoutingNow     = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	dataObjRoutingLag     = 3 * time.Hour
	dataObjRoutingBandEnd = dataObjRoutingNow.Add(-dataObjRoutingLag)

	// The test tenants hold data objects from now - 48h to now - 3h.
	dataObjRoutingBandStart = dataObjRoutingNow.Add(-48 * time.Hour)
)

// dataObjTenantLimits turns stream-first and data objects on for the tenants. Every tenant reads
// data objects from dataObjRoutingBandStart on.
func dataObjTenantLimits(tenants ...string) fakeLimits {
	limits := fakeLimits{
		streamFirstEnabled:    map[string]bool{},
		dataObjQueryStartTime: map[string]time.Time{},
	}
	for _, tenant := range tenants {
		limits.streamFirstEnabled[tenant] = true
		limits.dataObjQueryStartTime[tenant] = dataObjRoutingBandStart
	}
	return limits
}

func TestDataObjResolve(t *testing.T) {
	cfg := Config{DataObjEnabled: true, DataObjStorageLag: dataObjRoutingLag, now: func() time.Time { return dataObjRoutingNow }}
	insideStart, insideEnd := dataObjRoutingNow.Add(-24*time.Hour), dataObjRoutingNow.Add(-12*time.Hour)

	resolve := func(limits fakeLimits, tenants []string, query string, start, end time.Time) dataObjResolution {
		return dataObjResolve(cfg, limits, tenants, syntax.MustParseExpr(query), start, end)
	}

	// streamFirstRequestRange returns the request range whose read window for streamFirstQuery is
	// windowStart to windowEnd. The range vector of 1m moves the start by 1m, and the end is exclusive.
	streamFirstRequestRange := func(windowStart, windowEnd time.Time) (start, end time.Time) {
		return windowStart.Add(time.Minute), windowEnd.Add(-time.Nanosecond)
	}

	t.Run("accepts a stream-first sum inside the data-object range", func(t *testing.T) {
		res := resolve(dataObjTenantLimits("1"), []string{"1"}, streamFirstQuery, insideStart, insideEnd)

		require.Equal(t, "eligible", res.outcome)
		require.InDelta(t, 1.0, res.coverage, 1e-9)
		require.True(t, res.prefers())
	})

	t.Run("rejects every query when the config turns data objects off", func(t *testing.T) {
		res := dataObjResolve(Config{DataObjStorageLag: dataObjRoutingLag}, dataObjTenantLimits("1"), []string{"1"}, syntax.MustParseExpr(streamFirstQuery), insideStart, insideEnd)

		require.Equal(t, "data_objects_disabled", res.outcome)
		require.False(t, res.prefers())
	})

	t.Run("rejects a log query", func(t *testing.T) {
		res := resolve(dataObjTenantLimits("1"), []string{"1"}, `{app="a"} |= "x"`, insideStart, insideEnd)

		require.Equal(t, "unsupported_query", res.outcome)
		require.False(t, res.prefers())
	})

	t.Run("rejects a range aggregation that is not a sum", func(t *testing.T) {
		res := resolve(dataObjTenantLimits("1"), []string{"1"}, `count_over_time({app="a"}[1m])`, insideStart, insideEnd)

		require.Equal(t, "unsupported_query", res.outcome)
		require.Zero(t, res.coverage)
	})

	t.Run("rejects a sum of an aggregation that stream-first order does not support", func(t *testing.T) {
		res := resolve(dataObjTenantLimits("1"), []string{"1"}, `sum(rate({app="a"}[1m]))`, insideStart, insideEnd)

		require.Equal(t, "unsupported_query", res.outcome)
	})

	t.Run("rejects a tenant that turns stream-first off", func(t *testing.T) {
		limits := dataObjTenantLimits("1")
		limits.streamFirstEnabled["1"] = false

		res := resolve(limits, []string{"1"}, streamFirstQuery, insideStart, insideEnd)

		require.Equal(t, "stream_first_disabled", res.outcome)
	})

	t.Run("rejects a tenant without a data-object start time", func(t *testing.T) {
		limits := dataObjTenantLimits("1")
		delete(limits.dataObjQueryStartTime, "1")

		res := resolve(limits, []string{"1"}, streamFirstQuery, insideStart, insideEnd)

		require.Equal(t, "data_objects_disabled", res.outcome)
	})

	t.Run("rejects a request without tenants", func(t *testing.T) {
		res := resolve(dataObjTenantLimits("1"), nil, streamFirstQuery, insideStart, insideEnd)

		require.Equal(t, "data_objects_disabled", res.outcome)
	})

	t.Run("rejects a request that ends before the tenant start time", func(t *testing.T) {
		res := resolve(dataObjTenantLimits("1"), []string{"1"}, streamFirstQuery, dataObjRoutingBandStart.Add(-10*time.Hour), dataObjRoutingBandStart.Add(-5*time.Hour))

		require.Equal(t, "outside_data_object_range", res.outcome)
	})

	t.Run("rejects a request that starts after now minus the storage lag", func(t *testing.T) {
		res := resolve(dataObjTenantLimits("1"), []string{"1"}, streamFirstQuery, dataObjRoutingNow.Add(-2*time.Hour), dataObjRoutingNow.Add(-time.Hour))

		require.Equal(t, "outside_data_object_range", res.outcome)
	})

	t.Run("rejects a tenant whose start time is not before now minus the storage lag", func(t *testing.T) {
		limits := dataObjTenantLimits("1")
		limits.dataObjQueryStartTime["1"] = dataObjRoutingBandEnd

		res := resolve(limits, []string{"1"}, streamFirstQuery, insideStart, insideEnd)

		require.Equal(t, "outside_data_object_range", res.outcome)
	})

	t.Run("rejects a request whose read window ends exactly where the held range starts", func(t *testing.T) {
		end := dataObjRoutingBandStart.Add(-time.Nanosecond)

		res := resolve(dataObjTenantLimits("1"), []string{"1"}, streamFirstQuery, end.Add(-time.Hour), end)

		require.Equal(t, "outside_data_object_range", res.outcome)
	})

	t.Run("accepts a request whose read window ends one nanosecond after the held range starts", func(t *testing.T) {
		res := resolve(dataObjTenantLimits("1"), []string{"1"}, streamFirstQuery, dataObjRoutingBandStart.Add(-time.Hour), dataObjRoutingBandStart)

		require.Equal(t, "eligible", res.outcome)
	})

	t.Run("rejects a request whose read window starts exactly where the held range ends", func(t *testing.T) {
		start := dataObjRoutingBandEnd.Add(time.Minute)

		res := resolve(dataObjTenantLimits("1"), []string{"1"}, streamFirstQuery, start, start.Add(time.Hour))

		require.Equal(t, "outside_data_object_range", res.outcome)
		require.Zero(t, res.coverage)
	})

	t.Run("accepts a request that overlaps the range by one nanosecond of its read window", func(t *testing.T) {
		start := dataObjRoutingBandEnd.Add(time.Minute).Add(-time.Nanosecond)

		res := resolve(dataObjTenantLimits("1"), []string{"1"}, streamFirstQuery, start, start)

		require.Equal(t, "eligible", res.outcome)
	})

	t.Run("counts the range vector in the time range the query reads", func(t *testing.T) {
		start, end := dataObjRoutingNow.Add(-2*time.Hour), dataObjRoutingNow.Add(-90*time.Minute)

		short := resolve(dataObjTenantLimits("1"), []string{"1"}, streamFirstQuery, start, end)
		long := resolve(dataObjTenantLimits("1"), []string{"1"}, `sum(count_over_time({app="a"}[2h]))`, start, end)

		require.Equal(t, "outside_data_object_range", short.outcome)
		require.Equal(t, "eligible", long.outcome)
	})

	t.Run("counts the offset in the time range the query reads", func(t *testing.T) {
		start, end := dataObjRoutingNow.Add(-2*time.Hour), dataObjRoutingNow.Add(-time.Hour)

		res := resolve(dataObjTenantLimits("1"), []string{"1"}, `sum(count_over_time({app="a"}[1m] offset 24h))`, start, end)

		require.Equal(t, "eligible", res.outcome)
		require.InDelta(t, 1.0, res.coverage, 1e-9)
	})

	t.Run("reports the share of the read window that data objects hold", func(t *testing.T) {
		windowStart := dataObjRoutingBandEnd.Add(-25 * time.Minute)
		start, end := streamFirstRequestRange(windowStart, windowStart.Add(100*time.Minute))

		res := resolve(dataObjTenantLimits("1"), []string{"1"}, streamFirstQuery, start, end)

		require.Equal(t, "eligible", res.outcome)
		require.InDelta(t, 0.25, res.coverage, 1e-9)
		require.False(t, res.prefers())
	})

	t.Run("does not prefer data objects at exactly half coverage", func(t *testing.T) {
		windowStart := dataObjRoutingBandEnd.Add(-50 * time.Minute)
		start, end := streamFirstRequestRange(windowStart, windowStart.Add(100*time.Minute))

		res := resolve(dataObjTenantLimits("1"), []string{"1"}, streamFirstQuery, start, end)

		require.Equal(t, "eligible", res.outcome)
		require.False(t, res.prefers())
	})

	t.Run("prefers data objects above half coverage", func(t *testing.T) {
		windowStart := dataObjRoutingBandEnd.Add(-51 * time.Minute)
		start, end := streamFirstRequestRange(windowStart, windowStart.Add(100*time.Minute))

		res := resolve(dataObjTenantLimits("1"), []string{"1"}, streamFirstQuery, start, end)

		require.Equal(t, "eligible", res.outcome)
		require.True(t, res.prefers())
	})

	t.Run("accepts several tenants that all read data objects", func(t *testing.T) {
		res := resolve(dataObjTenantLimits("1", "2"), []string{"1", "2"}, streamFirstQuery, insideStart, insideEnd)

		require.Equal(t, "eligible", res.outcome)
	})

	t.Run("rejects several tenants when one of them has no data-object start time", func(t *testing.T) {
		limits := dataObjTenantLimits("1", "2")
		delete(limits.dataObjQueryStartTime, "2")

		res := resolve(limits, []string{"1", "2"}, streamFirstQuery, insideStart, insideEnd)

		require.Equal(t, "data_objects_disabled", res.outcome)
	})

	t.Run("rejects several tenants when one of them turns stream-first off", func(t *testing.T) {
		limits := dataObjTenantLimits("1", "2")
		limits.streamFirstEnabled["2"] = false

		res := resolve(limits, []string{"1", "2"}, streamFirstQuery, insideStart, insideEnd)

		require.Equal(t, "stream_first_disabled", res.outcome)
	})

	t.Run("uses the latest tenant start time as the start of the data-object range", func(t *testing.T) {
		limits := dataObjTenantLimits("1", "2")
		limits.dataObjQueryStartTime["2"] = dataObjRoutingNow.Add(-10 * time.Hour)

		res := resolve(limits, []string{"1", "2"}, streamFirstQuery, insideStart, insideEnd)

		require.Equal(t, "outside_data_object_range", res.outcome)
	})
}

func TestDataObjAnyPrefers(t *testing.T) {
	cfg := Config{DataObjEnabled: true, DataObjStorageLag: dataObjRoutingLag, now: func() time.Time { return dataObjRoutingNow }}
	limits := dataObjTenantLimits("1")
	tenants := []string{"1"}
	start, end := dataObjRoutingNow.Add(-24*time.Hour), dataObjRoutingNow.Add(-12*time.Hour)

	t.Run("predicts true for a stream-first sum", func(t *testing.T) {
		require.True(t, dataObjAnyPrefers(cfg, limits, tenants, syntax.MustParseExpr(streamFirstQuery), start, end))
	})

	t.Run("predicts true when one leg of a binary expression is a stream-first sum", func(t *testing.T) {
		expr := syntax.MustParseExpr(streamFirstQuery + ` / count(count_over_time({app="b"}[1m]))`)

		require.True(t, dataObjAnyPrefers(cfg, limits, tenants, expr, start, end))
	})

	t.Run("predicts false when no part of the query is a stream-first sum", func(t *testing.T) {
		expr := syntax.MustParseExpr(`rate({app="a"}[1m]) / count(rate({app="b"}[1m]))`)

		require.False(t, dataObjAnyPrefers(cfg, limits, tenants, expr, start, end))
	})

	t.Run("predicts false for a log query", func(t *testing.T) {
		require.False(t, dataObjAnyPrefers(cfg, limits, tenants, syntax.MustParseExpr(`{app="a"}`), start, end))
	})

	t.Run("predicts false when the time range is outside the data-object range", func(t *testing.T) {
		expr := syntax.MustParseExpr(streamFirstQuery)

		require.False(t, dataObjAnyPrefers(cfg, limits, tenants, expr, dataObjRoutingNow.Add(-2*time.Hour), dataObjRoutingNow.Add(-time.Hour)))
	})

	t.Run("uses the longest range vector of the whole query for the time range", func(t *testing.T) {
		withLongRange := syntax.MustParseExpr(streamFirstQuery + ` + sum(count_over_time({app="b"}[3h]))`)
		start, end := dataObjRoutingNow.Add(-3*time.Hour), dataObjRoutingNow.Add(-2*time.Hour)

		require.False(t, dataObjAnyPrefers(cfg, limits, tenants, syntax.MustParseExpr(streamFirstQuery), start, end))
		require.True(t, dataObjAnyPrefers(cfg, limits, tenants, withLongRange, start, end))
	})

	t.Run("uses the longest offset of the whole query for the time range", func(t *testing.T) {
		withLongOffset := syntax.MustParseExpr(streamFirstQuery + ` + sum(count_over_time({app="b"}[1m] offset 24h))`)
		start, end := dataObjRoutingNow.Add(-2*time.Hour), dataObjRoutingNow.Add(-time.Hour)

		require.False(t, dataObjAnyPrefers(cfg, limits, tenants, syntax.MustParseExpr(streamFirstQuery), start, end))
		require.True(t, dataObjAnyPrefers(cfg, limits, tenants, withLongOffset, start, end))
	})

	t.Run("predicts true when the offset moves a short range vector into the held range", func(t *testing.T) {
		expr := syntax.MustParseExpr(`sum(count_over_time({app="a"}[30m] offset 2h))`)
		ts := dataObjRoutingNow.Add(-time.Hour)

		require.True(t, dataObjAnyPrefers(cfg, limits, tenants, expr, ts, ts))
	})

	t.Run("predicts false when the config turns data objects off", func(t *testing.T) {
		off := Config{DataObjStorageLag: dataObjRoutingLag}

		require.False(t, dataObjAnyPrefers(off, limits, tenants, syntax.MustParseExpr(streamFirstQuery), start, end))
	})
}
