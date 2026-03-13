package main

import (
	"time"

	"github.com/prometheus/prometheus/prompb"
)

// buildMetrics converts parsed test results into Prometheus TimeSeries.
func buildMetrics(results *parsedResults, rangeType, job string) []prompb.TimeSeries {
	now := time.Now().UnixMilli()
	var series []prompb.TimeSeries

	// Per-test duration (only for parseable names)
	for _, t := range results.Tests {
		if t.Labels == nil {
			continue
		}
		series = append(series, prompb.TimeSeries{
			Labels: []prompb.Label{
				{Name: "__name__", Value: "logql_correctness_test_duration_seconds"},
				{Name: "direction", Value: t.Labels.Direction},
				{Name: "job", Value: job},
				{Name: "kind", Value: t.Labels.Kind},
				{Name: "query_file", Value: t.Labels.QueryFile},
				{Name: "range_type", Value: rangeType},
				{Name: "status", Value: string(t.Status)},
				{Name: "suite", Value: t.Labels.Suite},
			},
			Samples: []prompb.Sample{{Value: t.DurationSec, Timestamp: now}},
		})
	}

	// Aggregate: tests_total and pass_ratio per suite
	type suiteStats struct {
		pass, fail, err, skip int
	}
	suites := map[string]*suiteStats{}

	for _, t := range results.Tests {
		suite := "unknown"
		if t.Labels != nil {
			suite = t.Labels.Suite
		}
		if suites[suite] == nil {
			suites[suite] = &suiteStats{}
		}
		switch t.Status {
		case statusPass:
			suites[suite].pass++
		case statusFail:
			suites[suite].fail++
		case statusError:
			suites[suite].err++
		case statusSkip:
			suites[suite].skip++
		}
	}

	for suite, stats := range suites {
		for _, entry := range []struct {
			status string
			count  int
		}{
			{"pass", stats.pass},
			{"fail", stats.fail},
			{"error", stats.err},
			{"skip", stats.skip},
		} {
			series = append(series, prompb.TimeSeries{
				Labels: []prompb.Label{
					{Name: "__name__", Value: "logql_correctness_tests_total"},
					{Name: "job", Value: job},
					{Name: "range_type", Value: rangeType},
					{Name: "status", Value: entry.status},
					{Name: "suite", Value: suite},
				},
				Samples: []prompb.Sample{{Value: float64(entry.count), Timestamp: now}},
			})
		}

		denom := stats.pass + stats.fail + stats.err
		if denom > 0 {
			ratio := float64(stats.pass) / float64(denom)
			series = append(series, prompb.TimeSeries{
				Labels: []prompb.Label{
					{Name: "__name__", Value: "logql_correctness_run_pass_ratio"},
					{Name: "job", Value: job},
					{Name: "range_type", Value: rangeType},
					{Name: "suite", Value: suite},
				},
				Samples: []prompb.Sample{{Value: ratio, Timestamp: now}},
			})
		}
	}

	// Run-level duration
	series = append(series, prompb.TimeSeries{
		Labels: []prompb.Label{
			{Name: "__name__", Value: "logql_correctness_run_duration_seconds"},
			{Name: "job", Value: job},
			{Name: "range_type", Value: rangeType},
		},
		Samples: []prompb.Sample{{Value: results.TotalDurationSec, Timestamp: now}},
	})

	return series
}
