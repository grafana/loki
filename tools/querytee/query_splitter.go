package querytee

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

// isQueryEntirelyRecent checks if the entire query is for recent data (newer than the threshold).
// If true, the query should not be sent to goldfish at all.
func isQueryEntirelyRecent(r *http.Request, minAge time.Duration) bool {
	if minAge == 0 || r.URL.Path != "/loki/api/v1/query_range" {
		return false
	}

	if err := r.ParseForm(); err != nil {
		return false
	}

	rangeQuery, err := loghttp.ParseRangeQuery(r)
	if err != nil {
		return false
	}

	now := time.Now()
	threshold := now.Add(-minAge)

	// If the query start is at or after the threshold, entire query is recent
	return rangeQuery.Start.After(threshold) || rangeQuery.Start.Equal(threshold)
}

// shouldSplitQuery determines if a query_range query needs to be split based on the minimum age threshold.
// Returns whether splitting is needed, the aligned split point, and the step duration.
// If needsSplit is false, the query should be handled normally (either all recent or all old).
func shouldSplitQuery(r *http.Request, minAge time.Duration) (needsSplit bool, splitPoint time.Time, step time.Duration, err error) {
	if minAge == 0 {
		return false, time.Time{}, 0, nil
	}

	if r.URL.Path != "/loki/api/v1/query_range" {
		return false, time.Time{}, 0, nil
	}

	if err := r.ParseForm(); err != nil {
		return false, time.Time{}, 0, fmt.Errorf("failed to parse form: %w", err)
	}

	rangeQuery, err := loghttp.ParseRangeQuery(r)
	if err != nil {
		return false, time.Time{}, 0, fmt.Errorf("failed to parse range query: %w", err)
	}

	now := time.Now()
	threshold := now.Add(-minAge)

	if rangeQuery.Start.After(threshold) || rangeQuery.Start.Equal(threshold) {
		return false, time.Time{}, 0, nil
	}

	if rangeQuery.End.Before(threshold) || rangeQuery.End.Equal(threshold) {
		return false, time.Time{}, 0, nil
	}

	alignedSplit := alignToStep(threshold, rangeQuery.Start, rangeQuery.Step)
	return true, alignedSplit, rangeQuery.Step, nil
}

// alignToStep aligns a split time to the nearest step boundary at or before the split time.
// This ensures the split point falls exactly on a timestamp where Loki would evaluate the query.
func alignToStep(splitTime time.Time, queryStart time.Time, step time.Duration) time.Time {
	if step <= 0 {
		return splitTime
	}

	if splitTime.Before(queryStart) {
		return queryStart
	}

	elapsed := splitTime.Sub(queryStart)
	numSteps := elapsed / step

	alignedSplit := queryStart.Add(numSteps * step)
	return alignedSplit
}

// createSplitRequests creates two sub-requests from the original request:
// - oldQuery: from start to splitPoint (inclusive)
// - recentQuery: from (splitPoint + step) to end (no overlap)
// Both requests preserve all other parameters from the original.
func createSplitRequests(original *http.Request, splitPoint time.Time, step time.Duration) (oldQuery, recentQuery *http.Request, err error) {
	// Clone the original request twice
	oldQuery, err = cloneRequest(original)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to clone request for old query: %w", err)
	}

	recentQuery, err = cloneRequest(original)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to clone request for recent query: %w", err)
	}

	// Parse original query parameters
	oldQueryParams := oldQuery.URL.Query()
	recentQueryParams := recentQuery.URL.Query()

	endStr := oldQueryParams.Get("end")

	end, err := loghttp.ParseTimestamp(endStr, time.Now())
	if err != nil {
		return nil, nil, fmt.Errorf("invalid end time: %w", err)
	}

	// Old query: start to splitPoint
	oldQueryParams.Set("end", formatTime(splitPoint))
	oldQuery.URL.RawQuery = oldQueryParams.Encode()

	// Recent query: splitPoint + step to end (no overlap)
	recentStart := splitPoint.Add(step)
	recentQueryParams.Set("start", formatTime(recentStart))
	recentQueryParams.Set("end", formatTime(end))
	recentQuery.URL.RawQuery = recentQueryParams.Encode()

	return oldQuery, recentQuery, nil
}

