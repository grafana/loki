package dataset_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

func TestFoldAndPredicate(t *testing.T) {
	for _, tc := range []struct {
		name        string
		left, right dataset.Predicate
		want        dataset.Predicate
	}{
		{"both true", dataset.TruePredicate{}, dataset.TruePredicate{}, dataset.TruePredicate{}},
		{"left false short-circuits", dataset.FalsePredicate{}, namedPredicate, dataset.FalsePredicate{}},
		{"right false short-circuits", namedPredicate, dataset.FalsePredicate{}, dataset.FalsePredicate{}},
		{"left true yields the right operand", dataset.TruePredicate{}, namedPredicate, namedPredicate},
		{"right true yields the left operand", namedPredicate, dataset.TruePredicate{}, namedPredicate},
		{"nil folds like true", nil, namedPredicate, namedPredicate},
		{"neither constant keeps the composite", namedPredicate, namedPredicate,
			dataset.AndPredicate{Left: namedPredicate, Right: namedPredicate}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, dataset.FoldAndPredicate(tc.left, tc.right))
		})
	}
}

func TestFoldOrPredicate(t *testing.T) {
	for _, tc := range []struct {
		name        string
		left, right dataset.Predicate
		want        dataset.Predicate
	}{
		{"both false", dataset.FalsePredicate{}, dataset.FalsePredicate{}, dataset.FalsePredicate{}},
		{"left true short-circuits", dataset.TruePredicate{}, namedPredicate, dataset.TruePredicate{}},
		{"right true short-circuits", namedPredicate, dataset.TruePredicate{}, dataset.TruePredicate{}},
		{"left false yields the right operand", dataset.FalsePredicate{}, namedPredicate, namedPredicate},
		{"right false yields the left operand", namedPredicate, dataset.FalsePredicate{}, namedPredicate},
		{"nil folds like true", nil, namedPredicate, dataset.TruePredicate{}},
		{"neither constant keeps the composite", namedPredicate, namedPredicate,
			dataset.OrPredicate{Left: namedPredicate, Right: namedPredicate}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, dataset.FoldOrPredicate(tc.left, tc.right))
		})
	}
}

func TestFoldNotPredicate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		inner dataset.Predicate
		want  dataset.Predicate
	}{
		// Negating a constant is what a RowReader cannot do for itself: it has no De Morgan
		// rule for one and panics instead.
		{"not true", dataset.TruePredicate{}, dataset.FalsePredicate{}},
		{"not false", dataset.FalsePredicate{}, dataset.TruePredicate{}},
		{"not nil", nil, dataset.FalsePredicate{}},
		{"not a column predicate keeps the composite", namedPredicate,
			dataset.NotPredicate{Inner: namedPredicate}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, dataset.FoldNotPredicate(tc.inner))
		})
	}
}

// TestFoldPredicateCollapsesNestedConstants covers the shape the folding exists for: every
// leaf of a tree turned into a constant, which must reduce to a single constant rather than
// a composite naming no column.
func TestFoldPredicateCollapsesNestedConstants(t *testing.T) {
	allTrue := dataset.FoldAndPredicate(
		dataset.FoldOrPredicate(dataset.TruePredicate{}, dataset.FalsePredicate{}),
		dataset.FoldNotPredicate(dataset.FalsePredicate{}),
	)
	require.Equal(t, dataset.TruePredicate{}, allTrue)

	oneFalse := dataset.FoldAndPredicate(
		dataset.FoldNotPredicate(dataset.TruePredicate{}),
		dataset.FoldOrPredicate(dataset.TruePredicate{}, namedPredicate),
	)
	require.Equal(t, dataset.FalsePredicate{}, oneFalse)

	// A surviving column predicate is not swallowed by the constants around it.
	survives := dataset.FoldAndPredicate(
		dataset.FoldOrPredicate(dataset.FalsePredicate{}, namedPredicate),
		dataset.TruePredicate{},
	)
	require.Equal(t, namedPredicate, survives)
}
