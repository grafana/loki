package logql

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
)

// Test to reproduce issue #19580 where rate_counter returns half the expected rate
func TestRateCounterBug19580(t *testing.T) {
	// Create a generator that simulates the bug scenario:
	// Logs emitted every 60 seconds with counter increasing by ~2200 each time
	counterValueGen := func(i int64) logData {
		// Base value: 99608
		// Increase by ~2200 every 60 seconds
		// Samples at t=0, t=60, t=120 with values 99608, 101824, 104040
		baseValue := int64(99608)
		increment := int64(2216) * (i / 60) // Increase every 60 seconds
		value := baseValue + increment

		return logData{
			Entry: logproto.Entry{
				Timestamp: time.Unix(i, 0),
				Line:      fmt.Sprintf(`{"value": %d}`, value),
			},
			Sample: logproto.Sample{
				Timestamp: time.Unix(i, 0).UnixNano(),
				Hash:      uint64(i),
				Value:     float64(value),
			},
		}
	}

	// Create series with samples at t=0, t=60, t=120
	// This simulates logs emitted every minute
	samples := []logproto.Sample{
		counterValueGen(0).Sample,
		counterValueGen(60).Sample,
		counterValueGen(120).Sample,
	}

	series := logproto.Series{
		Samples: samples,
		Labels:  `{app="foo"}`,
	}

	test := struct {
		qs        string
		ts        time.Time
		direction logproto.Direction
		limit     uint32
		data      [][]logproto.Series
		params    []SelectSampleParams
	}{
		qs:        `rate_counter({app="foo"} | unwrap value [2m])`,
		ts:        time.Unix(120, 0), // Query at t=120
		direction: logproto.FORWARD,
		limit:     10,
		data: [][]logproto.Series{
			{series},
		},
		params: []SelectSampleParams{
			{&logproto.SampleQueryRequest{
				Start:    time.Unix(0, 0),   // Range starts at t=0
				End:      time.Unix(120, 0), // Range ends at t=120
				Selector: `rate_counter({app="foo"} | unwrap value[2m])`,
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`rate_counter({app="foo"} | unwrap value[2m])`),
				},
			}},
		},
	}

	eng := NewEngine(EngineOpts{}, newQuerierRecorder(t, test.data, test.params), NoLimits, log.NewNopLogger())
	params, err := NewLiteralParams(test.qs, test.ts, test.ts, 0, 0, test.direction, test.limit, nil, nil)
	require.NoError(t, err)

	q := eng.Query(params)
	res, err := q.Exec(user.InjectOrgID(context.Background(), "fake"))
	require.NoError(t, err)

	vec := res.Data.(promql.Vector)
	require.Len(t, vec, 1)

	// Expected calculation:
	// - Sample at t=0: value = 99608
	// - Sample at t=60: value = 101824
	// - Sample at t=120: value = 104040
	// - Value increase: 104040 - 99608 = 4432
	// - Time range: 120 seconds (2 minutes)
	// - Expected rate: 4432 / 120 = 36.93 messages/second
	expectedRate := 4432.0 / 120.0 // = 36.93333...

	actualRate := vec[0].F

	t.Logf("Expected rate: %.6f messages/second", expectedRate)
	t.Logf("Actual rate: %.6f messages/second", actualRate)
	t.Logf("Ratio (actual/expected): %.6f", actualRate/expectedRate)

	// The bug is that actual rate is approximately half of expected
	// Check if the actual rate is within 10% of the expected rate
	tolerance := expectedRate * 0.1
	require.InDelta(t, expectedRate, actualRate, tolerance,
		"rate_counter returned %.6f but expected %.6f (difference: %.2f%%)",
		actualRate, expectedRate, 100.0*(actualRate-expectedRate)/expectedRate)
}

