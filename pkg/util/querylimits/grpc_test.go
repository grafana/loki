package querylimits

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestGRPCQueryLimits(t *testing.T) {
	var err error
	limits := QueryLimits{
		MaxQueryLength:          model.Duration(2 * 24 * time.Hour),
		MaxQueryLookback:        model.Duration(14 * 24 * time.Hour),
		MaxEntriesLimitPerQuery: 100,
	}
	c1 := InjectQueryLimitsContext(context.Background(), limits)

	c1, err = injectIntoGRPCRequest(c1)
	require.NoError(t, err)

	c2, err := extractFromGRPCRequest(c1)
	require.NoError(t, err)
	require.Equal(t, limits, *(c2.Value(queryLimitsContextKey).(*QueryLimits)))

	c3, err := extractFromGRPCRequest(context.Background())
	require.NoError(t, err)
	require.Nil(t, c3.Value(queryLimitsContextKey))
}
