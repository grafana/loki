package push

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestGenericTreeRoundTrip proves the generic, role-agnostic tree round-trips
// through protobuf while reusing the existing LabelPairAdapter/EntryAdapter
// messages. A resource->scope->entry shape is just one instantiation.
func TestGenericTreeRoundTrip(t *testing.T) {
	tree := &LogsTree{
		Roots: []TreeNode{{
			Kind:       "resource",
			Attributes: []LabelPairAdapter{{Name: "service.name", Value: "checkout"}},
			Children: []TreeNode{{
				Kind:       "scope",
				Attributes: []LabelPairAdapter{{Name: "scope.name", Value: "http"}},
				Entries: []EntryAdapter{{
					Timestamp:          time.Unix(0, 1).UTC(),
					Line:               "hello from a leaf",
					StructuredMetadata: []LabelPairAdapter{{Name: "trace_id", Value: "abc"}},
				}},
			}},
		}},
	}

	data, err := tree.Marshal()
	require.NoError(t, err)

	got := &LogsTree{}
	require.NoError(t, got.Unmarshal(data))
	require.Equal(t, tree, got)
}

// TestGenericTreeArbitraryDepth shows the schema carries no fixed depth:
// root -> l1 -> l2 with an entry at the leaf round-trips just as well.
func TestGenericTreeArbitraryDepth(t *testing.T) {
	deep := &LogsTree{Roots: []TreeNode{{
		Kind: "l0",
		Children: []TreeNode{{
			Kind: "l1",
			Children: []TreeNode{{
				Kind:    "l2",
				Entries: []EntryAdapter{{Line: "deep"}},
			}},
		}},
	}}}

	data, err := deep.Marshal()
	require.NoError(t, err)

	got := &LogsTree{}
	require.NoError(t, got.Unmarshal(data))
	require.Equal(t, "deep", got.Roots[0].Children[0].Children[0].Entries[0].Line)
}