// Test with only 2 samples to isolate the bug
func TestRateCounterBugTwoSamples(t *testing.T) {
	// Create samples at t=60 and t=120 with values 101824 and 104040
	samples := []logproto.Sample{
		{
			Timestamp: time.Unix(60, 0).UnixNano(),
			Hash:      1,
			Value:     101824.0,
		},
		{
			Timestamp: time.Unix(120, 0).UnixNano(),
			Hash:      2,
			Value:     104040.0,
		},
	}

	series := logproto.Series{
		Samples: samples,
		Labels:  `{app="foo"}`,
	}

	test := struct {
		qs        string
		ts        time.Time
		direction logproto.Direction
		limit     uint32
		data      [][]logproto.Series
		params    []SelectSampleParams
	}{
		qs:        `rate_counter({app="foo"} | unwrap value [2m])`,
		ts:        time.Unix(120, 0),
		direction: logproto.FORWARD,
		limit:     10,
		data: [][]logproto.Series{
			{series},
		},
		params: []SelectSampleParams{
			{&logproto.SampleQueryRequest{
				Start:    time.Unix(0, 0),
				End:      time.Unix(120, 0),
				Selector: `rate_counter({app="foo"} | unwrap value[2m])`,
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`rate_counter({app="foo"} | unwrap value[2m])`),
				},
			}},
		},
	}

	eng := NewEngine(EngineOpts{}, newQuerierRecorder(t, test.data, test.params), NoLimits, log.NewNopLogger())
	params, err := NewLiteralParams(test.qs, test.ts, test.ts, 0, 0, test.direction, test.limit, nil, nil)
	require.NoError(t, err)

	q := eng.Query(params)
	res, err := q.Exec(user.InjectOrgID(context.Background(), "fake"))
	require.NoError(t, err)

	vec := res.Data.(promql.Vector)
	require.Len(t, vec, 1)

	// Expected calculation:
	// - Sample at t=60: value = 101824
	// - Sample at t=120: value = 104040
	// - Value increase: 104040 - 101824 = 2216
	// - Time range: 120 seconds
	// - Expected rate: 2216 / 120 = 18.47 messages/second
	expectedRate := 2216.0 / 120.0

	actualRate := vec[0].F

	t.Logf("Expected rate: %.6f messages/second", expectedRate)
	t.Logf("Actual rate: %.6f messages/second", actualRate)
	t.Logf("Ratio (actual/expected): %.6f", actualRate/expectedRate)

	tolerance := expectedRate * 0.1
	require.InDelta(t, expectedRate, actualRate, tolerance,
		"rate_counter returned %.6f but expected %.6f", actualRate, expectedRate)
}

// Test edge case: counter reset in the middle
func TestRateCounterWithReset(t *testing.T) {
	// Simulate a counter that resets (value decreases)
	samples := []logproto.Sample{
		{
			Timestamp: time.Unix(0, 0).UnixNano(),
			Hash:      1,
			Value:     100.0,
		},
		{
			Timestamp: time.Unix(60, 0).UnixNano(),
			Hash:      2,
			Value:     200.0, // Increased by 100
		},
		{
			Timestamp: time.Unix(120, 0).UnixNano(),
			Hash:      3,
			Value:     50.0, // Reset! (decreased from 200 to 50)
		},
		{
			Timestamp: time.Unix(180, 0).UnixNano(),
			Hash:      4,
			Value:     150.0, // Increased by 100 after reset
		},
	}

	series := logproto.Series{
		Samples: samples,
		Labels:  `{app="foo"}`,
	}

	test := struct {
		qs        string
		ts        time.Time
		direction logproto.Direction
		limit     uint32
		data      [][]logproto.Series
		params    []SelectSampleParams
	}{
		qs:        `rate_counter({app="foo"} | unwrap value [3m])`,
		ts:        time.Unix(180, 0),
		direction: logproto.FORWARD,
		limit:     10,
		data: [][]logproto.Series{
			{series},
		},
		params: []SelectSampleParams{
			{&logproto.SampleQueryRequest{
				Start:    time.Unix(0, 0),
				End:      time.Unix(180, 0),
				Selector: `rate_counter({app="foo"} | unwrap value[3m])`,
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`rate_counter({app="foo"} | unwrap value[3m])`),
				},
			}},
		},
	}

	eng := NewEngine(EngineOpts{}, newQuerierRecorder(t, test.data, test.params), NoLimits, log.NewNopLogger())
	params, err := NewLiteralParams(test.qs, test.ts, test.ts, 0, 0, test.direction, test.limit, nil, nil)
	require.NoError(t, err)

	q := eng.Query(params)
	res, err := q.Exec(user.InjectOrgID(context.Background(), "fake"))
	require.NoError(t, err)

	vec := res.Data.(promql.Vector)
	require.Len(t, vec, 1)

	// Expected: (150 - 100) + 200 (added at reset) = 250 total increase over 180 seconds
	// Rate: 250 / 180 = 1.389 per second
	actualRate := vec[0].F

	t.Logf("Actual rate with counter reset: %.6f", actualRate)
	// Counter reset handling adds the last value before reset (200) to the total
	// So we should see a positive rate even with the reset
	require.Greater(t, actualRate, 0.0, "rate should be positive even with counter reset")
}

