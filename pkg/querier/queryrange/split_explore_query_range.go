package queryrange

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/tenant"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/validation"
	"github.com/pkg/errors"
)

// TODO: need pattern ingester specific options
func SplitExploreQueryRangeMiddleware(limits Limits, iqo util.IngesterQueryOptions, log log.Logger) queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return &splitExploreQueryRange{
			next:   next,
			limits: limits,
			log:    log,
			splitter: exploreSplitter{
				newMetricQuerySplitter(limits, iqo),
				log,
			},
		}
	})
}

type exploreSplitter struct {
	*metricQuerySplitter
	log log.Logger
}

func (s *exploreSplitter) split(execTime time.Time, _ []string, r queryrangebase.Request) ([]queryrangebase.Request, error) {
	var reqs []queryrangebase.Request

	lokiReq := r.(*logproto.QuerySamplesRequest)

	start, end := s.alignStartEnd(r.GetStep(), lokiReq.Start, lokiReq.End)
	lokiReq = lokiReq.WithStartEnd(start, end).(*logproto.QuerySamplesRequest)

	origStart := start
	origEnd := end

	var needsIngesterSplits bool
	start, end, needsIngesterSplits = s.patternIngesterQueryBounds(execTime, s.iqo, lokiReq)
	start, end = s.alignStartEnd(r.GetStep(), start, end)

	if needsIngesterSplits {
		level.Debug(s.log).Log(
			"msg", "executing query to pattern ingesters for recent data",
			"start", start,
			"end", end,
			"query", lokiReq.Query,
			"step", lokiReq.Step,
		)
		reqs = append(reqs, &logproto.QuerySamplesRequest{
			Query: lokiReq.Query,
			Start: start,
			End:   end,
			Step:  lokiReq.Step,
		})

		// start of the ingester window is now the end of the traditional query
		end, _ = s.alignStartEnd(r.GetStep(), start.Add(-time.Nanosecond), end)

		// we restore the previous start time (the start time of the query)
		start = origStart

		// query only overlaps ingester query window, nothing more to do
		if start.After(end) || start.Equal(end) {
			return reqs, nil
		}
	} else {
		start = origStart
		end = origEnd
	}

	level.Debug(s.log).Log(
		"msg", "executing query to traditional query range endpoint for older data",
		"start", start,
		"end", end,
		"query", lokiReq.Query,
		"step", lokiReq.Step,
	)
	reqs = append(reqs, &LokiRequest{
		Query:     lokiReq.Query,
		Step:      lokiReq.Step,
		Direction: logproto.BACKWARD,
		StartTs:   start,
		EndTs:     end,
	})

	return reqs, nil
}

func (s *exploreSplitter) patternIngesterQueryBounds(execTime time.Time, iqo util.IngesterQueryOptions, req queryrangebase.Request) (time.Time, time.Time, bool) {
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

type splitExploreQueryRange struct {
	next     queryrangebase.Handler
	limits   Limits
	splitter exploreSplitter
	log      log.Logger
}

func shouldSplit(query string) bool {
	expr, err := syntax.ParseSampleExpr(query)
	if err != nil {
		return false
	}

	var selector syntax.LogSelectorExpr
	switch e := expr.(type) {
	case *syntax.VectorAggregationExpr:
		selector, err = e.Selector()
	case *syntax.RangeAggregationExpr:
		selector, err = e.Selector()
	default:
		return false
	}

	if err != nil {
		return false
	}

	if selector == nil || selector.HasFilter() {
		return false
	}

	return true
}

func (h *splitExploreQueryRange) Do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	if !shouldSplit(r.GetQuery()) {
		return h.next.Do(ctx, r)
	}

	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	splits, err := h.splitter.split(time.Now().UTC(), tenantIDs, r)
  if err != nil {
    return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
  }

	input := make([]*lokiResult, 0, len(splits))
	for _, interval := range splits {
		input = append(input, &lokiResult{
			req: interval,
			ch:  make(chan *packedResp),
		})
	}

	maxSeriesCapture := func(id string) int { return h.limits.MaxQuerySeries(ctx, id) }
	maxSeries := validation.SmallestPositiveIntPerTenant(tenantIDs, maxSeriesCapture)
	resps, err := h.Process(ctx, input, maxSeries)
	if err != nil {
		return nil, err
	}
	return mergeResponses(h.log, resps...)
}

