package postings

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/arrowconv"
)

// buildPredicateTestSection flushes a single Builder into its own dataobj
// and returns the opened Section. The caller-supplied seed varies the
// (ObjectPath, ColumnName) so two sections built with different seeds
// carry distinct logical content.
//
// ctx is passed in so each test exercises its own test-scoped context via
// t.Context(); the helper does not call t.Context() itself.
func buildPredicateTestSection(ctx context.Context, t *testing.T, seed string) *Section {
	t.Helper()

	b := NewBuilder(nil, 0, 0)
	ts := time.Unix(0, 0).UTC()
	b.ObserveLabelPosting(LabelObservation{
		ObjectPath:       "/obj/" + seed,
		SectionIndex:     0,
		ColumnName:       "col-" + seed,
		LabelValue:       "v",
		StreamID:         1,
		Timestamp:        ts,
		UncompressedSize: 1,
	})

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	var sec *Section
	for _, s := range obj.Sections() {
		if !CheckSection(s) {
			continue
		}
		sec, err = Open(ctx, s)
		require.NoError(t, err)
		break
	}
	require.NotNil(t, sec)
	return sec
}

// TestReaderOptions_Validate_CrossSectionRejected covers the two cross-
// section programmer-error code paths in Validate: (1) Columns themselves
// span two Sections, (2) a Predicate references a Column not in Columns.
// Each case targets a distinct branch in Validate; the substring assertion
// pins the specific error site.
func TestReaderOptions_Validate_CrossSectionRejected(t *testing.T) {
	ctx := t.Context()
	sec1 := buildPredicateTestSection(ctx, t, "cross-a")
	sec2 := buildPredicateTestSection(ctx, t, "cross-b")

	cases := []struct {
		name      string
		opts      ReaderOptions
		wantError string
	}{
		{
			name: "columns span two sections",
			opts: ReaderOptions{
				Columns: []*Column{sec1.Columns()[0], sec2.Columns()[0]},
			},
			wantError: "all columns must belong to the same section",
		},
		{
			name: "predicate column not in Columns",
			opts: ReaderOptions{
				Columns: sec1.Columns(),
				Predicates: []Predicate{
					EqualPredicate{
						Column: sec2.Columns()[0],
						Value:  scalar.NewStringScalar("anything"),
					},
				},
			},
			wantError: "not in Columns",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.Validate()
			require.Error(t, err)
			require.ErrorContains(t, err, tc.wantError)
		})
	}
}

// TestReaderOptions_Validate_UnsupportedScalar confirms that Validate
// rejects predicates whose scalar value has no DatasetType mapping. Arrow
// booleans have no dataset.PhysicalType mapping (see arrowconv.DatasetType).
func TestReaderOptions_Validate_UnsupportedScalar(t *testing.T) {
	// Sanity-check the assumption that boolean is unsupported. If a future
	// change adds boolean support, this test must be updated to use a still-
	// unsupported scalar type rather than silently degrading to a no-op.
	if _, ok := arrowconv.DatasetType(arrow.FixedWidthTypes.Boolean); ok {
		t.Skip("arrowconv.DatasetType(boolean) is supported now; pick a different unsupported scalar for this test")
	}

	ctx := t.Context()
	sec := buildPredicateTestSection(ctx, t, "scalar")
	cols := sec.Columns()
	require.NotEmpty(t, cols)

	opts := ReaderOptions{
		Columns: cols,
		Predicates: []Predicate{
			EqualPredicate{
				Column: cols[0],
				Value:  scalar.NewBooleanScalar(true),
			},
		},
	}

	err := opts.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "unsupported scalar type")
}

// unknownPredicate is a local type that satisfies the unexported
// isPredicate() marker — only possible from within this internal test
// package — to exercise the unrecognized-predicate path of Validate /
// walkPredicate.
type unknownPredicate struct{}

func (unknownPredicate) isPredicate() {}

// TestWalkPredicate_PanicsOnUnknownType pins the panic contract for unknown
// Predicate impls. Validate routes the predicate tree through walkPredicate,
// which panics on any type outside the documented set. The default-branch
// "unrecognized predicate type %T" error in Validate is therefore dead code
// by construction — the programmer-error invariant is enforced at the walk
// step, not by the error return.
func TestWalkPredicate_PanicsOnUnknownType(t *testing.T) {
	ctx := t.Context()
	sec := buildPredicateTestSection(ctx, t, "unknown")
	cols := sec.Columns()
	require.NotEmpty(t, cols)

	opts := ReaderOptions{
		Columns:    cols,
		Predicates: []Predicate{unknownPredicate{}},
	}

	require.PanicsWithValue(t,
		"postings.walkPredicate: unsupported predicate type",
		func() { _ = opts.Validate() },
	)
}
