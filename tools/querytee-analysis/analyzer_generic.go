package main

import (
	"context"
	"encoding/json"
	"fmt"
)

// --------------------------------------------------------
// StatusMismatchAnalyzer
// --------------------------------------------------------

type StatusMismatchAnalyzer struct{}

func (a *StatusMismatchAnalyzer) Name() string { return "status_mismatch" }

func (a *StatusMismatchAnalyzer) CanAnalyze(_ *MismatchEntry, _ string) bool {
	return true
}

func (a *StatusMismatchAnalyzer) Analyze(_ context.Context, _ *MismatchEntry, cellAResp, cellBResp []byte) ([]*AnalysisResult, error) {
	statusA := extractStatus(cellAResp)
	statusB := extractStatus(cellBResp)

	if statusA == statusB {
		return nil, nil
	}

	r := &AnalysisResult{
		DetectedMismatchType: "status_mismatch",
		PredictedCause:       PredictedCauseUnknown,
		Details:              fmt.Sprintf("cellA body_status=%s, cellB body_status=%s", statusA, statusB),
	}

	cellAFailed := statusA != statusSuccess
	cellBFailed := statusB != statusSuccess

	switch {
	case cellAFailed != cellBFailed:
		r.Details += "; one cell failed, may be transient error"
	default:
		r.Details += "; both cells had non-matching statuses"
	}

	return []*AnalysisResult{r}, nil
}

func extractStatus(raw []byte) string {
	if len(raw) == 0 {
		return "unknown"
	}
	var envelope struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Status == "" {
		return "unknown"
	}
	return envelope.Status
}

// --------------------------------------------------------
// ResultTypeMismatchAnalyzer
// --------------------------------------------------------

type ResultTypeMismatchAnalyzer struct{}

func (a *ResultTypeMismatchAnalyzer) Name() string { return "result_type_mismatch" }

func (a *ResultTypeMismatchAnalyzer) CanAnalyze(_ *MismatchEntry, _ string) bool {
	return true
}

func (a *ResultTypeMismatchAnalyzer) Analyze(_ context.Context, _ *MismatchEntry, cellAResp, cellBResp []byte) ([]*AnalysisResult, error) {
	typeA := detectResultType(cellAResp)
	typeB := detectResultType(cellBResp)

	if typeA == typeB || typeA == "" || typeB == "" {
		return nil, nil
	}

	r := &AnalysisResult{
		DetectedMismatchType: "result_type_mismatch",
		PredictedCause:       PredictedCauseDataMismatch,
		Details:              fmt.Sprintf("cellA returned %q, cellB returned %q; different result types indicate a query engine difference", typeA, typeB),
	}

	return []*AnalysisResult{r}, nil
}
