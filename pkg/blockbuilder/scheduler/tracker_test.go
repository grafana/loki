package scheduler

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
)

type mockCommitter struct {
	gotOffset    int64
	gotPartition int32
}

func (m *mockCommitter) reset() {
	m.gotOffset = -1
	m.gotPartition = -1
}

func (m *mockCommitter) Commit(_ context.Context, partition int32, offset int64) error {
	m.gotOffset = offset
	m.gotPartition = partition
	return nil
}

func TestCommitTracker(t *testing.T) {
	committer := &mockCommitter{}
	ct := &CommitTracker{
		uncommittedOffsets: make(map[int32][]types.Offsets),
		lastCommitted: map[int32]int64{
			0: 99,
			1: 109,
		},
		committer: committer,
		logger:    log.NewNopLogger(),
	}

	for _, input := range []struct {
		processJob               *types.Job
		expectedCompletedOffsets map[int32][]types.Offsets
		expectedLastCommitted    map[int32]int64

		expectedCommitterOffset   int64
		expectedComitterPartition int32
	}{
		{
			processJob:               types.NewJob(0, types.Offsets{Min: 110, Max: 140}), // add to uncommitted offsets
			expectedCompletedOffsets: map[int32][]types.Offsets{0: {{Min: 110, Max: 140}}},
			expectedLastCommitted:    map[int32]int64{0: 99, 1: 109},
		},
		{
			processJob:               types.NewJob(0, types.Offsets{Min: 180, Max: 200}),                         // add to uncommitted offsets
			expectedCompletedOffsets: map[int32][]types.Offsets{0: {{Min: 110, Max: 140}, {Min: 180, Max: 200}}}, // contains gap
			expectedLastCommitted:    map[int32]int64{0: 99, 1: 109},
		},
		{
			processJob:                types.NewJob(0, types.Offsets{Min: 100, Max: 110}), // fill gap
			expectedCommitterOffset:   139,
			expectedComitterPartition: 0,
			expectedCompletedOffsets:  map[int32][]types.Offsets{0: {{Min: 180, Max: 200}}},
			expectedLastCommitted:     map[int32]int64{0: 139, 1: 109},
		},
		{
			processJob:               types.NewJob(1, types.Offsets{Min: 100, Max: 110}), // already committed. ignore
			expectedCompletedOffsets: map[int32][]types.Offsets{0: {{Min: 180, Max: 200}}, 1: {}},
			expectedLastCommitted:    map[int32]int64{0: 139, 1: 109},
		},
		{
			processJob:                types.NewJob(0, types.Offsets{Min: 140, Max: 180}), // fill gap
			expectedCommitterOffset:   199,
			expectedComitterPartition: 0,
			expectedCompletedOffsets:  map[int32][]types.Offsets{0: {}, 1: {}},
			expectedLastCommitted:     map[int32]int64{0: 199, 1: 109},
		},
	} {
		committer.reset()

		err := ct.ProcessCompletedJob(context.Background(), input.processJob)
		require.NoError(t, err)

		require.Equal(t, input.expectedCompletedOffsets, ct.uncommittedOffsets)
		require.Equal(t, input.expectedLastCommitted, ct.lastCommitted)
		if input.expectedCommitterOffset != 0 {
			require.Equal(t, input.expectedCommitterOffset, committer.gotOffset)
			require.Equal(t, input.expectedComitterPartition, committer.gotPartition)
		}
	}

}
