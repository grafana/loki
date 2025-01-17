//go:build preview

package scheduler

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
)

// TestPreview is a utility test that runs a local server with the status page.
// Run it with: go test -tags=preview -v -run TestPreview
func TestPreview(t *testing.T) {
	// Setup mock data with varied timestamps
	now := time.Now()
	mockLister := &mockQueueLister{
		pendingJobs: []JobWithMetadata{
			{Job: types.NewJob(11, types.Offsets{Min: 11, Max: 20}), UpdateTime: now.Add(-2 * time.Hour), Priority: 23},
			{Job: types.NewJob(22, types.Offsets{Min: 21, Max: 30}), UpdateTime: now.Add(-1 * time.Hour), Priority: 42},
			{Job: types.NewJob(33, types.Offsets{Min: 22, Max: 40}), UpdateTime: now.Add(-30 * time.Minute), Priority: 11},
		},
		inProgressJobs: []JobWithMetadata{
			{Job: types.NewJob(44, types.Offsets{Min: 1, Max: 10}), StartTime: now.Add(-4 * time.Hour), UpdateTime: now.Add(-3 * time.Hour)},
			{Job: types.NewJob(55, types.Offsets{Min: 11, Max: 110}), StartTime: now.Add(-5 * time.Hour), UpdateTime: now.Add(-4 * time.Hour)},
		},
		completedJobs: []JobWithMetadata{
			{Job: types.NewJob(66, types.Offsets{Min: 1, Max: 50}), StartTime: now.Add(-8 * time.Hour), UpdateTime: now.Add(-7 * time.Hour), Status: types.JobStatusComplete},
			{Job: types.NewJob(77, types.Offsets{Min: 51, Max: 100}), StartTime: now.Add(-6 * time.Hour), UpdateTime: now.Add(-5 * time.Hour), Status: types.JobStatusComplete},
			{Job: types.NewJob(88, types.Offsets{Min: 101, Max: 150}), StartTime: now.Add(-4 * time.Hour), UpdateTime: now.Add(-3 * time.Hour), Status: types.JobStatusFailed},
			{Job: types.NewJob(99, types.Offsets{Min: 151, Max: 200}), StartTime: now.Add(-2 * time.Hour), UpdateTime: now.Add(-1 * time.Hour), Status: types.JobStatusComplete},
		},
	}

	mockReader := &mockOffsetReader{
		groupLag: map[int32]kadm.GroupMemberLag{
			0: {
				Lag:       10,
				Partition: 3,
				End:       kadm.ListedOffset{Offset: 100},
				Commit:    kadm.Offset{At: 90},
			},
			1: {
				Lag:       0,
				Partition: 1,
				End:       kadm.ListedOffset{Offset: 100},
				Commit:    kadm.Offset{At: 100},
			},
			2: {
				Lag:       233,
				Partition: 2,
				End:       kadm.ListedOffset{Offset: 333},
				Commit:    kadm.Offset{At: 100},
			},
		},
	}

	handler := newStatusPageHandler(mockLister, mockReader, int64(time.Hour/time.Millisecond))

	// Start local server
	server := httptest.NewServer(handler)
	defer server.Close()

	fmt.Printf("\n\n=== Preview server running ===\nOpen this URL in your browser:\n%s\nPress Ctrl+C to stop the server\n\n", server.URL)

	// Keep server running until interrupted
	select {}
}
