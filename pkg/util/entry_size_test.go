package util //nolint:revive

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

func TestEffectiveStructuredMetadataSize(t *testing.T) {
	resource := push.LabelsAdapter{
		{Name: "service.name", Value: "svc"},        // 12 + 3
		{Name: constants.LevelLabel, Value: "info"}, // excluded
	}
	scope := push.LabelsAdapter{
		{Name: "scope.name", Value: "lib"}, // 10 + 3
	}
	resourceSize := StructuredMetadataSize(resource)
	scopeSize := StructuredMetadataSize(scope)
	require.Equal(t, 15, resourceSize)
	require.Equal(t, 13, scopeSize)

	entry := push.Entry{
		Line:               "hello",
		StructuredMetadata: push.LabelsAdapter{{Name: "traceID", Value: "1234"}}, // 7 + 4
		SharedResourceRef:  1,
		SharedScopeRef:     2,
	}

	require.Equal(t, 11+resourceSize+scopeSize, EffectiveStructuredMetadataSize(&entry, resourceSize, scopeSize))
	require.Equal(t, len(entry.Line)+11+resourceSize+scopeSize, EffectiveEntryTotalSize(&entry, resourceSize, scopeSize))

	// Only one of the two sets referenced.
	require.Equal(t, 11+resourceSize, EffectiveStructuredMetadataSize(&entry, resourceSize, 0))
	require.Equal(t, 11+scopeSize, EffectiveStructuredMetadataSize(&entry, 0, scopeSize))

	// With nothing shared the helpers must agree with the non-shared variants.
	require.Equal(t, StructuredMetadataSize(entry.StructuredMetadata), EffectiveStructuredMetadataSize(&entry, 0, 0))
	require.Equal(t, EntryTotalSize(&entry), EffectiveEntryTotalSize(&entry, 0, 0))
}

func TestEffectiveSizeMatchesExpandedEntry(t *testing.T) {
	resource := push.LabelsAdapter{{Name: "service.name", Value: "svc"}}
	scope := push.LabelsAdapter{{Name: "scope.name", Value: "lib"}}
	entry := push.Entry{
		Line:               "hello",
		StructuredMetadata: push.LabelsAdapter{{Name: "traceID", Value: "1234"}},
		SharedResourceRef:  1,
		SharedScopeRef:     2,
	}

	// The deferred accounting must equal what we would have measured had the shared
	// metadata been expanded into the entry at the distributor.
	expanded := push.Entry{
		Line:               entry.Line,
		StructuredMetadata: push.EffectiveStructuredMetadata(resource, scope, entry.StructuredMetadata),
	}

	require.Equal(t, EntryTotalSize(&expanded), EffectiveEntryTotalSize(&entry, StructuredMetadataSize(resource), StructuredMetadataSize(scope)))
}

func TestSharedSetsSize(t *testing.T) {
	require.Equal(t, 0, SharedSetsSize(nil))
	require.Equal(t, 0, SharedSetsSize([]push.SharedStructuredMetadataSet{}))

	sets := []push.SharedStructuredMetadataSet{
		{Attrs: []push.LabelAdapter{
			{Name: "service.name", Value: "svc"},        // 12 + 3
			{Name: constants.LevelLabel, Value: "info"}, // excluded
		}},
		{Attrs: []push.LabelAdapter{{Name: "scope.name", Value: "lib"}}}, // 10 + 3
		{},
	}
	require.Equal(t, 15+13, SharedSetsSize(sets))

	// Each set counts once no matter how many entries reference it: this is the stream level,
	// unexpanded view, unlike the per entry EffectiveEntryTotalSize accounting.
	entries := []push.Entry{
		{Line: "a", SharedResourceRef: 1, SharedScopeRef: 2},
		{Line: "b", SharedResourceRef: 1, SharedScopeRef: 2},
		{Line: "c", SharedResourceRef: 1, SharedScopeRef: 2},
	}
	expandedTotal := 0
	for i := range entries {
		expandedTotal += EffectiveEntryTotalSize(&entries[i], 15, 13)
	}
	require.Equal(t, 3*(1+15+13), expandedTotal)
	require.Equal(t, 15+13, SharedSetsSize(sets), "the pool is unaffected by how many entries reference it")
}
