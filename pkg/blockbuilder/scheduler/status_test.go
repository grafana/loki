package scheduler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
)

type mockQueueLister struct {
	pendingJobs    []JobWithMetadata
	inProgressJobs []JobWithMetadata
	completedJobs  []JobWithMetadata
}

func (m *mockQueueLister) ListPendingJobs() []JobWithMetadata {
	return m.pendingJobs
}

func (m *mockQueueLister) ListInProgressJobs() []JobWithMetadata {
	return m.inProgressJobs
}

func (m *mockQueueLister) ListCompletedJobs() []JobWithMetadata {
	return m.completedJobs
}

func TestStatusPageHandler_ServeHTTP(t *testing.T) {
	t.Skip("skipping. only added to inspect the generated status page.")

	// Setup mock data
	mockLister := &mockQueueLister{
		pendingJobs: []JobWithMetadata{
			{Job: types.NewJob(11, types.Offsets{Min: 11, Max: 20}), UpdateTime: time.Now().Add(-2 * time.Hour), Priority: 23},
			{Job: types.NewJob(22, types.Offsets{Min: 21, Max: 30}), UpdateTime: time.Now().Add(-1 * time.Hour), Priority: 42},
			{Job: types.NewJob(33, types.Offsets{Min: 22, Max: 40}), UpdateTime: time.Now().Add(-1 * time.Hour), Priority: 11},
		},
		inProgressJobs: []JobWithMetadata{
			{Job: types.NewJob(0, types.Offsets{Min: 1, Max: 10}), StartTime: time.Now().Add(-4 * time.Hour), UpdateTime: time.Now().Add(-3 * time.Hour)},
			{Job: types.NewJob(1, types.Offsets{Min: 11, Max: 110}), StartTime: time.Now().Add(-5 * time.Hour), UpdateTime: time.Now().Add(-4 * time.Hour)},
		},
	}

	mockReader := &mockOffsetReader{
		groupLag: map[int32]partition.Lag{
			0: partition.NewLag(
				90,  // startOffset (committed offset)
				100, // endOffset
				90,  // committedOffset
				10,  // rawLag (matches original Lag field)
			),
			1: partition.NewLag(
				100, // startOffset (committed offset)
				100, // endOffset
				100, // committedOffset
				0,   // rawLag (matches original Lag field)
			),
			2: partition.NewLag(
				100, // startOffset (committed offset)
				333, // endOffset
				100, // committedOffset
				233, // rawLag (matches original Lag field)
			),
		},
	}

	handler := newStatusPageHandler(mockLister, mockReader, 0)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Verify status code
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status OK; got %v", resp.StatusCode)
	}

	// Verify content type
	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/html" {
		t.Errorf("expected Content-Type text/html; got %v", contentType)
	}

	// Write response body to file for inspection
	err := os.WriteFile("/tmp/generated_status.html", w.Body.Bytes(), 0644)
	if err != nil {
		t.Errorf("failed to write response body to file: %v", err)
	}
}
