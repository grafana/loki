package queryrange

import (
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/validation"
)

type splitter interface {
	split(tenantIDs []string, request queryrangebase.Request, interval time.Duration) ([]queryrangebase.Request, error)
}

type defaultSplitter struct {
	limits Limits
	iqo    util.IngesterQueryOptions
}

func newDefaultSplitter(limits Limits, iqo util.IngesterQueryOptions) *defaultSplitter {
	return &defaultSplitter{limits, iqo}
}

func (s *defaultSplitter) split(tenantIDs []string, req queryrangebase.Request, interval time.Duration) ([]queryrangebase.Request, error) {
	var (
		reqs             []queryrangebase.Request
		factory          func(start, end time.Time)
		endTimeInclusive = true
	)

	switch r := req.(type) {
	case *LokiRequest:
		endTimeInclusive = false
		factory = func(start, end time.Time) {
			reqs = append(reqs, &LokiRequest{
				Query:     r.Query,
				Limit:     r.Limit,
				Step:      r.Step,
				Interval:  r.Interval,
				Direction: r.Direction,
				Path:      r.Path,
				StartTs:   start,
				EndTs:     end,
				Plan:      r.Plan,
			})
		}
	case *LokiSeriesRequest:
		// metadata queries have end time inclusive.
		// Set endTimeInclusive to true so that ForInterval keeps a gap of 1ms between splits to
		// avoid querying duplicate data in adjacent queries.
		factory = func(start, end time.Time) {
			reqs = append(reqs, &LokiSeriesRequest{
				Match:   r.Match,
				Path:    r.Path,
				StartTs: start,
				EndTs:   end,
				Shards:  r.Shards,
			})
		}
	case *LabelRequest:
		// metadata queries have end time inclusive.
		// Set endTimeInclusive to true so that ForInterval keeps a gap of 1ms between splits to
		// avoid querying duplicate data in adjacent queries.
		factory = func(start, end time.Time) {
			reqs = append(reqs, NewLabelRequest(start, end, r.Query, r.Name, r.Path()))
		}
	case *logproto.IndexStatsRequest:
		factory = func(start, end time.Time) {
			reqs = append(reqs, &logproto.IndexStatsRequest{
				From:     model.TimeFromUnix(start.Unix()),
				Through:  model.TimeFromUnix(end.Unix()),
				Matchers: r.GetMatchers(),
			})
		}
	case *logproto.VolumeRequest:
		factory = func(start, end time.Time) {
			reqs = append(reqs, &logproto.VolumeRequest{
				From:         model.TimeFromUnix(start.Unix()),
				Through:      model.TimeFromUnix(end.Unix()),
				Matchers:     r.GetMatchers(),
				Limit:        r.Limit,
				TargetLabels: r.TargetLabels,
				AggregateBy:  r.AggregateBy,
			})
		}
	default:
		return nil, nil
	}

	// Treat range of given query within the `query_ingesters_within` window differently by splitting using the `split_ingester_queries_by_interval`
	// instead of the default `split_queries_by_interval`; rebound the start/end time after doing so to build intervals
	// for queries outside the `query_ingesters_within` window.
	//
	// The given factory is responsible for building the splits and appending to reqs.
	start, end := buildIngesterQuerySplitsAndRebound(s.limits, s.iqo, tenantIDs, req, factory)
	if !start.Equal(end) {
		util.ForInterval(interval, start, end, endTimeInclusive, factory)
	}
	return reqs, nil
}

type metricQuerySplitter struct {
	limits Limits
	iqo    util.IngesterQueryOptions
}

func newMetricQuerySplitter(limits Limits, iqo util.IngesterQueryOptions) *metricQuerySplitter {
	return &metricQuerySplitter{limits, iqo}
}

// reduceSplitIntervalForRangeVector reduces the split interval for a range query based on the duration of the range vector.
// Large range vector durations will not be split into smaller intervals because it can cause the queries to be slow by over-processing data.
func (s *metricQuerySplitter) reduceSplitIntervalForRangeVector(r *LokiRequest, interval time.Duration) (time.Duration, error) {
	maxRange, _, err := maxRangeVectorAndOffsetDuration(r.Plan.AST)
	if err != nil {
		return 0, err
	}
	if maxRange > interval {
		return maxRange, nil
	}
	return interval, nil
}

// Round up to the step before the next interval boundary.
func (s *metricQuerySplitter) nextIntervalBoundary(t time.Time, step int64, interval time.Duration) time.Time {
	stepNs := step * 1e6
	nsPerInterval := interval.Nanoseconds()
	startOfNextInterval := ((t.UnixNano() / nsPerInterval) + 1) * nsPerInterval
	// ensure that target is a multiple of steps away from the start time
	target := startOfNextInterval - ((startOfNextInterval - t.UnixNano()) % stepNs)
	if target == startOfNextInterval {
		target -= stepNs
	}
	return time.Unix(0, target)
}