// concatenateResponses concatenates two query_range responses.
// Since the queries are split at step boundaries with no overlap, we can simply
// concatenate the values arrays for each metric or entries for each stream.
// The direction parameter is used for stream concatenation to preserve correct entry order.
// Returns a new BackendResponse with the concatenated body and combined metadata.
func concatenateResponses(oldResponse, recentResponse *BackendResponse, r *http.Request) (*BackendResponse, error) {
	if oldResponse == nil || recentResponse == nil {
		return nil, fmt.Errorf("cannot concatenate nil responses")
	}

	direction := extractDirection(r)
	var oldResp, recentResp queryrangebase.Response
	codec := queryrange.DefaultCodec
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := codec.DecodeRequest(ctx, r, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decode request: %w", err)
	}

	// Check if both responses are successful
	if !oldResponse.succeeded() {
		return nil, fmt.Errorf("old response has non-success status: %d", oldResponse.status)
	}
	if !recentResponse.succeeded() {
		return nil, fmt.Errorf("recent response has non-success status: %d", recentResponse.status)
	}

	oldResp, err = queryrange.DecodeResponseJSONFrom(oldResponse.body, req, r.Header)
	if err != nil {
		return nil, fmt.Errorf("failed to decode old response: %w", err)
	}
	recentResp, err = queryrange.DecodeResponseJSONFrom(recentResponse.body, req, r.Header)
	if err != nil {
		return nil, fmt.Errorf("failed to decode recent response: %w", err)
	}

	var mergedStats stats.Result
	var warnings []string

	switch old := oldResp.(type) {
	case *queryrange.LokiPromResponse:
		mergedStats = old.Statistics
		recentTyped, ok := recentResp.(*queryrange.LokiPromResponse)
		if !ok {
			return nil, fmt.Errorf("failed to cast recent response to LokiPromResponse")
		}
		mergedStats.MergeSplit(recentTyped.Statistics)

	case *queryrange.LokiResponse:
		mergedStats = old.Statistics
		recentTyped, ok := recentResp.(*queryrange.LokiResponse)
		if !ok {
			return nil, fmt.Errorf("failed to cast recent response to LokiResponse")
		}
		mergedStats.MergeSplit(recentTyped.Statistics)

		// Merge and deduplicate warnings
		warningsMap := make(map[string]struct{})
		for _, w := range old.Warnings {
			warningsMap[w] = struct{}{}
		}
		for _, w := range recentTyped.Warnings {
			warningsMap[w] = struct{}{}
		}
		warnings = make([]string, 0, len(warningsMap))
		for w := range warningsMap {
			warnings = append(warnings, w)
		}
		sort.Strings(warnings)
	}

	var result queryrangebase.Response

	switch old := oldResp.(type) {
	case *queryrange.LokiPromResponse:
		oldMatrix := old.Response.Data.Result
		new, ok := recentResp.(*queryrange.LokiPromResponse)
		if !ok {
			return nil, fmt.Errorf("failed to cast recent response to LokiPromResponse")
		}
		recentMatrix := new.Response.Data.Result
		result = concatenateMatrices(oldMatrix, recentMatrix, mergedStats)

	case *queryrange.LokiResponse:
		oldStreams := old.Data.Result
		new, ok := recentResp.(*queryrange.LokiResponse)
		if !ok {
			return nil, fmt.Errorf("failed to cast recent response to LokiResponse")
		}
		recentStreams := new.Data.Result
		result = concatenateStreams(oldStreams, recentStreams, direction, mergedStats, warnings)
	default:
		return nil, fmt.Errorf("unsupported response type: %T", oldResp)
	}

	httpResp, err := codec.EncodeResponse(ctx, r, result)
	if err != nil {
		return nil, fmt.Errorf("failed to encode concatenated response: %w", err)
	}
	defer httpResp.Body.Close()

	concatenatedBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read encoded response body: %w", err)
	}

	// Create a new BackendResponse with concatenated data
	// Use the old response's metadata (backend, status, traceID, spanID) and combine durations
	return &BackendResponse{
		backend:  oldResponse.backend,
		status:   oldResponse.status,
		body:     concatenatedBody,
		err:      nil,
		duration: oldResponse.duration + recentResponse.duration,
		traceID:  oldResponse.traceID, // Keep old response's trace ID as it was the initiating request
		spanID:   oldResponse.spanID,
	}, nil
}

