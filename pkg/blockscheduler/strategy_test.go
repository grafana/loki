package blockscheduler

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
)

func TestTimeRangePlanner_Plan(t *testing.T) {
	interval := 15 * time.Minute
	for _, tc := range []struct {
		name         string
		now          time.Time
		expectedJobs []Job
		groupLag     map[int32]kadm.GroupMemberLag
		consumeUpto  map[int32]kadm.ListedOffset
	}{
		{
			// Interval 1
			// now: 00:42:00. consume until 00:15:00
			// last consumed offset 100 with record ts: 00:10:00
			// record offset with ts after 00:15:00 - offset 200
			// resulting jobs: [100, 200]
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
			expectedJobs: []Job{
				{
					Partition:   0,
					StartOffset: 101,
					EndOffset:   200,
				},
			},
		},
		{
			// Interval 2
			// now: 00:46:00. consume until 00:30:00
			// last consumed offset 199 with record ts: 00:11:00
			// record offset with ts after 00:30:00 - offset 300
			// resulting jobs: [200, 300]
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
			expectedJobs: []Job{
				{
					Partition:   0,
					StartOffset: 200,
					EndOffset:   300,
				},
				{
					Partition:   1,
					StartOffset: 12,
					EndOffset:   123,
				},
			},
		},
		{
			// Interval 2 - run scheduling again
			// now: 00:48:00. consume until 00:30:00
			// last consumed offset 299
			// record offset with ts after 00:30:00 - offset 300
			// no jobs to schedule for partition 0
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
				// still pending. assume no builder were assigned
				1: {
					Offset: 123,
				},
			},
			expectedJobs: []Job{
				{
					Partition:   1,
					StartOffset: 12,
					EndOffset:   123,
				},
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

func (m *mockOffsetReader) ListOffsetsAfterMilli(_ context.Context, ts int64) (map[int32]kadm.ListedOffset, error) {
	return m.offsetsAfterMilli, nil
}

func (m *mockOffsetReader) GroupLag(_ context.Context) (map[int32]kadm.GroupMemberLag, error) {
	return m.groupLag, nil
}
