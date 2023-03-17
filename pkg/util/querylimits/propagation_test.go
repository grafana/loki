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
	payload := `{"maxEntriesLimitPerQuery": 100, "maxQueryLength": "2d", "maxQueryLookback": "2w", "queryTimeout": "5s"}`
	limits, err := UnmarshalQueryLimits([]byte(payload))
	require.NoError(t, err)
	require.Equal(t, model.Duration(2*24*time.Hour), limits.MaxQueryLength)
	require.Equal(t, model.Duration(14*24*time.Hour), limits.MaxQueryLookback)
	require.Equal(t, model.Duration(5*time.Second), limits.QueryTimeout)
	require.Equal(t, 100, limits.MaxEntriesLimitPerQuery)
	// some limits are empty
	payload = `{"maxQueryLength":"1h"}`
	limits, err = UnmarshalQueryLimits([]byte(payload))
	require.NoError(t, err)
	require.Equal(t, model.Duration(3600000000000), limits.MaxQueryLength)
	require.Equal(t, model.Duration(0), limits.MaxQueryLookback)
	require.Equal(t, 0, limits.MaxEntriesLimitPerQuery)
}

func TestSerializingQueryLimits(t *testing.T) {
	// full struct
	limits := QueryLimits{
		MaxQueryLength:          model.Duration(2 * 24 * time.Hour),
		MaxQueryLookback:        model.Duration(14 * 24 * time.Hour),
		MaxEntriesLimitPerQuery: 100,
		QueryTimeout:            model.Duration(5 * time.Second),
	}

	actual, err := MarshalQueryLimits(&limits)
	require.NoError(t, err)
	expected := `{"maxEntriesLimitPerQuery": 100, "maxQueryLength": "2d", "maxQueryLookback": "2w", "queryTimeout": "5s"}`
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
