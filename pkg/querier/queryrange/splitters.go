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
	split(execTime time.Time, tenantIDs []string, request queryrangebase.Request, interval time.Duration) ([]queryrangebase.Request, error)
}

type defaultSplitter struct {
	limits Limits
	iqo    util.IngesterQueryOptions
}

func newDefaultSplitter(limits Limits, iqo util.IngesterQueryOptions) *defaultSplitter {
	return &defaultSplitter{limits, iqo}
}

func (s *defaultSplitter) split(execTime time.Time, tenantIDs []string, req queryrangebase.Request, interval time.Duration) ([]queryrangebase.Request, error) {
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

	var (
		ingesterSplits []queryrangebase.Request
		origStart      = req.GetStart().UTC()
		origEnd        = req.GetEnd().UTC()
	)

	start, end, needsIngesterSplits := ingesterQueryBounds(execTime, s.iqo, req)

	if ingesterQueryInterval := validation.MaxDurationPerTenant(tenantIDs, s.limits.IngesterQuerySplitDuration); ingesterQueryInterval != 0 && needsIngesterSplits {
		// perform splitting using special interval (`split_ingester_queries_by_interval`)
		util.ForInterval(ingesterQueryInterval, start, end, endTimeInclusive, factory)

		// rebound after ingester queries have been split out
		end = start
		start = req.GetStart().UTC()
		if endTimeInclusive {
			end = end.Add(-util.SplitGap)
		}

		// query only overlaps ingester query window, nothing more to do
		if start.After(end) || start.Equal(end) {
			return reqs, nil
		}

		// copy the splits, reset the results
		ingesterSplits = reqs
		reqs = nil
	} else {
		start = origStart
		end = origEnd
	}

	// perform splitting over the rest of the time range
	util.ForInterval(interval, origStart, end, endTimeInclusive, factory)

	// move the ingester splits to the end to maintain correct order
	reqs = append(reqs, ingesterSplits...)
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

func (s *metricQuerySplitter) split(execTime time.Time, tenantIDs []string, r queryrangebase.Request, interval time.Duration) ([]queryrangebase.Request, error) {
	var reqs []queryrangebase.Request

	lokiReq := r.(*LokiRequest)

	interval, err := s.reduceSplitIntervalForRangeVector(lokiReq, interval)
	if err != nil {
		return nil, err
	}

	start, end := s.alignStartEnd(r.GetStep(), lokiReq.StartTs, lokiReq.EndTs)

	lokiReq = lokiReq.WithStartEnd(start, end).(*LokiRequest)

	factory := func(start, end time.Time) {
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

	// step is >= configured split interval, let us just split the query interval by step
	// TODO this is likely buggy when step >= query range, how should we handle this?
	if lokiReq.Step >= interval.Milliseconds() {
		util.ForInterval(time.Duration(lokiReq.Step*1e6), lokiReq.StartTs, lokiReq.EndTs, false, factory)

		return reqs, nil
	}

	var (
		ingesterSplits      []queryrangebase.Request
		needsIngesterSplits bool
	)

	origStart := start
	origEnd := end

	start, end, needsIngesterSplits = ingesterQueryBounds(execTime, s.iqo, lokiReq)
	start, end = s.alignStartEnd(r.GetStep(), start, end)

	if ingesterQueryInterval := validation.MaxDurationPerTenant(tenantIDs, s.limits.IngesterQuerySplitDuration); ingesterQueryInterval != 0 && needsIngesterSplits {
		// perform splitting using special interval (`split_ingester_queries_by_interval`)
		s.buildMetricSplits(lokiReq.GetStep(), ingesterQueryInterval, start, end, factory)

		// rebound after ingester queries have been split out
		//
		// the end time should now be the boundary of the `query_ingester_within` window, which is "start" currently;
		// but since start is already step-aligned we need to subtract 1ns to align it down by 1 more step so that we
		// get a consistent step between splits
		end, _ = s.alignStartEnd(r.GetStep(), start.Add(-time.Nanosecond), end)
		// we restore the previous start time (the start time of the query)
		start = origStart

		// query only overlaps ingester query window, nothing more to do
		if start.After(end) || start.Equal(end) {
			return reqs, nil
		}

		// copy the splits, reset the results
		ingesterSplits = reqs
		reqs = nil
	} else {
		start = origStart
		end = origEnd
	}

	// perform splitting over the rest of the time range
	s.buildMetricSplits(lokiReq.GetStep(), interval, start, end, factory)

	// move the ingester splits to the end to maintain correct order
	reqs = append(reqs, ingesterSplits...)

	return reqs, nil
}

func (s *metricQuerySplitter) alignStartEnd(step int64, start, end time.Time) (time.Time, time.Time) {
	// step align start and end time of the query. Start time is rounded down and end time is rounded up.
	stepNs := step * 1e6
	startNs := start.UnixNano()

	endNs := end.UnixNano()
	if mod := endNs % stepNs; mod != 0 {
		endNs += stepNs - mod
	}

	return time.Unix(0, startNs-startNs%stepNs), time.Unix(0, endNs)
}

func (s *metricQuerySplitter) buildMetricSplits(step int64, interval time.Duration, start, end time.Time, factory func(start, end time.Time)) {
	for splStart := start; splStart.Before(end); splStart = s.nextIntervalBoundary(splStart, step, interval).Add(time.Duration(step) * time.Millisecond) {
		splEnd := s.nextIntervalBoundary(splStart, step, interval)
		if splEnd.Add(time.Duration(step)*time.Millisecond).After(end) || splEnd.Add(time.Duration(step)*time.Millisecond) == end {
			splEnd = end
		}
		factory(splStart, splEnd)
	}
}

// ingesterQueryBounds determines if we need to split time ranges overlapping the ingester query window (`query_ingesters_within`)
// and retrieve the bounds for those specific splits
func ingesterQueryBounds(execTime time.Time, iqo util.IngesterQueryOptions, req queryrangebase.Request) (time.Time, time.Time, bool) {
	start, end := req.GetStart().UTC(), req.GetEnd().UTC()

	// ingesters are not queried, nothing to do
	if iqo == nil || iqo.QueryStoreOnly() {
		return start, end, false
	}

	windowSize := iqo.QueryIngestersWithin()
	ingesterWindow := execTime.UTC().Add(-windowSize)

	// clamp to the start time
	if ingesterWindow.Before(start) {
		ingesterWindow = start
	}

	// query range does not overlap with ingester query window, nothing to do
	if end.Before(ingesterWindow) {
		return start, end, false
	}

	return ingesterWindow, end, true
}
