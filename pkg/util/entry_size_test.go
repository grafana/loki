package util //nolint:revive

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

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
	// unexpanded view. Had the sets been expanded into every entry instead, the same pool would
	// have been charged once per referencing entry.
	entries := []push.Entry{
		{Line: "a", SharedResourceRef: 1, SharedScopeRef: 2},
		{Line: "b", SharedResourceRef: 1, SharedScopeRef: 2},
		{Line: "c", SharedResourceRef: 1, SharedScopeRef: 2},
	}
	expandedTotal := 0
	for i := range entries {
		expanded := push.Entry{
			Line:               entries[i].Line,
			StructuredMetadata: push.EffectiveStructuredMetadata(sets[0].Attrs, sets[1].Attrs, entries[i].StructuredMetadata),
		}
		expandedTotal += EntryTotalSize(&expanded)
	}
	require.Equal(t, 3*(1+15+13), expandedTotal)
	require.Equal(t, 15+13, SharedSetsSize(sets), "the pool is unaffected by how many entries reference it")
}
