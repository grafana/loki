package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --------------------------------------------------------
// extractStatus tests
// --------------------------------------------------------

func TestExtractStatus(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		assert.Equal(t, "success", extractStatus([]byte(`{"status":"success"}`)))
	})

	t.Run("error", func(t *testing.T) {
		assert.Equal(t, "error", extractStatus([]byte(`{"status":"error","error":"timeout"}`)))
	})

	t.Run("empty body", func(t *testing.T) {
		assert.Equal(t, "unknown", extractStatus(nil))
	})

	t.Run("invalid json", func(t *testing.T) {
		assert.Equal(t, "unknown", extractStatus([]byte(`{invalid`)))
	})

	t.Run("missing status field", func(t *testing.T) {
		assert.Equal(t, "unknown", extractStatus([]byte(`{"data":{}}`)))
	})
}

// --------------------------------------------------------
// StatusMismatchAnalyzer tests
// --------------------------------------------------------

func TestStatusMismatchAnalyzer_CanAnalyze(t *testing.T) {
	a := &StatusMismatchAnalyzer{}
	assert.True(t, a.CanAnalyze(&MismatchEntry{}, ""))
	assert.True(t, a.CanAnalyze(&MismatchEntry{}, "streams"))
}

func TestStatusMismatchAnalyzer_SameStatus(t *testing.T) {
	a := &StatusMismatchAnalyzer{}
	entry := &MismatchEntry{}
	respA := []byte(`{"status":"success"}`)
	respB := []byte(`{"status":"success"}`)

	results, err := a.Analyze(context.Background(), entry, respA, respB)
	require.NoError(t, err)
	assert.Nil(t, results, "same status should return nil")
}

func TestStatusMismatchAnalyzer_DifferentStatus(t *testing.T) {
	a := &StatusMismatchAnalyzer{}
	entry := &MismatchEntry{}
	respA := []byte(`{"status":"success"}`)
	respB := []byte(`{"status":"error","error":"timeout"}`)

	results, err := a.Analyze(context.Background(), entry, respA, respB)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "status_mismatch", results[0].DetectedMismatchType)
	assert.Contains(t, results[0].Details, "success")
	assert.Contains(t, results[0].Details, "error")
	assert.Contains(t, results[0].Details, "one cell failed")
}

func TestStatusMismatchAnalyzer_OneCellFailed(t *testing.T) {
	a := &StatusMismatchAnalyzer{}
	entry := &MismatchEntry{}
	respA := []byte(`{"status":"error"}`)
	respB := []byte(`{"status":"success"}`)

	results, err := a.Analyze(context.Background(), entry, respA, respB)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, PredictedCauseUnknown, results[0].PredictedCause)
	assert.Contains(t, results[0].Details, "one cell failed")
}

func TestStatusMismatchAnalyzer_BothUnknown(t *testing.T) {
	a := &StatusMismatchAnalyzer{}
	entry := &MismatchEntry{}

	results, err := a.Analyze(context.Background(), entry, nil, []byte(`{"status":"success"}`))
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].Details, "unknown")
}

// --------------------------------------------------------
// ResultTypeMismatchAnalyzer tests
// --------------------------------------------------------

func TestResultTypeMismatchAnalyzer_SameType(t *testing.T) {
	a := &ResultTypeMismatchAnalyzer{}
	entry := &MismatchEntry{}
	resp := []byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`)

	results, err := a.Analyze(context.Background(), entry, resp, resp)
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestResultTypeMismatchAnalyzer_DifferentTypes(t *testing.T) {
	a := &ResultTypeMismatchAnalyzer{}
	entry := &MismatchEntry{}
	respA := []byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`)
	respB := []byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`)

	results, err := a.Analyze(context.Background(), entry, respA, respB)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "result_type_mismatch", results[0].DetectedMismatchType)
	assert.Equal(t, PredictedCauseDataMismatch, results[0].PredictedCause)
	assert.Contains(t, results[0].Details, "vector")
	assert.Contains(t, results[0].Details, "matrix")
}

func TestResultTypeMismatchAnalyzer_EmptyResponse(t *testing.T) {
	a := &ResultTypeMismatchAnalyzer{}
	entry := &MismatchEntry{}

	results, err := a.Analyze(context.Background(), entry, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, results, "empty responses should be skipped")
}

func TestResultTypeMismatchAnalyzer_OneEmptyResponse(t *testing.T) {
	a := &ResultTypeMismatchAnalyzer{}
	entry := &MismatchEntry{}
	resp := []byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`)

	results, err := a.Analyze(context.Background(), entry, resp, nil)
	require.NoError(t, err)
	assert.Nil(t, results, "one empty response should be skipped")
}
