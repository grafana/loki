package push

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEffectiveStructuredMetadata(t *testing.T) {
	own := LabelsAdapter{{Name: "traceID", Value: "1234"}}
	resource := LabelsAdapter{{Name: "service.name", Value: "svc"}}
	scope := LabelsAdapter{{Name: "scope.name", Value: "lib"}}

	for _, tc := range []struct {
		name     string
		resource LabelsAdapter
		scope    LabelsAdapter
		own      LabelsAdapter
		expected LabelsAdapter
	}{
		{name: "all nil"},
		{name: "all empty", resource: LabelsAdapter{}, scope: LabelsAdapter{}, own: LabelsAdapter{}},
		{name: "only resource", resource: resource, expected: resource},
		{name: "only scope", scope: scope, expected: scope},
		{name: "only own", own: own, expected: own},
		{name: "only resource, others empty", resource: resource, scope: LabelsAdapter{}, own: LabelsAdapter{}, expected: resource},
		{name: "only scope, others empty", resource: LabelsAdapter{}, scope: scope, own: LabelsAdapter{}, expected: scope},
		{name: "only own, others empty", resource: LabelsAdapter{}, scope: LabelsAdapter{}, own: own, expected: own},
		{
			name:     "resource and scope",
			resource: resource,
			scope:    scope,
			expected: LabelsAdapter{
				{Name: "service.name", Value: "svc"},
				{Name: "scope.name", Value: "lib"},
			},
		},
		{
			name:     "resource and own",
			resource: resource,
			own:      own,
			expected: LabelsAdapter{
				{Name: "service.name", Value: "svc"},
				{Name: "traceID", Value: "1234"},
			},
		},
		{
			name:  "scope and own",
			scope: scope,
			own:   own,
			expected: LabelsAdapter{
				{Name: "scope.name", Value: "lib"},
				{Name: "traceID", Value: "1234"},
			},
		},
		{
			// The read path keeps the last pair for a repeated name, so this order gives
			// own > scope > resource.
			name:     "all three keep resource, scope, own order",
			resource: resource,
			scope:    scope,
			own:      own,
			expected: LabelsAdapter{
				{Name: "service.name", Value: "svc"},
				{Name: "scope.name", Value: "lib"},
				{Name: "traceID", Value: "1234"},
			},
		},
		{
			name:     "own wins a collision with both shared sets by coming last",
			resource: LabelsAdapter{{Name: "host.name", Value: "from-resource"}},
			scope:    LabelsAdapter{{Name: "host.name", Value: "from-scope"}},
			own:      LabelsAdapter{{Name: "host.name", Value: "from-log-record"}},
			expected: LabelsAdapter{
				{Name: "host.name", Value: "from-resource"},
				{Name: "host.name", Value: "from-scope"},
				{Name: "host.name", Value: "from-log-record"},
			},
		},
		{
			name:     "scope wins a collision with resource by coming later",
			resource: LabelsAdapter{{Name: "host.name", Value: "from-resource"}},
			scope:    LabelsAdapter{{Name: "host.name", Value: "from-scope"}},
			expected: LabelsAdapter{
				{Name: "host.name", Value: "from-resource"},
				{Name: "host.name", Value: "from-scope"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := EffectiveStructuredMetadata(tc.resource, tc.scope, tc.own)
			if len(tc.expected) == 0 {
				require.Empty(t, got)
				return
			}
			require.Equal(t, tc.expected, got)
		})
	}
}

func TestEffectiveStructuredMetadataDoesNotMutateInputs(t *testing.T) {
	// Give each input spare capacity so that an in-place append would silently succeed.
	own := make(LabelsAdapter, 1, 4)
	own[0] = LabelAdapter{Name: "traceID", Value: "1234"}
	resource := make(LabelsAdapter, 1, 4)
	resource[0] = LabelAdapter{Name: "service.name", Value: "svc"}
	scope := make(LabelsAdapter, 1, 4)
	scope[0] = LabelAdapter{Name: "scope.name", Value: "lib"}

	merged := EffectiveStructuredMetadata(resource, scope, own)
	require.Len(t, merged, 3)

	for i := range merged {
		merged[i] = LabelAdapter{Name: "clobbered", Value: "clobbered"}
	}
	require.Equal(t, LabelAdapter{Name: "service.name", Value: "svc"}, resource[0])
	require.Equal(t, LabelAdapter{Name: "scope.name", Value: "lib"}, scope[0])
	require.Equal(t, LabelAdapter{Name: "traceID", Value: "1234"}, own[0])
	require.Equal(t, 1, len(own))
}

// TestEffectiveStructuredMetadataAliasedViewsHaveNoSpareCapacity covers the fast paths that
// return one of the inputs as is: the returned view must have no spare capacity so that a
// later append allocates a copy instead of writing into the sibling memory of the entry's
// slice or of a set of the stream's pool, which is typically there because proto
// unmarshalling grows those slices with append.
func TestEffectiveStructuredMetadataAliasedViewsHaveNoSpareCapacity(t *testing.T) {
	t.Run("nothing shared aliases own", func(t *testing.T) {
		// Two entries' structured metadata backed by the same array, as an over-allocated
		// unmarshal buffer would leave them.
		backing := make(LabelsAdapter, 2, 4)
		backing[0] = LabelAdapter{Name: "traceID", Value: "1234"}
		backing[1] = LabelAdapter{Name: "spanID", Value: "5678"}

		own := backing[:1]
		view := EffectiveStructuredMetadata(nil, nil, own)
		require.Equal(t, LabelsAdapter{{Name: "traceID", Value: "1234"}}, view)
		require.Equal(t, len(view), cap(view))

		//nolint:gocritic // appending to the returned view is exactly what must stay harmless.
		view = append(view, LabelAdapter{Name: "appended", Value: "appended"})
		require.Equal(t, LabelAdapter{Name: "spanID", Value: "5678"}, backing[1], "append must not have written into the sibling entry")
	})

	t.Run("only a resource set aliases it", func(t *testing.T) {
		pool := sharedPoolWithSibling(t, LabelAdapter{Name: "service.name", Value: "svc"})

		view := EffectiveStructuredMetadata(pool.set, nil, nil)
		require.Equal(t, len(view), cap(view))

		//nolint:gocritic // appending to the returned view is exactly what must stay harmless.
		view = append(view, LabelAdapter{Name: "appended", Value: "appended"})
		require.Equal(t, LabelAdapter{Name: "sibling", Value: "sibling"}, pool.sibling[0], "append must not have written into the pool's spare capacity")
		require.Equal(t, LabelsAdapter{{Name: "service.name", Value: "svc"}}, pool.set)
	})

	t.Run("only a scope set aliases it", func(t *testing.T) {
		pool := sharedPoolWithSibling(t, LabelAdapter{Name: "scope.name", Value: "lib"})

		view := EffectiveStructuredMetadata(nil, pool.set, nil)
		require.Equal(t, len(view), cap(view))

		//nolint:gocritic // appending to the returned view is exactly what must stay harmless.
		view = append(view, LabelAdapter{Name: "appended", Value: "appended"})
		require.Equal(t, LabelAdapter{Name: "sibling", Value: "sibling"}, pool.sibling[0], "append must not have written into the pool's spare capacity")
	})
}

func TestCombinedShared(t *testing.T) {
	resource := LabelsAdapter{{Name: "service.name", Value: "svc"}}
	scope := LabelsAdapter{{Name: "scope.name", Value: "lib"}}

	require.Empty(t, CombinedShared(nil, nil))
	require.Equal(t, resource, CombinedShared(resource, nil))
	require.Equal(t, scope, CombinedShared(nil, scope))
	require.Equal(t, LabelsAdapter{
		{Name: "service.name", Value: "svc"},
		{Name: "scope.name", Value: "lib"},
	}, CombinedShared(resource, scope))

	// Resource before scope: the chunk keeps the given order for two shared pairs sharing a
	// name, and the read path keeps the last, so scope wins.
	require.Equal(t, LabelsAdapter{
		{Name: "host.name", Value: "from-resource"},
		{Name: "host.name", Value: "from-scope"},
	}, CombinedShared(
		LabelsAdapter{{Name: "host.name", Value: "from-resource"}},
		LabelsAdapter{{Name: "host.name", Value: "from-scope"}},
	))
}

func TestCombinedSharedAliasedViewsHaveNoSpareCapacity(t *testing.T) {
	t.Run("no scope aliases resource", func(t *testing.T) {
		pool := sharedPoolWithSibling(t, LabelAdapter{Name: "service.name", Value: "svc"})

		view := CombinedShared(pool.set, nil)
		require.Equal(t, len(view), cap(view))

		//nolint:gocritic // appending to the returned view is exactly what must stay harmless.
		view = append(view, LabelAdapter{Name: "appended", Value: "appended"})
		require.Equal(t, LabelAdapter{Name: "sibling", Value: "sibling"}, pool.sibling[0])
	})

	t.Run("no resource aliases scope", func(t *testing.T) {
		pool := sharedPoolWithSibling(t, LabelAdapter{Name: "scope.name", Value: "lib"})

		view := CombinedShared(nil, pool.set)
		require.Equal(t, len(view), cap(view))

		//nolint:gocritic // appending to the returned view is exactly what must stay harmless.
		view = append(view, LabelAdapter{Name: "appended", Value: "appended"})
		require.Equal(t, LabelAdapter{Name: "sibling", Value: "sibling"}, pool.sibling[0])
	})

	t.Run("both parts are copied", func(t *testing.T) {
		resource := LabelsAdapter{{Name: "service.name", Value: "svc"}}
		scope := LabelsAdapter{{Name: "scope.name", Value: "lib"}}

		combined := CombinedShared(resource, scope)
		combined[0] = LabelAdapter{Name: "clobbered", Value: "clobbered"}
		combined[1] = LabelAdapter{Name: "clobbered", Value: "clobbered"}
		require.Equal(t, LabelAdapter{Name: "service.name", Value: "svc"}, resource[0])
		require.Equal(t, LabelAdapter{Name: "scope.name", Value: "lib"}, scope[0])
	})
}

func TestStreamSharedFor(t *testing.T) {
	resource := LabelsAdapter{{Name: "service.name", Value: "svc"}}
	scope := LabelsAdapter{{Name: "scope.name", Value: "lib"}}
	s := Stream{
		SharedStructuredMetadataSets: []SharedStructuredMetadataSet{
			{Attrs: resource},
			{Attrs: scope},
		},
	}

	for _, tc := range []struct {
		name        string
		resourceRef uint32
		scopeRef    uint32
		expResource LabelsAdapter
		expScope    LabelsAdapter
	}{
		{name: "no references"},
		{name: "resource only", resourceRef: 1, expResource: resource},
		{name: "scope only", scopeRef: 2, expScope: scope},
		{name: "both", resourceRef: 1, scopeRef: 2, expResource: resource, expScope: scope},
		{name: "both point at the same set", resourceRef: 1, scopeRef: 1, expResource: resource, expScope: resource},
		// Out of range references mean the producer built the stream wrong. SharedFor stays
		// non-failing and resolves them to nothing; ValidateSharedRefs is what reports them.
		{name: "resource past the end of the pool", resourceRef: 3, scopeRef: 2, expScope: scope},
		{name: "scope past the end of the pool", resourceRef: 1, scopeRef: 99, expResource: resource},
		{name: "max uint32 reference", resourceRef: ^uint32(0), scopeRef: ^uint32(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := Entry{SharedResourceRef: tc.resourceRef, SharedScopeRef: tc.scopeRef}
			gotResource, gotScope := s.SharedFor(&e)
			require.Equal(t, tc.expResource, gotResource)
			require.Equal(t, tc.expScope, gotScope)
		})
	}

	t.Run("empty pool resolves every reference to nothing", func(t *testing.T) {
		empty := Stream{}
		gotResource, gotScope := empty.SharedFor(&Entry{SharedResourceRef: 1, SharedScopeRef: 1})
		require.Nil(t, gotResource)
		require.Nil(t, gotScope)
	})
}

func TestStreamValidateSharedRefs(t *testing.T) {
	pool := []SharedStructuredMetadataSet{
		{Attrs: []LabelAdapter{{Name: "service.name", Value: "svc"}}},
		{Attrs: []LabelAdapter{{Name: "scope.name", Value: "lib"}}},
	}

	t.Run("valid references", func(t *testing.T) {
		s := Stream{
			SharedStructuredMetadataSets: pool,
			Entries: []Entry{
				{SharedResourceRef: 0, SharedScopeRef: 0},
				{SharedResourceRef: 1, SharedScopeRef: 2},
				{SharedResourceRef: 2, SharedScopeRef: 1},
			},
		}
		require.NoError(t, s.ValidateSharedRefs())
	})

	t.Run("no pool and no references", func(t *testing.T) {
		s := Stream{Entries: []Entry{{}, {}}}
		require.NoError(t, s.ValidateSharedRefs())
	})

	t.Run("resource reference past the end", func(t *testing.T) {
		s := Stream{
			SharedStructuredMetadataSets: pool,
			Entries:                      []Entry{{}, {SharedResourceRef: 3}},
		}
		err := s.ValidateSharedRefs()
		require.ErrorContains(t, err, "entry 1")
		require.ErrorContains(t, err, "resource set")
	})

	t.Run("scope reference past the end", func(t *testing.T) {
		s := Stream{
			SharedStructuredMetadataSets: pool,
			Entries:                      []Entry{{SharedScopeRef: 7}},
		}
		err := s.ValidateSharedRefs()
		require.ErrorContains(t, err, "entry 0")
		require.ErrorContains(t, err, "scope set")
	})

	t.Run("references but no pool", func(t *testing.T) {
		s := Stream{Entries: []Entry{{SharedResourceRef: 1}}}
		require.Error(t, s.ValidateSharedRefs())
	})
}

type poolWithSibling struct {
	set     LabelsAdapter
	sibling LabelsAdapter
}

// sharedPoolWithSibling returns a one element shared set that has spare capacity holding
// something else, the way a pool grown by proto unmarshalling looks.
func sharedPoolWithSibling(t *testing.T, attr LabelAdapter) poolWithSibling {
	t.Helper()

	backing := make(LabelsAdapter, 1, 4)
	backing[0] = attr
	sibling := backing[:2:2][1:]
	sibling[0] = LabelAdapter{Name: "sibling", Value: "sibling"}

	return poolWithSibling{set: backing, sibling: sibling}
}
