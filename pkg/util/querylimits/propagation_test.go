package querylimits

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestInjectAndExtractQueryLimits(t *testing.T) {
	ctx := context.Background()
	length := model.Duration(2 * 24 * time.Hour)
	lookback := model.Duration(14 * 24 * time.Hour)
	timeout := model.Duration(5 * time.Second)
	entries := 100
	limits := QueryLimits{
		MaxQueryLength:          &length,
		MaxQueryLookback:        &lookback,
		MaxEntriesLimitPerQuery: &entries,
		QueryTimeout:            &timeout,
	}

	ctx = InjectQueryLimitsContext(ctx, limits)
	res := ExtractQueryLimitsContext(ctx)
	require.Equal(t, limits, *res)
}

func TestDeserializingQueryLimits(t *testing.T) {
	payload := `{"maxQueryLength":"1h"}`
	limits, err := UnmarshalQueryLimits([]byte(payload))
	fmt.Println("limits: ", limits)
	require.NoError(t, err)
	require.Equal(t, model.Duration(3600000000000), *limits.MaxQueryLength)
}

func TestSerializingQueryLimits(t *testing.T) {
	length := model.Duration(2 * 24 * time.Hour)
	lookback := model.Duration(14 * 24 * time.Hour)
	timeout := model.Duration(5 * time.Second)
	entries := 100
	limits := QueryLimits{
		MaxQueryLength:          &length,
		MaxQueryLookback:        &lookback,
		MaxEntriesLimitPerQuery: &entries,
		QueryTimeout:            &timeout,
	}

	actual, err := MarshalQueryLimits(&limits)
	require.NoError(t, err)
	expected := `{"maxEntriesLimitPerQuery": 100, "maxQueryLength": "2d", "maxQueryLookback": "2w", "queryTimeout": "5s"}`
	require.JSONEq(t, expected, string(actual))
}
