package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAnalyzeConfig_FromTo(t *testing.T) {
	cfg := &AnalyzeConfig{
		From: "2026-03-04T10:00:00Z",
		To:   "2026-03-04T11:00:00Z",
	}
	parsed, err := parseAnalyzeConfig(cfg)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC), parsed.FromTime)
	assert.Equal(t, time.Date(2026, 3, 4, 11, 0, 0, 0, time.UTC), parsed.ToTime)
}

func TestParseAnalyzeConfig_Since(t *testing.T) {
	before := time.Now().UTC()
	cfg := &AnalyzeConfig{
		Since: time.Hour,
	}
	parsed, err := parseAnalyzeConfig(cfg)
	require.NoError(t, err)
	after := time.Now().UTC()

	assert.True(t, parsed.ToTime.After(before) || parsed.ToTime.Equal(before))
	assert.True(t, parsed.ToTime.Before(after) || parsed.ToTime.Equal(after))
	assert.InDelta(t, float64(time.Hour), float64(parsed.ToTime.Sub(parsed.FromTime)), float64(time.Second))
}

func TestParseAnalyzeConfig_SinceConflictsWithFromTo(t *testing.T) {
	cfg := &AnalyzeConfig{
		Since: time.Hour,
		From:  "2026-03-04T10:00:00Z",
		To:    "2026-03-04T11:00:00Z",
	}
	_, err := parseAnalyzeConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be used together")
}

func TestParseAnalyzeConfig_SinceConflictsWithFrom(t *testing.T) {
	cfg := &AnalyzeConfig{
		Since: time.Hour,
		From:  "2026-03-04T10:00:00Z",
	}
	_, err := parseAnalyzeConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be used together")
}

func TestParseAnalyzeConfig_NeitherSpecified(t *testing.T) {
	cfg := &AnalyzeConfig{}
	_, err := parseAnalyzeConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "specify either --since or both --from and --to")
}

func TestParseAnalyzeConfig_OnlyFrom(t *testing.T) {
	cfg := &AnalyzeConfig{From: "2026-03-04T10:00:00Z"}
	_, err := parseAnalyzeConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "specify either --since or both --from and --to")
}

func TestParseAnalyzeConfig_FromAfterTo(t *testing.T) {
	cfg := &AnalyzeConfig{
		From: "2026-03-04T12:00:00Z",
		To:   "2026-03-04T10:00:00Z",
	}
	_, err := parseAnalyzeConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--from must be before --to")
}

func TestParseAnalyzeConfig_BadFromFormat(t *testing.T) {
	cfg := &AnalyzeConfig{
		From: "not-a-time",
		To:   "2026-03-04T10:00:00Z",
	}
	_, err := parseAnalyzeConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing --from")
}

func TestParseAnalyzeConfig_DeepMode(t *testing.T) {
	cfg := &AnalyzeConfig{
		Since: time.Hour,
		Mode:  "deep",
	}
	parsed, err := parseAnalyzeConfig(cfg)
	require.NoError(t, err)
	assert.True(t, parsed.DeepMode)
}

func TestParseAnalyzeConfig_BasicModeDefault(t *testing.T) {
	cfg := &AnalyzeConfig{
		Since: time.Hour,
		Mode:  "basic",
	}
	parsed, err := parseAnalyzeConfig(cfg)
	require.NoError(t, err)
	assert.False(t, parsed.DeepMode)
}

func TestParseAnalyzeConfig_CorrelationIDs(t *testing.T) {
	cfg := &AnalyzeConfig{
		Since:             time.Hour,
		CorrelationIDsStr: "aaa, bbb , ccc",
	}
	parsed, err := parseAnalyzeConfig(cfg)
	require.NoError(t, err)
	require.Len(t, parsed.CorrelationIDs, 3)
	_, ok := parsed.CorrelationIDs["aaa"]
	assert.True(t, ok)
	_, ok = parsed.CorrelationIDs["bbb"]
	assert.True(t, ok)
	_, ok = parsed.CorrelationIDs["ccc"]
	assert.True(t, ok)
}

func TestParseAnalyzeConfig_EmptyCorrelationIDs(t *testing.T) {
	cfg := &AnalyzeConfig{
		Since: time.Hour,
	}
	parsed, err := parseAnalyzeConfig(cfg)
	require.NoError(t, err)
	assert.Nil(t, parsed.CorrelationIDs)
}
