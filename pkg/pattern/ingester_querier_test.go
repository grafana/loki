package pattern

import (
	"bufio"
	"os"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func Test_prunePatterns(t *testing.T) {
	file, err := os.Open(`testdata/patterns.txt`)
	require.NoError(t, err)
	defer file.Close()

	resp := new(logproto.QueryPatternsResponse)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		resp.Series = append(resp.Series, &logproto.PatternSeries{
			Pattern: scanner.Text(),
		})
	}
	require.NoError(t, scanner.Err())

func (f *fakeRingClient) Ring() ring.ReadRing {
	return &fakeRing{}
}

type fakeRing struct{}

// InstancesWithTokensCount returns the number of instances in the ring that have tokens.
func (f *fakeRing) InstancesWithTokensCount() int {
	panic("not implemented") // TODO: Implement
}

// InstancesInZoneCount returns the number of instances in the ring that are registered in given zone.
func (f *fakeRing) InstancesInZoneCount(_ string) int {
	panic("not implemented") // TODO: Implement
}

// InstancesWithTokensInZoneCount returns the number of instances in the ring that are registered in given zone and have tokens.
func (f *fakeRing) InstancesWithTokensInZoneCount(_ string) int {
	panic("not implemented") // TODO: Implement
}

// ZonesCount returns the number of zones for which there's at least 1 instance registered in the ring.
func (f *fakeRing) ZonesCount() int {
	panic("not implemented") // TODO: Implement
}

func (f *fakeRing) Get(
	_ uint32,
	_ ring.Operation,
	_ []ring.InstanceDesc,
	_ []string,
	_ []string,
) (ring.ReplicationSet, error) {
	panic("not implemented")
}

func (f *fakeRing) GetAllHealthy(_ ring.Operation) (ring.ReplicationSet, error) {
	return ring.ReplicationSet{}, nil
}

func (f *fakeRing) GetReplicationSetForOperation(_ ring.Operation) (ring.ReplicationSet, error) {
	return ring.ReplicationSet{}, nil
}

func (f *fakeRing) ReplicationFactor() int {
	panic("not implemented")
}

func (f *fakeRing) InstancesCount() int {
	panic("not implemented")
}

func (f *fakeRing) ShuffleShard(_ string, _ int) ring.ReadRing {
	panic("not implemented")
}

func (f *fakeRing) GetInstanceState(_ string) (ring.InstanceState, error) {
	panic("not implemented")
}

func (f *fakeRing) ShuffleShardWithLookback(
	_ string,
	_ int,
	_ time.Duration,
	_ time.Time,
) ring.ReadRing {
	panic("not implemented")
}

func (f *fakeRing) HasInstance(_ string) bool {
	panic("not implemented")
}

func (f *fakeRing) CleanupShuffleShardCache(_ string) {
	panic("not implemented")
}

	require.Equal(t, expectedPatterns, patterns)
	require.Less(t, len(patterns), startingPatterns, "prunePatterns should remove duplicates")
}
