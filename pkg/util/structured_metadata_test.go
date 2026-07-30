package util //nolint:revive

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"
)

func TestStructuredMetadataHash(t *testing.T) {
	set := push.LabelsAdapter{
		{Name: "service.name", Value: "svc"},
		{Name: "scope.name", Value: "lib"},
	}

	require.Equal(t, uint64(0), StructuredMetadataHash(nil))
	require.Equal(t, uint64(0), StructuredMetadataHash(push.LabelsAdapter{}))

	h := StructuredMetadataHash(set)
	require.NotZero(t, h)
	require.Equal(t, h, StructuredMetadataHash(set), "hashing must be deterministic")

	// Order dependent by design, see the doc comment.
	reversed := push.LabelsAdapter{set[1], set[0]}
	require.NotEqual(t, h, StructuredMetadataHash(reversed))

	// The separator keeps a name/value boundary shift from colliding.
	require.NotEqual(t,
		StructuredMetadataHash(push.LabelsAdapter{{Name: "ab", Value: "c"}}),
		StructuredMetadataHash(push.LabelsAdapter{{Name: "a", Value: "bc"}}),
	)
}

func TestSharedPairHash(t *testing.T) {
	const (
		resourceHash = uint64(0x1122334455667788)
		scopeHash    = uint64(0x99aabbccddeeff00)
	)

	t.Run("deterministic", func(t *testing.T) {
		require.Equal(t, SharedPairHash(resourceHash, scopeHash), SharedPairHash(resourceHash, scopeHash))
	})

	t.Run("nothing shared stays the zero sentinel", func(t *testing.T) {
		require.Equal(t, uint64(0), SharedPairHash(0, 0))
	})

	t.Run("a single set is not the zero sentinel", func(t *testing.T) {
		require.NotZero(t, SharedPairHash(resourceHash, 0))
		require.NotZero(t, SharedPairHash(0, scopeHash))
	})

	t.Run("distinct inputs give distinct hashes", func(t *testing.T) {
		seen := map[uint64]string{}
		for _, tc := range []struct {
			name            string
			resource, scope uint64
		}{
			{"none", 0, 0},
			{"resource only", resourceHash, 0},
			{"scope only", 0, scopeHash},
			{"both", resourceHash, scopeHash},
			{"swapped", scopeHash, resourceHash},
			{"same set twice", resourceHash, resourceHash},
			{"other resource", resourceHash + 1, scopeHash},
			{"other scope", resourceHash, scopeHash + 1},
		} {
			h := SharedPairHash(tc.resource, tc.scope)
			if prev, ok := seen[h]; ok {
				t.Fatalf("%q and %q hash to the same value %d", prev, tc.name, h)
			}
			seen[h] = tc.name
		}
	})

	t.Run("the two roles are not interchangeable", func(t *testing.T) {
		// Swapping resource and scope changes which set wins a name collision, so the pair
		// identity has to change with it.
		require.NotEqual(t, SharedPairHash(resourceHash, scopeHash), SharedPairHash(scopeHash, resourceHash))
	})

	t.Run("built from StructuredMetadataHash", func(t *testing.T) {
		resource := push.LabelsAdapter{{Name: "service.name", Value: "svc"}}
		scope := push.LabelsAdapter{{Name: "scope.name", Value: "lib"}}

		pair := SharedPairHash(StructuredMetadataHash(resource), StructuredMetadataHash(scope))
		require.NotZero(t, pair)
		require.Equal(t, pair, SharedPairHash(StructuredMetadataHash(resource), StructuredMetadataHash(scope)))
		require.NotEqual(t, pair, SharedPairHash(StructuredMetadataHash(resource), 0))

		// An entry referencing no set at all keeps the "nothing shared" sentinel.
		require.Equal(t, uint64(0), SharedPairHash(StructuredMetadataHash(nil), StructuredMetadataHash(nil)))
	})
}
