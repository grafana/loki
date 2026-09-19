package limits

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

// The decision logic is added in a follow-up, so the RPC answers no streams
// and the frontend completes the response instead.
func TestService_CheckLimitsAndShard(t *testing.T) {
	s := Service{}
	resp, err := s.CheckLimitsAndShard(t.Context(), &proto.CheckLimitsAndShardRequest{
		Tenant:  "test",
		Streams: []*proto.StreamMetadata{{StreamHash: 0x1}},
	})
	require.NoError(t, err)
	require.Empty(t, resp.Results)
}
