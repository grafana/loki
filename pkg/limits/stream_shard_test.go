package limits

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestService_CheckLimitsAndShard_UnownedPartitionStreamsGetAnExplicitFailedResult(t *testing.T) {
	// Regression guard: a stream whose partition isn't owned by this
	// instance must not silently vanish from the response -- it must come
	// back with an explicit ShardDecisionContext=ReasonFailed entry, so the
	// frontend's fail-open handling can engage for it instead of the caller
	// getting no answer at all for that stream.
	store := newTestStreamShardStore(t, 100, "1B", nil)
	pm, err := newPartitionManager(prometheus.NewRegistry())
	require.NoError(t, err)
	// No partitions assigned to pm, so every stream below is "unowned".
	s := &Service{
		cfg:              Config{NumPartitions: 1},
		partitionManager: pm,
		streamShardStore: store,
		streamShardStreamsDiscardedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "test_streams_discarded_total",
		}, []string{"partition"}),
	}

	resp, err := s.CheckLimitsAndShard(t.Context(), &proto.CheckLimitsAndShardRequest{
		Tenant:  "tenant1",
		Streams: []*proto.StreamMetadata{{StreamHash: 1, TotalSize: 100}},
	})
	require.NoError(t, err)
	require.Equal(t, []*proto.StreamShardResult{{
		StreamHash:           1,
		Shards:               1,
		ShardDecisionContext: uint32(ReasonFailed),
	}}, resp.Results)
}
