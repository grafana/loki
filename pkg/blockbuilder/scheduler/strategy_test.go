package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
)

func TestTimeRangePlanner_Plan(t *testing.T) {
	interval := 15 * time.Minute
	for _, tc := range []struct {
		name         string
		now          time.Time
		expectedJobs []*JobWithPriority[int]
		groupLag     map[int32]kadm.GroupMemberLag
		consumeUpto  map[int32]kadm.ListedOffset
	}{
		{
			name: "normal case. schedule first interval",
			now:  time.Date(0, 0, 0, 0, 42, 0, 0, time.UTC), // 00:42:00
			groupLag: map[int32]kadm.GroupMemberLag{
				0: {
					Commit: kadm.Offset{
						At: 100,
					},
					Partition: 0,
				},
			},
			consumeUpto: map[int32]kadm.ListedOffset{
				0: {
					Offset: 200,
				},
			},
			expectedJobs: []*JobWithPriority[int]{
				NewJobWithPriority(
					types.NewJob(0, types.Offsets{Min: 101, Max: 200}),
					99, // 200-101
				),
			},
		},
		{
			name: "normal case. schedule second interval",
			now:  time.Date(0, 0, 0, 0, 46, 0, 0, time.UTC), // 00:46:00
			groupLag: map[int32]kadm.GroupMemberLag{
				0: {
					Commit: kadm.Offset{
						At: 199,
					},
					Partition: 0,
				},
				1: {
					Commit: kadm.Offset{
						At: 11,
					},
					Partition: 1,
				},
			},
			consumeUpto: map[int32]kadm.ListedOffset{
				0: {
					Offset: 300,
				},
				1: {
					Offset: 123,
				},
			},
			expectedJobs: []*JobWithPriority[int]{
				NewJobWithPriority(
					types.NewJob(0, types.Offsets{Min: 200, Max: 300}),
					100, // 300-200
				),
				NewJobWithPriority(
					types.NewJob(1, types.Offsets{Min: 12, Max: 123}),
					111, // 123-12
				),
			},
		},
		{
			name: "no pending records to consume. schedule second interval once more time",
			now:  time.Date(0, 0, 0, 0, 48, 0, 0, time.UTC), // 00:48:00
			groupLag: map[int32]kadm.GroupMemberLag{
				0: {
					Commit: kadm.Offset{
						At: 299,
					},
					Partition: 0,
				},
				1: {
					Commit: kadm.Offset{
						At: 11,
					},
					Partition: 1,
				},
			},
			consumeUpto: map[int32]kadm.ListedOffset{
				0: {
					Offset: 300,
				},
				1: {
					Offset: 123,
				},
			},
			expectedJobs: []*JobWithPriority[int]{
				NewJobWithPriority(
					types.NewJob(1, types.Offsets{Min: 12, Max: 123}),
					111, // 123-12
				),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockOffsetReader := &mockOffsetReader{
				offsetsAfterMilli: tc.consumeUpto,
				groupLag:          tc.groupLag,
			}
			planner := NewTimeRangePlanner(interval, mockOffsetReader, func() time.Time { return tc.now }, log.NewNopLogger())

			jobs, err := planner.Plan(context.Background())
			require.NoError(t, err)

			require.Equal(t, len(tc.expectedJobs), len(jobs))
			require.Equal(t, tc.expectedJobs, jobs)
		})
	}
}

type mockOffsetReader struct {
	offsetsAfterMilli map[int32]kadm.ListedOffset
	groupLag          map[int32]kadm.GroupMemberLag
}

func (m *mockOffsetReader) ListOffsetsAfterMilli(_ context.Context, _ int64) (map[int32]kadm.ListedOffset, error) {
	return m.offsetsAfterMilli, nil
}

func (m *mockOffsetReader) GroupLag(_ context.Context) (map[int32]kadm.GroupMemberLag, error) {
	return m.groupLag, nil
}
