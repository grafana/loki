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