func mergeResponses(
	log log.Logger,
	responses ...queryrangebase.Response,
) (queryrangebase.Response, error) {
	level.Debug(log).
		Log("msg", "merging explore query range responses", "responses", len(responses))

	if len(responses) == 0 {
		return nil, errors.New("merging responses requires at least one response")
	}

	if len(responses) == 1 {
		return responses[0], nil
	}

	output := map[string]logproto.Series{}

	var headers []queryrangebase.PrometheusResponseHeader
	for _, r := range responses {
		switch res := r.(type) {
		case *LokiPromResponse:
			level.Debug(log).Log(
				"msg", "merging explore query range response",
				"type", fmt.Sprintf("%T", res),
				"results", len(res.Response.Data.Result),
			)
			if len(headers) == 0 {
				headers = []queryrangebase.PrometheusResponseHeader{}
				for _, header := range res.Response.Headers {
					headers = append(headers, *header)
				}
			}

			for _, stream := range res.Response.Data.Result {
				lbls := logproto.FromLabelAdaptersToLabels(stream.Labels)
				metric := lbls.String()
				existing, ok := output[metric]
				if !ok {
					existing = logproto.Series{
						Labels:     metric,
						StreamHash: lbls.Hash(),
						Samples:    []logproto.Sample{},
					}
				}

				// We need to make sure we don't repeat samples. This causes some visualisations to be broken in Grafana.
				// The prometheus API is inclusive of start and end timestamps.
				if len(existing.Samples) > 0 && len(stream.Samples) > 0 {
					existingEndTs := existing.Samples[len(existing.Samples)-1].Timestamp
					if existingEndTs == (stream.Samples[0].TimestampMs * 1e6) {
						// Typically this the cases where only 1 sample point overlap,
						// so optimize with simple code.
						stream.Samples = stream.Samples[1:]
					} else if existingEndTs > (stream.Samples[0].TimestampMs * 1e6) {
						// Overlap might be big, use heavier algorithm to remove overlap.
						stream.Samples = sliceSamples(stream.Samples, existingEndTs)
					} // else there is no overlap, yay!
				}
				existing.Samples = append(existing.Samples, convertSamples(stream.Samples)...)
				output[metric] = existing
			}
		case *QuerySamplesResponse:
			level.Debug(log).Log(
				"msg", "merging explore query range response",
				"type", fmt.Sprintf("%T", res),
				"results", len(res.Response.Series),
			)

			if len(headers) == 0 {
				headers = res.Headers
			}

			for _, series := range res.Response.Series {
				metric := series.Labels
				existing, ok := output[metric]
				if !ok {
					existing = logproto.Series{
						Labels:     series.Labels,
						StreamHash: series.StreamHash,
						Samples:    []logproto.Sample{},
					}
				}

				// We need to make sure we don't repeat samples. This causes some visualisations to be broken in Grafana.
				// The prometheus API is inclusive of start and end timestamps.
				if len(existing.Samples) > 0 && len(series.Samples) > 0 {
					existingEndTs := existing.Samples[len(existing.Samples)-1].Timestamp
					if existingEndTs == series.Samples[0].Timestamp {
						// Typically this the cases where only 1 sample point overlap,
						// so optimize with simple code.
						series.Samples = series.Samples[1:]
					} else if existingEndTs > series.Samples[0].Timestamp {
						// Overlap might be big, use heavier algorithm to remove overlap.
						series.Samples = sliceSeriesSamples(series.Samples, existingEndTs)
					} // else there is no overlap, yay!
				}
				existing.Samples = append(existing.Samples, series.Samples...)
				output[metric] = existing
			}
		default:
			return nil, fmt.Errorf("unsupported response type %t", res)
		}
	}

	series := make([]logproto.Series, 0, len(output))
	for _, s := range output {
		series = append(series, s)
	}

	return &QuerySamplesResponse{
		Response: &logproto.QuerySamplesResponse{
			Series: series,
			// TODO: merge stats and warnings
			// Stats:    stats.Ingester{},
			// Warnings: []string{},
		},
		Headers: headers,
	}, nil
}

