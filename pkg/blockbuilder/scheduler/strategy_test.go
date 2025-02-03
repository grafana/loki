package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
)

type mockOffsetReader struct {
	groupLag map[int32]partition.Lag
}

func (m *mockOffsetReader) GroupLag(_ context.Context, _ int64) (map[int32]partition.Lag, error) {
	return m.groupLag, nil
}

// compareJobs compares two JobWithMetadata instances ignoring UpdateTime
func compareJobs(t *testing.T, expected, actual *JobWithMetadata) {
	require.Equal(t, expected.Job, actual.Job)
	require.Equal(t, expected.Priority, actual.Priority)
	require.Equal(t, expected.Status, actual.Status)
	require.Equal(t, expected.StartTime, actual.StartTime)
}

func TestRecordCountPlanner_Plan(t *testing.T) {
	for _, tc := range []struct {
		name             string
		recordCount      int64
		minOffsetsPerJob int
		expectedJobs     []*JobWithMetadata
		groupLag         map[int32]partition.Lag
	}{
		{
			name:             "single partition, single job",
			recordCount:      100,
			minOffsetsPerJob: 0,
			groupLag: map[int32]partition.Lag{
				0: partition.NewLag(
					50,  // startOffset (committed offset)
					150, // endOffset
					100, // committedOffset
					50,  // rawLag (endOffset - committedOffset)
				),
			},
			expectedJobs: []*JobWithMetadata{
				NewJobWithMetadata(
					types.NewJob(0, types.Offsets{Min: 101, Max: 150}),
					49, // 150-101
				),
			},
		},
		{
			name:             "single partition, multiple jobs",
			recordCount:      50,
			minOffsetsPerJob: 0,
			groupLag: map[int32]partition.Lag{
				0: partition.NewLag(
					50,  // startOffset (committed offset)
					200, // endOffset
					100, // committedOffset
					100, // rawLag (endOffset - committedOffset)
				),
			},
			expectedJobs: []*JobWithMetadata{
				NewJobWithMetadata(
					types.NewJob(0, types.Offsets{Min: 101, Max: 151}),
					99, // priority is total remaining: 200-101
				),
				NewJobWithMetadata(
					types.NewJob(0, types.Offsets{Min: 151, Max: 200}),
					49, // priority is total remaining: 200-151
				),
			},
		},
		{
			name:             "multiple partitions",
			recordCount:      100,
			minOffsetsPerJob: 0,
			groupLag: map[int32]partition.Lag{
				0: partition.NewLag(
					100, // startOffset (committed offset)
					150, // endOffset
					100, // committedOffset
					50,  // rawLag (endOffset - committedOffset)
				),
				1: partition.NewLag(
					200, // startOffset (committed offset)
					400, // endOffset
					200, // committedOffset
					200, // rawLag (endOffset - committedOffset)
				),
			},
			expectedJobs: []*JobWithMetadata{
				NewJobWithMetadata(
					types.NewJob(0, types.Offsets{Min: 101, Max: 150}),
					49, // priority is total remaining: 150-101
				),
				NewJobWithMetadata(
					types.NewJob(1, types.Offsets{Min: 201, Max: 301}),
					199, // priority is total remaining: 400-201
				),
				NewJobWithMetadata(
					types.NewJob(1, types.Offsets{Min: 301, Max: 400}),
					99, // priority is total remaining: 400-301
				),
			},
		},
		{
			name:             "no lag",
			recordCount:      100,
			minOffsetsPerJob: 0,
			groupLag: map[int32]partition.Lag{
				0: partition.NewLag(
					100, // startOffset (committed offset)
					100, // endOffset
					100, // committedOffset
					0,   // rawLag (endOffset - committedOffset)
				),
			},
			expectedJobs: nil,
		},
		{
			name:             "skip small jobs",
			recordCount:      100,
			minOffsetsPerJob: 40,
			groupLag: map[int32]partition.Lag{
				0: partition.NewLag(
					100, // startOffset (committed offset)
					130, // endOffset
					100, // committedOffset
					30,  // rawLag (endOffset - committedOffset)
				),
				1: partition.NewLag(
					200, // startOffset (committed offset)
					300, // endOffset
					200, // committedOffset
					100, // rawLag (endOffset - committedOffset)
				),
			},
			expectedJobs: []*JobWithMetadata{
				NewJobWithMetadata(
					types.NewJob(1, types.Offsets{Min: 201, Max: 300}),
					99, // priority is total remaining: 300-201
				),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockReader := &mockOffsetReader{
				groupLag: tc.groupLag,
			}
			cfg := Config{
				Interval:          time.Second, // foced > 0 in validation
				Strategy:          RecordCountStrategy,
				TargetRecordCount: tc.recordCount,
			}
			require.NoError(t, cfg.Validate())
			planner := NewRecordCountPlanner(mockReader, tc.recordCount, 0, log.NewNopLogger())
			jobs, err := planner.Plan(context.Background(), 0, tc.minOffsetsPerJob)
			require.NoError(t, err)

			require.Equal(t, len(tc.expectedJobs), len(jobs))
			for i := range tc.expectedJobs {
				compareJobs(t, tc.expectedJobs[i], jobs[i])
			}
		})
	}
}
