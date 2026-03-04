package main

import (
	"context"
	"fmt"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/loghttp"
)

// --------------------------------------------------------
// helpers shared across stream analysis
// --------------------------------------------------------

type parsedStreamResponse struct {
	Streams loghttp.Streams
}

func parseStreamsResponse(raw []byte) (*parsedStreamResponse, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty response body")
	}
	var resp struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Data   struct {
			ResultType string              `json:"resultType"`
			Result     jsoniter.RawMessage `json:"result"`
		} `json:"data"`
	}
	if err := jsoniter.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if resp.Status != "" && resp.Status != "success" {
		return nil, fmt.Errorf("response status %q: %s", resp.Status, resp.Error)
	}
	if resp.Data.ResultType != string(loghttp.ResultTypeStream) {
		return nil, fmt.Errorf("expected streams, got %s", resp.Data.ResultType)
	}
	var streams loghttp.Streams
	if err := jsoniter.Unmarshal(resp.Data.Result, &streams); err != nil {
		return nil, fmt.Errorf("unmarshal streams: %w", err)
	}
	return &parsedStreamResponse{Streams: streams}, nil
}

func streamsByLabels(streams loghttp.Streams) map[string]*loghttp.Stream {
	m := make(map[string]*loghttp.Stream, len(streams))
	for i := range streams {
		m[streams[i].Labels.String()] = &streams[i]
	}
	return m
}

// --------------------------------------------------------
// StreamAnalyzer: comprehensive stream response comparison.
// Detects all types of stream mismatches and returns one
// AnalysisResult per (stream, issue_type) pair.
// --------------------------------------------------------

type StreamAnalyzer struct{}

func (a *StreamAnalyzer) Name() string { return "stream" }

func (a *StreamAnalyzer) CanAnalyze(_ *MismatchEntry, resultType string) bool {
	return resultType == string(loghttp.ResultTypeStream)
}

func (a *StreamAnalyzer) Analyze(_ context.Context, entry *MismatchEntry, cellAResp, cellBResp []byte) ([]*AnalysisResult, error) {
	parsedA, errA := parseStreamsResponse(cellAResp)
	parsedB, errB := parseStreamsResponse(cellBResp)
	if errA != nil || errB != nil {
		return []*AnalysisResult{{
			DetectedMismatchType: "parse_error",
			PredictedCause:       PredictedCauseUnknown,
			Details:              fmt.Sprintf("could not parse responses: cellA=%v, cellB=%v", errA, errB),
		}}, nil
	}

	mapA := streamsByLabels(parsedA.Streams)
	mapB := streamsByLabels(parsedB.Streams)

	var results []*AnalysisResult

	// Missing streams: in A but not B.
	for lbl := range mapA {
		if _, ok := mapB[lbl]; !ok {
			results = append(results, newStreamResult(entry, "stream_missing_in_cell_b", lbl,
				PredictedCauseDataMismatch,
				"stream present in cellA but missing from cellB"))
		}
	}

	// Missing streams: in B but not A.
	for lbl := range mapB {
		if _, ok := mapA[lbl]; !ok {
			results = append(results, newStreamResult(entry, "stream_missing_in_cell_a", lbl,
				PredictedCauseDataMismatch,
				"stream present in cellB but missing from cellA"))
		}
	}

	// Shared streams: compare comprehensively.
	for lbl, sA := range mapA {
		sB, ok := mapB[lbl]
		if !ok {
			continue
		}

		// --- entry count mismatch ---
		if len(sA.Entries) != len(sB.Entries) {
			wider, narrower := sA.Entries, sB.Entries
			if len(sB.Entries) > len(sA.Entries) {
				wider, narrower = sB.Entries, sA.Entries
			}
			divStart, divEnd := findDivergenceRange(wider, narrower)

			detail := fmt.Sprintf("cellA has %d entries, cellB has %d entries", len(sA.Entries), len(sB.Entries))
			if !divStart.IsZero() {
				detail += fmt.Sprintf("; divergence range %s to %s",
					divStart.Format(time.RFC3339Nano), divEnd.Format(time.RFC3339Nano))
			}

			results = append(results, newStreamResult(entry, "stream_entry_count", lbl,
				PredictedCauseDataMismatch, detail))
		}

		// --- line content mismatch ---
		minLen := min(len(sA.Entries), len(sB.Entries))
		for i := 0; i < minLen; i++ {
			if sA.Entries[i].Line != sB.Entries[i].Line {
				ts := sA.Entries[i].Timestamp
				results = append(results, newStreamResult(entry, "stream_line_mismatch", lbl,
					PredictedCauseDataMismatch,
					fmt.Sprintf("line content differs at position %d (timestamp %s)", i, ts.Format(time.RFC3339Nano))))
				break
			}
		}

		// --- timestamp mismatch (same lines but different timestamps) ---
		for i := 0; i < minLen; i++ {
			if sA.Entries[i].Timestamp != sB.Entries[i].Timestamp && sA.Entries[i].Line == sB.Entries[i].Line {
				results = append(results, newStreamResult(entry, "stream_timestamp_mismatch", lbl,
					PredictedCauseDataMismatch,
					fmt.Sprintf("timestamps differ at position %d (cellA=%s, cellB=%s) but lines match",
						i, sA.Entries[i].Timestamp.Format(time.RFC3339Nano), sB.Entries[i].Timestamp.Format(time.RFC3339Nano))))
				break
			}
		}

		// --- structured metadata / parsed labels mismatch ---
		for i := 0; i < minLen; i++ {
			eMeta := !labels.Equal(sA.Entries[i].StructuredMetadata, sB.Entries[i].StructuredMetadata)
			eParsed := !labels.Equal(sA.Entries[i].Parsed, sB.Entries[i].Parsed)
			if eMeta || eParsed {
				mtype := "structured_metadata_mismatch"
				msg := "structured metadata differs"
				if eParsed && !eMeta {
					mtype = "parsed_labels_mismatch"
					msg = "parsed labels differ"
				} else if eParsed && eMeta {
					mtype = "metadata_and_parsed_mismatch"
					msg = "structured metadata and parsed labels differ"
				}

				results = append(results, newStreamResult(entry, mtype, lbl, PredictedCauseDataMismatch,
					fmt.Sprintf("%s at position %d (timestamp %s)", msg, i, sA.Entries[i].Timestamp.Format(time.RFC3339Nano))))
				break
			}
		}
	}

	return results, nil
}

func newStreamResult(entry *MismatchEntry, mtype, item string, cause PredictedCause, details string) *AnalysisResult {
	r := &AnalysisResult{
		DetectedMismatchType: mtype,
		MismatchedItem:       item,
		PredictedCause:       cause,
		Details:              details,
	}
	return r
}

func findDivergenceRange(wider, narrower []loghttp.Entry) (start, end time.Time) {
	narrowerTS := make(map[int64]struct{}, len(narrower))
	for _, e := range narrower {
		narrowerTS[e.Timestamp.UnixNano()] = struct{}{}
	}

	for _, e := range wider {
		if _, ok := narrowerTS[e.Timestamp.UnixNano()]; ok {
			continue
		}
		if start.IsZero() || e.Timestamp.Before(start) {
			start = e.Timestamp
		}
		if e.Timestamp.After(end) {
			end = e.Timestamp
		}
	}
	return start, end
}