// sliceSamples assumes given samples are sorted by timestamp in ascending order and
// return a sub slice whose first element's is the smallest timestamp that is strictly
// bigger than the given minTs. Empty slice is returned if minTs is bigger than all the
// timestamps in samples.
func sliceSamples(samples []logproto.LegacySample, minTs int64) []logproto.LegacySample {
	if len(samples) <= 0 || minTs < samples[0].TimestampMs {
		return samples
	}

	if len(samples) > 0 && minTs > samples[len(samples)-1].TimestampMs {
		return samples[len(samples):]
	}

	searchResult := sort.Search(len(samples), func(i int) bool {
		return samples[i].TimestampMs > minTs
	})

	return samples[searchResult:]
}

// sliceSeriesSamples assumes given samples are sorted by timestamp in ascending order and
// return a sub slice whose first element's is the smallest timestamp that is strictly
// bigger than the given minTs. Empty slice is returned if minTs is bigger than all the
// timestamps in samples.
func sliceSeriesSamples(samples []logproto.Sample, minTs int64) []logproto.Sample {
	if len(samples) <= 0 || minTs < samples[0].Timestamp {
		return samples
	}

	if len(samples) > 0 && minTs > samples[len(samples)-1].Timestamp {
		return samples[len(samples):]
	}

	searchResult := sort.Search(len(samples), func(i int) bool {
		return samples[i].Timestamp > minTs
	})

	return samples[searchResult:]
}

func convertSamples(samples []logproto.LegacySample) []logproto.Sample {
	out := make([]logproto.Sample, 0, len(samples))
	for _, s := range samples {
		out = append(out, logproto.Sample{
			Timestamp: s.TimestampMs * 1e6, // convert milliseconds to nanoseconds
			Value:     s.Value,
		})
	}
	return out
}

func (h *splitExploreQueryRange) Process(
	ctx context.Context,
	input []*lokiResult,
	maxSeries int,
) ([]queryrangebase.Response, error) {
	var responses []queryrangebase.Response
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(errors.New("split explore metric queries process canceled"))

	ch := h.Feed(ctx, input)

	// per request wrapped handler for limiting the amount of series.
	next := newSeriesLimiter(maxSeries).Wrap(h.next)
	for i := 0; i < len(input); i++ {
		go h.loop(ctx, ch, next)
	}

	for _, x := range input {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case data := <-x.ch:
			if data.err != nil {
				return nil, data.err
			}

			responses = append(responses, data.resp)
		}
	}

	return responses, nil
}

func (h *splitExploreQueryRange) Feed(ctx context.Context, input []*lokiResult) chan *lokiResult {
	ch := make(chan *lokiResult)

	go func() {
		defer close(ch)
		for _, d := range input {
			select {
			case <-ctx.Done():
				return
			case ch <- d:
				continue
			}
		}
	}()

	return ch
}

func (h *splitExploreQueryRange) loop(ctx context.Context, ch <-chan *lokiResult, next queryrangebase.Handler) {
	for data := range ch {

		resp, err := next.Do(ctx, data.req)

		select {
		case <-ctx.Done():
			return
		case data.ch <- &packedResp{resp, err}:
			// The parent Process method will return on the first error. So stop
			// processng.
			if err != nil {
				return
			}
		}
	}
}
