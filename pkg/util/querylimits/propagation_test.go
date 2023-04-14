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

	ctx = InjectQueryLimitsContext(ctx, limits)
	res := ExtractQueryLimitsContext(ctx)
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
