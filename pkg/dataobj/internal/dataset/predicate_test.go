package dataset_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

// namedPredicate stands in for any predicate that names a column.
var namedPredicate = dataset.EqualPredicate{Value: dataset.Int64Value(1)}

func TestIsConstPredicate(t *testing.T) {
	for _, tc := range []struct {
		name      string
		predicate dataset.Predicate
		wantKeep  bool
		wantOK    bool
	}{
		{"true", dataset.TruePredicate{}, true, true},
		{"false", dataset.FalsePredicate{}, false, true},
		{"nil keeps every row", nil, true, true},
		{"a column predicate is not constant", namedPredicate, false, false},
		{"a composite is not constant", dataset.AndPredicate{Left: namedPredicate, Right: namedPredicate}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keep, ok := dataset.IsConstPredicate(tc.predicate)
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.wantKeep, keep)
		})
	}
}

func TestNewConstPredicate(t *testing.T) {
	require.Equal(t, dataset.TruePredicate{}, dataset.NewConstPredicate(true))
	require.Equal(t, dataset.FalsePredicate{}, dataset.NewConstPredicate(false))
}
