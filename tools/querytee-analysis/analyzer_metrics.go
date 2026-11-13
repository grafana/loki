package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/querytee/comparator"
)

// --------------------------------------------------------
// helpers shared across metric analysis
// --------------------------------------------------------

type parsedMetricResponse struct {
	ResultType string
	Matrix     model.Matrix
	Vector     model.Vector
	Scalar     *model.Scalar
}

func parseMetricResponse(raw []byte) (*parsedMetricResponse, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty response body")
	}
	var resp comparator.SamplesResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if resp.Status != "" && resp.Status != statusSuccess {
		return nil, fmt.Errorf("response status %q", resp.Status)
	}
	parsed := &parsedMetricResponse{ResultType: resp.Data.ResultType}

	switch resp.Data.ResultType {
	case "matrix":
		if err := json.Unmarshal(resp.Data.Result, &parsed.Matrix); err != nil {
			return nil, fmt.Errorf("unmarshal matrix: %w", err)
		}
	case "vector":
		if err := json.Unmarshal(resp.Data.Result, &parsed.Vector); err != nil {
			return nil, fmt.Errorf("unmarshal vector: %w", err)
		}
	case "scalar":
		var s model.Scalar
		if err := json.Unmarshal(resp.Data.Result, &s); err != nil {
			return nil, fmt.Errorf("unmarshal scalar: %w", err)
		}
		parsed.Scalar = &s
	default:
		return nil, fmt.Errorf("unsupported metric type: %s", resp.Data.ResultType)
	}
	return parsed, nil
}

func metricLabelsToMap(m model.Metric) map[string]string {
	result := make(map[string]string, len(m))
	for k, v := range m {
		if string(k) == "__name__" {
			continue
		}
		result[string(k)] = string(v)
	}
	return result
}

// --------------------------------------------------------
// MetricAnalyzer: comprehensive metric response comparison.
// Detects all types of metric mismatches and returns one
// AnalysisResult per (metric, issue_type) pair.
// --------------------------------------------------------

type MetricAnalyzer struct{}

func (a *MetricAnalyzer) Name() string { return "metric" }

func (a *MetricAnalyzer) CanAnalyze(_ *MismatchEntry, resultType string) bool {
	return resultType == "matrix" || resultType == "vector" || resultType == "scalar"
}

func (a *MetricAnalyzer) Analyze(_ context.Context, entry *MismatchEntry, cellAResp, cellBResp []byte) ([]*AnalysisResult, error) {
	parsedA, errA := parseMetricResponse(cellAResp)
	parsedB, errB := parseMetricResponse(cellBResp)
	if errA != nil || errB != nil {
		return []*AnalysisResult{{
			DetectedMismatchType: "parse_error",
			PredictedCause:       PredictedCauseUnknown,
			Details:              fmt.Sprintf("could not parse responses: cellA=%v, cellB=%v", errA, errB),
		}}, nil
	}

	if parsedA.ResultType != parsedB.ResultType {
		return nil, nil
	}

	var results []*AnalysisResult

	switch parsedA.ResultType {
	case "matrix":
		results = analyzeMatrix(entry, parsedA, parsedB)
	case "vector":
		results = analyzeVector(entry, parsedA, parsedB)
	case "scalar":
		results = analyzeScalar(entry, parsedA, parsedB)
	}

	return results, nil
}

func analyzeMatrix(_ *MismatchEntry, parsedA, parsedB *parsedMetricResponse) []*AnalysisResult {
	fpToA := matrixByFingerprint(parsedA.Matrix)
	fpToB := matrixByFingerprint(parsedB.Matrix)

	var results []*AnalysisResult

	for fp, s := range fpToA {
		if _, ok := fpToB[fp]; !ok {
			results = append(results, newMetricResult("metric_missing_in_cell_b", s.Metric.String(), PredictedCauseDataMismatch, fmt.Sprintf("metric present in cellA (%d values) but missing from cellB", len(s.Values))))
		}
	}
	for fp, s := range fpToB {
		if _, ok := fpToA[fp]; !ok {
			results = append(results, newMetricResult("metric_missing_in_cell_a", s.Metric.String(), PredictedCauseDataMismatch, fmt.Sprintf("metric present in cellB (%d values) but missing from cellA", len(s.Values))))
		}
	}

	for fp, sA := range fpToA {
		sB, ok := fpToB[fp]
		if !ok {
			continue
		}

		if len(sA.Values) != len(sB.Values) {
			results = append(results, newMetricResult("sample_count_mismatch", sA.Metric.String(), PredictedCauseDataMismatch, fmt.Sprintf("cellA has %d samples, cellB has %d samples", len(sA.Values), len(sB.Values))))
		}

		minLen := min(len(sA.Values), len(sB.Values))
		for i := 0; i < minLen; i++ {
			vA := float64(sA.Values[i].Value)
			vB := float64(sB.Values[i].Value)
			delta := math.Abs(vA - vB)
			tsDiff := sA.Values[i].Timestamp != sB.Values[i].Timestamp

			if delta > 0 || tsDiff {
				detail := fmt.Sprintf("value delta=%.6g at timestamp %s",
					delta, sA.Values[i].Timestamp.Time().Format("2006-01-02T15:04:05Z"))
				if tsDiff {
					detail = fmt.Sprintf("timestamps differ (cellA=%s, cellB=%s), value delta=%.6g",
						sA.Values[i].Timestamp.Time().Format("2006-01-02T15:04:05Z"),
						sB.Values[i].Timestamp.Time().Format("2006-01-02T15:04:05Z"), delta)
				}
				results = append(results, newMetricResult("sample_value_mismatch", sA.Metric.String(), PredictedCauseDataMismatch, detail))
				break
			}
		}
	}

	return results
}