// Test edge case: irregular sample intervals
func TestRateCounterIrregularIntervals(t *testing.T) {
	// Samples at irregular intervals: 0s, 45s, 90s, 180s
	samples := []logproto.Sample{
		{
			Timestamp: time.Unix(0, 0).UnixNano(),
			Hash:      1,
			Value:     1000.0,
		},
		{
			Timestamp: time.Unix(45, 0).UnixNano(),
			Hash:      2,
			Value:     1500.0, // +500 in 45s
		},
		{
			Timestamp: time.Unix(90, 0).UnixNano(),
			Hash:      3,
			Value:     2000.0, // +500 in 45s
		},
		{
			Timestamp: time.Unix(180, 0).UnixNano(),
			Hash:      4,
			Value:     3000.0, // +1000 in 90s
		},
	}

	series := logproto.Series{
		Samples: samples,
		Labels:  `{app="foo"}`,
	}

	test := struct {
		qs        string
		ts        time.Time
		direction logproto.Direction
		limit     uint32
		data      [][]logproto.Series
		params    []SelectSampleParams
	}{
		qs:        `rate_counter({app="foo"} | unwrap value [3m])`,
		ts:        time.Unix(180, 0),
		direction: logproto.FORWARD,
		limit:     10,
		data: [][]logproto.Series{
			{series},
		},
		params: []SelectSampleParams{
			{&logproto.SampleQueryRequest{
				Start:    time.Unix(0, 0),
				End:      time.Unix(180, 0),
				Selector: `rate_counter({app="foo"} | unwrap value[3m])`,
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`rate_counter({app="foo"} | unwrap value[3m])`),
				},
			}},
		},
	}

	eng := NewEngine(EngineOpts{}, newQuerierRecorder(t, test.data, test.params), NoLimits, log.NewNopLogger())
	params, err := NewLiteralParams(test.qs, test.ts, test.ts, 0, 0, test.direction, test.limit, nil, nil)
	require.NoError(t, err)

	q := eng.Query(params)
	res, err := q.Exec(user.InjectOrgID(context.Background(), "fake"))
	require.NoError(t, err)

	vec := res.Data.(promql.Vector)
	require.Len(t, vec, 1)

	// Total increase: 3000 - 1000 = 2000
	// Time range: 180 seconds
	// Expected rate: 2000 / 180 = 11.111... per second
	expectedRate := 2000.0 / 180.0
	actualRate := vec[0].F

	t.Logf("Expected rate (irregular intervals): %.6f", expectedRate)
	t.Logf("Actual rate (irregular intervals): %.6f", actualRate)

	tolerance := expectedRate * 0.15 // Allow 15% tolerance due to extrapolation
	require.InDelta(t, expectedRate, actualRate, tolerance,
		"rate_counter should handle irregular intervals correctly")
}

// Test edge case: single sample at boundary
func TestRateCounterSingleSampleAtBoundary(t *testing.T) {
	// Only one sample, exactly at the range start boundary
	samples := []logproto.Sample{
		{
			Timestamp: time.Unix(0, 0).UnixNano(),
			Hash:      1,
			Value:     1000.0,
		},
	}

	series := logproto.Series{
		Samples: samples,
		Labels:  `{app="foo"}`,
	}

	test := struct {
		qs        string
		ts        time.Time
		direction logproto.Direction
		limit     uint32
		data      [][]logproto.Series
		params    []SelectSampleParams
	}{
		qs:        `rate_counter({app="foo"} | unwrap value [2m])`,
		ts:        time.Unix(120, 0),
		direction: logproto.FORWARD,
		limit:     10,
		data: [][]logproto.Series{
			{series},
		},
		params: []SelectSampleParams{
			{&logproto.SampleQueryRequest{
				Start:    time.Unix(0, 0),
				End:      time.Unix(120, 0),
				Selector: `rate_counter({app="foo"} | unwrap value[2m])`,
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`rate_counter({app="foo"} | unwrap value[2m])`),
				},
			}},
		},
	}

	eng := NewEngine(EngineOpts{}, newQuerierRecorder(t, test.data, test.params), NoLimits, log.NewNopLogger())
	params, err := NewLiteralParams(test.qs, test.ts, test.ts, 0, 0, test.direction, test.limit, nil, nil)
	require.NoError(t, err)

	q := eng.Query(params)
	res, err := q.Exec(user.InjectOrgID(context.Background(), "fake"))
	require.NoError(t, err)

	vec := res.Data.(promql.Vector)
	require.Len(t, vec, 1)

	// With only one sample, rate should be 0 (need at least 2 samples for rate)
	actualRate := vec[0].F
	t.Logf("Actual rate with single sample: %.6f", actualRate)
	require.Equal(t, 0.0, actualRate, "rate should be 0 with only one sample")
}
