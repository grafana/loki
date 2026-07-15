package wirepb_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/dskit/httpgrpc"

	"github.com/grafana/loki/v3/pkg/engine/internal/proto/wirepb"
)

// requireHeaderAntisymmetric fails the test unless a.CompareWiresmith(b) and
// b.CompareWiresmith(a) have opposite signs (or are both zero).
func requireHeaderAntisymmetric(t *testing.T, a, b wirepb.HeaderAdapter) {
	t.Helper()
	ab := a.CompareWiresmith(b)
	ba := b.CompareWiresmith(a)
	switch {
	case ab == 0 || ba == 0:
		require.Zerof(t, ab, "a.Compare(b)=%d but b.Compare(a)=%d: only one side reports equality", ab, ba)
		require.Zerof(t, ba, "a.Compare(b)=%d but b.Compare(a)=%d: only one side reports equality", ab, ba)
	case ab > 0:
		require.Negativef(t, ba, "antisymmetry violated: a.Compare(b)=%d (>0) but b.Compare(a)=%d (want <0)", ab, ba)
	default:
		require.Positivef(t, ba, "antisymmetry violated: a.Compare(b)=%d (<0) but b.Compare(a)=%d (want >0)", ab, ba)
	}
}

// TestHeaderAdapterCompareWiresmith covers the ordering behavior of the
// marshal-based CompareWiresmith: equal headers compare 0, differing Key/Values compare
// non-zero and antisymmetrically, and Compare's 0/non-zero split matches EqualWiresmith
// (see the drift_guard_test.go banner: HeaderAdapter is `type HeaderAdapter
// httpgrpc.Header`, so Marshal is the same gogo-generated encoding EqualWiresmith's
// delegated Equal reasons about).
func TestHeaderAdapterCompareWiresmith(t *testing.T) {
	base := wirepb.HeaderAdapter{Key: "X-Scope-OrgID", Values: []string{"fake", "fake-2"}}

	t.Run("equal headers compare zero", func(t *testing.T) {
		other := wirepb.HeaderAdapter{Key: "X-Scope-OrgID", Values: []string{"fake", "fake-2"}}
		require.Zero(t, base.CompareWiresmith(other))
		require.Zero(t, other.CompareWiresmith(base))
		require.True(t, base.EqualWiresmith(other))
	})

	t.Run("differing key", func(t *testing.T) {
		other := wirepb.HeaderAdapter{Key: "X-Other", Values: []string{"fake", "fake-2"}}
		require.NotZero(t, base.CompareWiresmith(other))
		require.False(t, base.EqualWiresmith(other))
		requireHeaderAntisymmetric(t, base, other)
	})

	t.Run("differing values length", func(t *testing.T) {
		other := wirepb.HeaderAdapter{Key: "X-Scope-OrgID", Values: []string{"fake"}}
		require.NotZero(t, base.CompareWiresmith(other))
		require.False(t, base.EqualWiresmith(other))
		requireHeaderAntisymmetric(t, base, other)
	})

	t.Run("differing values content, same length", func(t *testing.T) {
		other := wirepb.HeaderAdapter{Key: "X-Scope-OrgID", Values: []string{"fake", "different"}}
		require.NotZero(t, base.CompareWiresmith(other))
		require.False(t, base.EqualWiresmith(other))
		requireHeaderAntisymmetric(t, base, other)
	})

	t.Run("wrong type", func(t *testing.T) {
		require.Equal(t, -1, base.CompareWiresmith("not a header"))
	})

	t.Run("nil pointer other", func(t *testing.T) {
		var nilPtr *wirepb.HeaderAdapter
		require.Equal(t, -1, base.CompareWiresmith(nilPtr))
	})

	t.Run("pointer other", func(t *testing.T) {
		other := &wirepb.HeaderAdapter{Key: "X-Scope-OrgID", Values: []string{"fake", "fake-2"}}
		require.Zero(t, base.CompareWiresmith(other))
	})
}

// TestHeaderAdapterCompareWiresmithFieldComplete guards wiresmith-yp37: the old
// CompareWiresmith hand-enumerated Key then Values, so a future field on httpgrpc.Header
// would be silently ignored by comparison (while EqualWiresmith, delegating to gogo's
// Header.Equal, would pick it up automatically). This test walks every field of the
// vendored httpgrpc.Header via reflection -- not a hardcoded Key/Values pair -- and proves
// CompareWiresmith distinguishes two Headers differing only in that field. If
// httpgrpc.Header ever gains a field, this test starts exercising it automatically without
// modification: either the perturbation switch below already knows how to vary that field
// kind (and the assertions hold, or fail if CompareWiresmith regressed to ignoring it), or
// it doesn't (and the default case fails closed, naming the field, rather than silently
// skipping it) -- matching the fail-closed philosophy of this package's
// drift_guard_test.go.
func TestHeaderAdapterCompareWiresmithFieldComplete(t *testing.T) {
	base := httpgrpc.Header{Key: "X-Scope-OrgID", Values: []string{"fake", "fake-2"}}
	typ := reflect.TypeOf(base)
	require.Positive(t, typ.NumField(), "httpgrpc.Header has no fields to compare")

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		t.Run(field.Name, func(t *testing.T) {
			modified := base
			fv := reflect.ValueOf(&modified).Elem().Field(i)

			switch {
			case fv.Kind() == reflect.String:
				fv.SetString(fv.String() + "-changed")
			case fv.Kind() == reflect.Slice && fv.Type().Elem().Kind() == reflect.String:
				fv.Set(reflect.AppendSlice(fv, reflect.ValueOf([]string{"-changed"})))
			default:
				t.Fatalf("httpgrpc.Header gained field %q of kind %s that this test doesn't know "+
					"how to perturb; extend the switch above so CompareWiresmith's field-completeness "+
					"stays provable (wiresmith-yp37), then confirm HeaderAdapter.CompareWiresmith "+
					"actually distinguishes it", field.Name, fv.Kind())
			}

			a := wirepb.HeaderAdapter(base)
			b := wirepb.HeaderAdapter(modified)
			require.NotEqualf(t, 0, a.CompareWiresmith(b),
				"CompareWiresmith did not distinguish two Headers differing only in field %q; "+
					"if this is a newly added field, CompareWiresmith is silently ignoring it "+
					"(wiresmith-yp37)", field.Name)
			requireHeaderAntisymmetric(t, a, b)
		})
	}
}
