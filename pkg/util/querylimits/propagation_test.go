package querylimits

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestInjectAndExtractQueryLimits(t *testing.T) {
	ctx := context.Background()
	limits := QueryLimits{
		MaxQueryLength:          model.Duration(2 * 24 * time.Hour),
		MaxQueryLookback:        model.Duration(14 * 24 * time.Hour),
		MaxEntriesLimitPerQuery: 100,
		QueryTimeout:            model.Duration(5 * time.Second),
	}

	ctx = InjectQueryLimitsIntoContext(ctx, limits)
	res := ExtractQueryLimitsFromContext(ctx)
	require.Equal(t, limits, *res)
}

func TestDeserializingQueryLimits(t *testing.T) {
	// full limits
	payload := `{"maxEntriesLimitPerQuery": 100, "maxQueryLength": "2d", "maxQueryLookback": "2w", "maxQueryTime": "5s", "maxQueryBytesRead": "1MB", "maxQuerierBytesRead": "1MB"}`
	limits, err := UnmarshalQueryLimits([]byte(payload))
	require.NoError(t, err)
	require.Equal(t, model.Duration(2*24*time.Hour), limits.MaxQueryLength)
	require.Equal(t, model.Duration(14*24*time.Hour), limits.MaxQueryLookback)
	require.Equal(t, model.Duration(5*time.Second), limits.QueryTimeout)
	require.Equal(t, 100, limits.MaxEntriesLimitPerQuery)
	require.Equal(t, 1*1024*1024, limits.MaxQueryBytesRead.Val())
	// some limits are empty
	payload = `{"maxQueryLength":"1h"}`
	limits, err = UnmarshalQueryLimits([]byte(payload))
	require.NoError(t, err)
	require.Equal(t, model.Duration(3600000000000), limits.MaxQueryLength)
	require.Equal(t, model.Duration(0), limits.MaxQueryLookback)
	require.Equal(t, 0, limits.MaxEntriesLimitPerQuery)
	require.Equal(t, 0, limits.MaxQueryBytesRead.Val())
}

func TestSerializingQueryLimits(t *testing.T) {
	// full struct
	limits := QueryLimits{
		MaxQueryLength:          model.Duration(2 * 24 * time.Hour),
		MaxQueryLookback:        model.Duration(14 * 24 * time.Hour),
		MaxEntriesLimitPerQuery: 100,
		QueryTimeout:            model.Duration(5 * time.Second),
		MaxQueryBytesRead:       1 * 1024 * 1024,
	}

	actual, err := MarshalQueryLimits(&limits)
	require.NoError(t, err)
	expected := `{"maxEntriesLimitPerQuery": 100, "maxQueryLength": "2d", "maxQueryLookback": "2w", "maxQueryTime": "5s", "maxQueryBytesRead": "1MB"}`
	require.JSONEq(t, expected, string(actual))

	// some limits are empty
	limits = QueryLimits{
		MaxQueryLength:          model.Duration(2 * 24 * time.Hour),
		MaxQueryLookback:        model.Duration(14 * 24 * time.Hour),
		MaxEntriesLimitPerQuery: 100,
	}

	actual, err = MarshalQueryLimits(&limits)
	require.NoError(t, err)
	expected = `{"maxEntriesLimitPerQuery": 100, "maxQueryLength": "2d", "maxQueryLookback": "2w"}`
	require.JSONEq(t, expected, string(actual))
}

func TestInjectAndExtractQueryLimitsContext(t *testing.T) {
	ctx := context.Background()
	baseTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	limitsCtx := Context{
		Expr: `{job="app"}`,
		From: baseTime,
		To:   baseTime.Add(1 * time.Hour),
	}

	ctx = InjectQueryLimitsContextIntoContext(ctx, limitsCtx)
	res := ExtractQueryLimitsContextFromContext(ctx)
	require.Equal(t, limitsCtx, *res)
}

func TestDeserializingQueryLimitsContext(t *testing.T) {
	baseTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	endTime := baseTime.Add(1 * time.Hour)

	// full context
	payload := `{"expr": "{job=\"app\"}", "from": "2024-01-15T10:30:00Z", "to": "2024-01-15T11:30:00Z"}`
	limitsCtx, err := UnmarshalQueryLimitsContext([]byte(payload))
	require.NoError(t, err)
	require.Equal(t, `{job="app"}`, limitsCtx.Expr)
	require.Equal(t, baseTime.Unix(), limitsCtx.From.Unix())
	require.Equal(t, endTime.Unix(), limitsCtx.To.Unix())

	// some fields are empty
	payload = `{"expr": "rate({job=\"app\"}[5m])"}`
	limitsCtx, err = UnmarshalQueryLimitsContext([]byte(payload))
	require.NoError(t, err)
	require.Equal(t, `rate({job="app"}[5m])`, limitsCtx.Expr)
	require.True(t, limitsCtx.From.IsZero())
	require.True(t, limitsCtx.To.IsZero())
}

func TestSerializingQueryLimitsContext(t *testing.T) {
	baseTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	endTime := baseTime.Add(1 * time.Hour)

	// full struct
	limitsCtx := Context{
		Expr: `{job="app"}`,
		From: baseTime,
		To:   endTime,
	}

	actual, err := MarshalQueryLimitsContext(&limitsCtx)
	require.NoError(t, err)
	expected := `{"expr": "{job=\"app\"}", "from": "2024-01-15T10:30:00Z", "to": "2024-01-15T11:30:00Z"}`
	require.JSONEq(t, expected, string(actual))

	// some fields are empty
	limitsCtx = Context{
		Expr: `rate({job="app"}[5m])`,
	}

	actual, err = MarshalQueryLimitsContext(&limitsCtx)
	require.NoError(t, err)
	expected = `{"expr": "rate({job=\"app\"}[5m])", "from": "0001-01-01T00:00:00Z", "to": "0001-01-01T00:00:00Z"}`
	require.JSONEq(t, expected, string(actual))
}
