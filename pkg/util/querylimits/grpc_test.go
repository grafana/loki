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

	dur := model.Duration(2 * 24 * time.Hour)
	entries := 100
	limits := QueryLimits{
		MaxQueryLength:          &dur,
		MaxQueryLookback:        &dur,
		MaxEntriesLimitPerQuery: &entries,
	}
	c1 := InjectQueryLimitsContext(context.Background(), limits)

	c1, err = injectIntoGRPCRequest(c1)
	require.NoError(t, err)

	c2, err := extractFromGRPCRequest(c1)
	require.NoError(t, err)
	require.Equal(t, limits, *(c2.Value(queryLimitsContextKey).(*QueryLimits)))
}