func analyzeVector(_ *MismatchEntry, parsedA, parsedB *parsedMetricResponse) []*AnalysisResult {
	fpToA := vectorByFingerprint(parsedA.Vector)
	fpToB := vectorByFingerprint(parsedB.Vector)

	var results []*AnalysisResult

	for fp, s := range fpToA {
		if _, ok := fpToB[fp]; !ok {
			results = append(results, newMetricResult("metric_missing_in_cell_b", s.Metric.String(), PredictedCauseDataMismatch, "metric present in cellA but missing from cellB"))
		}
	}
	for fp, s := range fpToB {
		if _, ok := fpToA[fp]; !ok {
			results = append(results, newMetricResult("metric_missing_in_cell_a", s.Metric.String(), PredictedCauseDataMismatch, "metric present in cellB but missing from cellA"))
		}
	}

	for fp, sampleA := range fpToA {
		sampleB, ok := fpToB[fp]
		if !ok {
			continue
		}

		delta := math.Abs(float64(sampleA.Value) - float64(sampleB.Value))
		tsDiff := sampleA.Timestamp != sampleB.Timestamp

		if delta > 0 || tsDiff {
			detail := fmt.Sprintf("value delta=%.6g", delta)
			if tsDiff {
				detail = fmt.Sprintf("timestamps differ (cellA=%s, cellB=%s), value delta=%.6g",
					sampleA.Timestamp.Time().Format("2006-01-02T15:04:05Z"),
					sampleB.Timestamp.Time().Format("2006-01-02T15:04:05Z"), delta)
			}
			results = append(results, newMetricResult("sample_value_mismatch", sampleA.Metric.String(), PredictedCauseDataMismatch, detail))
		}
	}

	return results
}

func analyzeScalar(_ *MismatchEntry, parsedA, parsedB *parsedMetricResponse) []*AnalysisResult {
	if parsedA.Scalar == nil || parsedB.Scalar == nil {
		return nil
	}

	delta := math.Abs(float64(parsedA.Scalar.Value) - float64(parsedB.Scalar.Value))
	tsDiff := parsedA.Scalar.Timestamp != parsedB.Scalar.Timestamp

	if delta == 0 && !tsDiff {
		return nil
	}

	detail := fmt.Sprintf("scalar value delta=%.6g", delta)
	if tsDiff {
		detail = fmt.Sprintf("scalar timestamps differ, value delta=%.6g", delta)
	}

	return []*AnalysisResult{newMetricResult("scalar_value_mismatch", "scalar", PredictedCauseDataMismatch, detail)}
}

func newMetricResult(mtype, item string, cause PredictedCause, details string) *AnalysisResult {
	r := &AnalysisResult{
		DetectedMismatchType: mtype,
		MismatchedItem:       item,
		PredictedCause:       cause,
		Details:              details,
	}
	return r
}

// --------------------------------------------------------
// metric helpers
// --------------------------------------------------------

func matrixByFingerprint(m model.Matrix) map[model.Fingerprint]*model.SampleStream {
	result := make(map[model.Fingerprint]*model.SampleStream, len(m))
	for i := range m {
		result[m[i].Metric.Fingerprint()] = m[i]
	}
	return result
}

func vectorByFingerprint(v model.Vector) map[model.Fingerprint]*model.Sample {
	result := make(map[model.Fingerprint]*model.Sample, len(v))
	for i := range v {
		result[v[i].Metric.Fingerprint()] = v[i]
	}
	return result
}
