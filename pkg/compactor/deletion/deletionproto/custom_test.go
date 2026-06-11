package deletionproto

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

// TestChunkProtoRoundTripIngestedAt validates that the IngestedAt field added to
// the Chunk message survives marshal/unmarshal and the generated accessor and
// Equal helpers. It guards the hand-maintained generated code for this field.
func TestChunkProtoRoundTripIngestedAt(t *testing.T) {
	for _, ingestedAt := range []model.Time{0, 1234, model.Now()} {
		c := &Chunk{
			From:        100,
			Through:     200,
			Fingerprint: 42,
			Checksum:    7,
			KB:          3,
			Entries:     9,
			IngestedAt:  ingestedAt,
		}

		data, err := c.Marshal()
		require.NoError(t, err)

		var got Chunk
		require.NoError(t, got.Unmarshal(data))

		require.Equal(t, ingestedAt, got.IngestedAt)
		require.Equal(t, ingestedAt, got.GetIngestedAt())
		require.True(t, c.Equal(&got))
	}
}
