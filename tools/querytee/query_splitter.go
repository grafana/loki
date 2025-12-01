package querytee

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	jsoniter "github.com/json-iterator/go"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/prometheus/common/model"
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
func concatenateResponses(oldResponse, recentResponse *BackendResponse, direction logproto.Direction) (*BackendResponse, error) {
	if oldResponse == nil || recentResponse == nil {
		return nil, fmt.Errorf("cannot concatenate nil responses")
	}

	var oldResp, recentResp loghttp.QueryResponse

	if err := oldResp.UnmarshalJSON(oldResponse.body); err != nil {
		return nil, fmt.Errorf("failed to unmarshal old response: %w", err)
	}

	if err := recentResp.UnmarshalJSON(recentResponse.body); err != nil {
		return nil, fmt.Errorf("failed to unmarshal recent response: %w", err)
	}

	// Check if both responses are successful
	if oldResp.Status != "success" {
		return nil, fmt.Errorf("old response has non-success status: %s", oldResp.Status)
	}
	if recentResp.Status != "success" {
		return nil, fmt.Errorf("recent response has non-success status: %s", recentResp.Status)
	}

	if oldResp.Data.ResultType != recentResp.Data.ResultType {
		return nil, fmt.Errorf("old and recent responses have different result types: %s vs %s (respectively)", oldResp.Data.ResultType, recentResp.Data.ResultType)
	}

	var concatenated loghttp.ResultValue
	var resultType loghttp.ResultType

	switch oldResp.Data.ResultType {
	case loghttp.ResultTypeMatrix:
		oldMatrix, ok := oldResp.Data.Result.(loghttp.Matrix)
		if !ok {
			return nil, fmt.Errorf("failed to cast old result to Matrix")
		}

		recentMatrix, ok := recentResp.Data.Result.(loghttp.Matrix)
		if !ok {
			return nil, fmt.Errorf("failed to cast recent result to Matrix")
		}

		concatenated = concatenateMatrices(oldMatrix, recentMatrix)
		resultType = loghttp.ResultTypeMatrix

	case loghttp.ResultTypeStream:
		oldStreams, ok := oldResp.Data.Result.(loghttp.Streams)
		if !ok {
			return nil, fmt.Errorf("failed to cast old result to Streams")
		}

		recentStreams, ok := recentResp.Data.Result.(loghttp.Streams)
		if !ok {
			return nil, fmt.Errorf("failed to cast recent result to Streams")
		}

		concatenated = concatenateStreams(oldStreams, recentStreams, direction)
		resultType = loghttp.ResultTypeStream

	default:
		return nil, fmt.Errorf("unsupported result type: %s", oldResp.Data.ResultType)
	}

	// Merge statistics
	mergedStats := oldResp.Data.Statistics
	mergedStats.MergeSplit(recentResp.Data.Statistics)

	// Merge warnings (deduplicate)
	warningsMap := make(map[string]struct{})
	for _, w := range oldResp.Warnings {
		warningsMap[w] = struct{}{}
	}
	for _, w := range recentResp.Warnings {
		warningsMap[w] = struct{}{}
	}

	warnings := make([]string, 0, len(warningsMap))
	for w := range warningsMap {
		warnings = append(warnings, w)
	}
	sort.Strings(warnings)

	// Build the concatenated response
	result := loghttp.QueryResponse{
		Status: "success",
		Data: loghttp.QueryResponseData{
			ResultType: resultType,
			Result:     concatenated,
			Statistics: mergedStats,
		},
		Warnings: warnings,
	}

	concatenatedBody, err := jsoniter.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal concatenated response: %w", err)
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
func concatenateMatrices(oldMatrix, recentMatrix loghttp.Matrix) loghttp.Matrix {
	// Create a map of metrics by their string representation
	metricMap := make(map[string]*model.SampleStream)

	// Add all old matrix streams
	for i := range oldMatrix {
		stream := oldMatrix[i]
		key := stream.Metric.String()
		metricMap[key] = &model.SampleStream{
			Metric: stream.Metric,
			//TODO: can I simplify this instead of using an append?
			Values: append([]model.SamplePair{}, stream.Values...),
		}
	}

	// Concatenate with recent matrix streams
	for i := range recentMatrix {
		stream := recentMatrix[i]
		key := stream.Metric.String()

		if existing, ok := metricMap[key]; ok {
			// Metric exists in both - concatenate values
			existing.Values = append(existing.Values, stream.Values...)
		} else {
			// Metric only in recent - add it
			metricMap[key] = &model.SampleStream{
				Metric: stream.Metric,
				Values: append([]model.SamplePair{}, stream.Values...),
			}
		}
	}

	// Convert map back to slice and sort by metric labels for consistent output
	//TODO: is there a more modern way to get the keys from a map?
	keys := make([]string, 0, len(metricMap))
	for key := range metricMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make(loghttp.Matrix, 0, len(metricMap))
	for _, key := range keys {
		result = append(result, *metricMap[key])
	}

	return result
}

// concatenateStreams concatenates two stream results by merging entries for each unique label set.
// The direction parameter determines the order of entry concatenation:
// - FORWARD: old entries + recent entries (oldest first)
// - BACKWARD: recent entries + old entries (newest first)
func concatenateStreams(oldStreams, recentStreams loghttp.Streams, direction logproto.Direction) loghttp.Streams {
	streamMap := make(map[string]*loghttp.Stream)

	for i := range oldStreams {
		stream := oldStreams[i]
		key := stream.Labels.String()
		streamMap[key] = &loghttp.Stream{
			Labels:  stream.Labels,
			Entries: append([]loghttp.Entry{}, stream.Entries...),
		}
	}

	for i := range recentStreams {
		stream := recentStreams[i]
		key := stream.Labels.String()

		if existing, ok := streamMap[key]; ok {
			if direction == logproto.FORWARD {
				existing.Entries = append(existing.Entries, stream.Entries...)
			} else {
				existing.Entries = append(stream.Entries, existing.Entries...)
			}
		} else {
			streamMap[key] = &loghttp.Stream{
				Labels:  stream.Labels,
				Entries: append([]loghttp.Entry{}, stream.Entries...),
			}
		}
	}

	// Convert map back to slice and sort by label string for consistent output
	keys := make([]string, 0, len(streamMap))
	for key := range streamMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make(loghttp.Streams, 0, len(streamMap))
	for _, key := range keys {
		result = append(result, *streamMap[key])
	}

	return result
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
