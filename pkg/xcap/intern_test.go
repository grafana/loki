package xcap

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestInternStatisticReturnsSharedInstance asserts repeated interning of the
// same definition returns the identical instance, so captures share statistics.
func TestInternStatisticReturnsSharedInstance(t *testing.T) {
	in := newStatisticInterner(internStatisticLimit)

	s1, err := in.intern("bytes.read", DataTypeInt64, AggregationTypeSum)
	require.NoError(t, err)
	s2, err := in.intern("bytes.read", DataTypeInt64, AggregationTypeSum)
	require.NoError(t, err)

	require.Same(t, s1, s2, "same definition must return the same instance")
	require.Equal(t, StatisticKey{Name: "bytes.read", DataType: DataTypeInt64, Aggregation: AggregationTypeSum}, s1.Key())
	require.Equal(t, ExportWire, s1.Scope())
}

// TestInternStatisticDistinctDefinitions asserts differing name, data type, or
// aggregation each yield a distinct, correctly typed instance.
func TestInternStatisticDistinctDefinitions(t *testing.T) {
	in := newStatisticInterner(internStatisticLimit)

	intStat, err := in.intern("v", DataTypeInt64, AggregationTypeSum)
	require.NoError(t, err)
	floatStat, err := in.intern("v", DataTypeFloat64, AggregationTypeMin)
	require.NoError(t, err)
	boolStat, err := in.intern("v", DataTypeBool, AggregationTypeMax)
	require.NoError(t, err)
	byName, err := in.intern("w", DataTypeInt64, AggregationTypeSum)
	require.NoError(t, err)
	byAgg, err := in.intern("v", DataTypeInt64, AggregationTypeMin)
	require.NoError(t, err)

	require.IsType(t, &StatisticInt64{}, intStat)
	require.IsType(t, &StatisticFloat64{}, floatStat)
	require.IsType(t, &StatisticFlag{}, boolStat)
	require.NotSame(t, intStat, floatStat)
	require.NotSame(t, intStat, byName)
	require.NotSame(t, intStat, byAgg)
}

// TestInternStatisticBounded asserts that once the cache is full, further
// distinct statistics are returned correctly but not cached (bounding memory),
// while already-cached statistics keep returning the shared instance.
func TestInternStatisticBounded(t *testing.T) {
	in := newStatisticInterner(1)

	cached, err := in.intern("first", DataTypeInt64, AggregationTypeSum)
	require.NoError(t, err)

	// The cache is now full. A new definition is still returned correctly.
	overflow1, err := in.intern("second", DataTypeInt64, AggregationTypeSum)
	require.NoError(t, err)
	require.Equal(t, "second", overflow1.Name())

	// Because it was not cached, interning it again returns a different
	// instance.
	overflow2, err := in.intern("second", DataTypeInt64, AggregationTypeSum)
	require.NoError(t, err)
	require.NotSame(t, overflow1, overflow2, "overflow statistics must not be cached")

	// The already-cached statistic keeps returning the shared instance.
	cachedAgain, err := in.intern("first", DataTypeInt64, AggregationTypeSum)
	require.NoError(t, err)
	require.Same(t, cached, cachedAgain)
}
