package xcap

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/xcap/statid"
	"github.com/stretchr/testify/require"
)

func TestValidateStatisticRegistry(t *testing.T) {
	var statistics [statid.Count]Statistic
	require.Error(t, validateStatisticRegistry(statistics))

	stat := NewStatisticInt64(statid.Invalid, "test", AggregationTypeSum)
	for id := statid.ID(1); id < statid.Count; id++ {
		statistics[id] = stat
	}
	require.NoError(t, validateStatisticRegistry(statistics))
}

func TestValidateStatisticRegistryAllowsReservedIDHole(t *testing.T) {
	var statistics [statid.Count]Statistic
	stat := NewStatisticInt64(statid.Invalid, "test", AggregationTypeSum)
	reservedID := statid.ID(1)

	for id := statid.ID(1); id < statid.Count; id++ {
		if id != reservedID {
			statistics[id] = stat
		}
	}

	require.NoError(t, validateStatisticRegistryWithReserved(statistics, func(id statid.ID) bool {
		return id == reservedID
	}))
}

func TestMustValidateStatisticRegistryPanicsWhenIncomplete(t *testing.T) {
	require.Panics(t, MustValidateStatisticRegistry)
}

func TestRegisterStatisticRejectsConflictingID(t *testing.T) {
	withTestStatisticRegistry(t)

	NewStatisticInt64(statid.ID(1), "first", AggregationTypeSum)
	require.Panics(t, func() {
		NewStatisticInt64(statid.ID(1), "conflict", AggregationTypeSum)
	})
}