// concatenateMatrices concatenates two matrices by merging samples for each unique metric.
// Since there's no overlap by design, we simply append the samples.
// Returns a LokiPromResponse containing the merged matrix data.
func concatenateMatrices(oldMatrix, recentMatrix []queryrangebase.SampleStream, mergedStats stats.Result) queryrangebase.Response {
	// Create a map of metrics by their string representation
	metricMap := make(map[string]*queryrangebase.SampleStream)

	// Add all old matrix streams
	for i := range oldMatrix {
		stream := oldMatrix[i]
		key := logproto.FromLabelAdaptersToLabels(stream.Labels).String()
		metricMap[key] = &queryrangebase.SampleStream{
			Labels:  stream.Labels,
			Samples: append([]logproto.LegacySample{}, stream.Samples...),
		}
	}

	// Concatenate with recent matrix streams
	for i := range recentMatrix {
		stream := recentMatrix[i]
		key := logproto.FromLabelAdaptersToLabels(stream.Labels).String()

		if existing, ok := metricMap[key]; ok {
			// Metric exists in both - concatenate samples
			existing.Samples = append(existing.Samples, stream.Samples...)
		} else {
			// Metric only in recent - add it
			metricMap[key] = &queryrangebase.SampleStream{
				Labels:  stream.Labels,
				Samples: append([]logproto.LegacySample{}, stream.Samples...),
			}
		}
	}

	// Convert map back to slice and sort by metric labels for consistent output
	keys := make([]string, 0, len(metricMap))
	for key := range metricMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]queryrangebase.SampleStream, 0, len(metricMap))
	for _, key := range keys {
		result = append(result, *metricMap[key])
	}

	return &queryrange.LokiPromResponse{
		Response: &queryrangebase.PrometheusResponse{
			Status: "success",
			Data: queryrangebase.PrometheusData{
				ResultType: loghttp.ResultTypeMatrix,
				Result:     result,
			},
		},
		Statistics: mergedStats,
	}
}

// concatenateStreams concatenates two stream results by merging entries for each unique label set.
// The direction parameter determines the order of entry concatenation:
// - FORWARD: old entries + recent entries (oldest first)
// - BACKWARD: recent entries + old entries (newest first)
// Returns a LokiResponse containing the merged stream data.
func concatenateStreams(oldStreams, recentStreams []logproto.Stream, direction logproto.Direction, mergedStats stats.Result, warnings []string) queryrangebase.Response {
	streamMap := make(map[string]*logproto.Stream)

	for i := range oldStreams {
		stream := oldStreams[i]
		key := stream.Labels
		streamMap[key] = &logproto.Stream{
			Labels:  stream.Labels,
			Entries: append([]logproto.Entry{}, stream.Entries...),
		}
	}

	for i := range recentStreams {
		stream := recentStreams[i]
		key := stream.Labels

		if existing, ok := streamMap[key]; ok {
			if direction == logproto.FORWARD {
				existing.Entries = append(existing.Entries, stream.Entries...)
			} else {
				existing.Entries = append(stream.Entries, existing.Entries...)
			}
		} else {
			streamMap[key] = &logproto.Stream{
				Labels:  stream.Labels,
				Entries: append([]logproto.Entry{}, stream.Entries...),
			}
		}
	}

	// Convert map back to slice and sort by label string for consistent output
	keys := make([]string, 0, len(streamMap))
	for key := range streamMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]logproto.Stream, 0, len(streamMap))
	for _, key := range keys {
		result = append(result, *streamMap[key])
	}

	return &queryrange.LokiResponse{
		Status:     "success",
		Data:       queryrange.LokiData{ResultType: loghttp.ResultTypeStream, Result: result},
		Statistics: mergedStats,
		Warnings:   warnings,
		Direction:  direction,
	}
}

// cloneRequest creates a deep copy of an HTTP request.
func cloneRequest(r *http.Request) (*http.Request, error) {
	clone := r.Clone(r.Context())

	// Clone the body if present
	if r.Body != nil {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		// Restore original body
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		// Set cloned body
		clone.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	return clone, nil
}

// formatTime formats a time as a nanosecond Unix timestamp (Loki format).
func formatTime(t time.Time) string {
	return strconv.FormatInt(t.UnixNano(), 10)
}