func (s *metricQuerySplitter) split(tenantIDs []string, r queryrangebase.Request, interval time.Duration) ([]queryrangebase.Request, error) {
	var reqs []queryrangebase.Request

	lokiReq := r.(*LokiRequest)

	interval, err := s.reduceSplitIntervalForRangeVector(lokiReq, interval)
	if err != nil {
		return nil, err
	}

	// step align start and end time of the query. Start time is rounded down and end time is rounded up.
	stepNs := r.GetStep() * 1e6
	startNs := lokiReq.StartTs.UnixNano()
	start := time.Unix(0, startNs-startNs%stepNs)

	endNs := lokiReq.EndTs.UnixNano()
	if mod := endNs % stepNs; mod != 0 {
		endNs += stepNs - mod
	}
	end := time.Unix(0, endNs)

	lokiReq = lokiReq.WithStartEnd(start, end).(*LokiRequest)

	// step is >= configured split interval, let us just split the query interval by step
	if lokiReq.Step >= interval.Milliseconds() {
		util.ForInterval(time.Duration(lokiReq.Step*1e6), lokiReq.StartTs, lokiReq.EndTs, false, func(start, end time.Time) {
			reqs = append(reqs, &LokiRequest{
				Query:     lokiReq.Query,
				Limit:     lokiReq.Limit,
				Step:      lokiReq.Step,
				Interval:  lokiReq.Interval,
				Direction: lokiReq.Direction,
				Path:      lokiReq.Path,
				StartTs:   start,
				EndTs:     end,
				Plan:      lokiReq.Plan,
			})
		})

		return reqs, nil
	}

	for start := lokiReq.StartTs; start.Before(lokiReq.EndTs); start = s.nextIntervalBoundary(start, r.GetStep(), interval).Add(time.Duration(r.GetStep()) * time.Millisecond) {
		end := s.nextIntervalBoundary(start, r.GetStep(), interval)
		if end.Add(time.Duration(r.GetStep())*time.Millisecond).After(lokiReq.EndTs) || end.Add(time.Duration(r.GetStep())*time.Millisecond) == lokiReq.EndTs {
			end = lokiReq.EndTs
		}
		reqs = append(reqs, &LokiRequest{
			Query:     lokiReq.Query,
			Limit:     lokiReq.Limit,
			Step:      lokiReq.Step,
			Interval:  lokiReq.Interval,
			Direction: lokiReq.Direction,
			Path:      lokiReq.Path,
			StartTs:   start,
			EndTs:     end,
			Plan:      lokiReq.Plan,
		})
	}

	return reqs, nil
}

// buildIngesterQuerySplitsAndRebound creates subqueries for the given request if it spans the `query_ingesters_within` window.
// It returns the new start & end times which exclude this window.
func buildIngesterQuerySplitsAndRebound(limits Limits, iqo util.IngesterQueryOptions, tenantIDs []string, req queryrangebase.Request, factory func(start, end time.Time)) (time.Time, time.Time) {
	start, end := req.GetStart().UTC(), req.GetEnd().UTC()

	// ingesters are not queried, nothing to do
	if iqo == nil || iqo.QueryStoreOnly() {
		return start, end
	}

	// split_ingester_queries_by_interval = 30m
	// start = 	12:00
	// end = 	15:27
	// now =	16:00
	// query window = 13:00 - 16:00
	// needs splits from $queryWindowStart (13:00) to end
	// 12:00 to 12:59:59.999 unaffected
	// rewrite end time to exclude query window
	// result:
	// 		start = 12:00
	//		end =	12:59:59.999
	//		splits = [13:00, 13:30, 14:00, 14:30, 15:00, 15:27]

	windowSize := iqo.QueryIngestersWithin()
	queryWindowStart := time.Now().UTC().Add(-windowSize)

	// clamp to the start time
	if queryWindowStart.Before(start) {
		queryWindowStart = start
	}

	// query range does not overlap with ingester query window, nothing to do
	if end.Before(queryWindowStart) {
		return start, end
	}

	newStart := start
	newEnd := queryWindowStart.Add(-time.Nanosecond)
	if start.After(newEnd) {
		// query is fully within the ingester query window
		newStart = newEnd
	}

	interval := validation.MaxDurationPerTenant(tenantIDs, limits.IngesterQuerySplitDuration)
	util.ForInterval(interval, queryWindowStart, end, true, factory) // TODO end time always inclusive?

	return newStart, newEnd
}
