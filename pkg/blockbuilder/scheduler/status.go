package scheduler

import (
	"context"
	_ "embed"
	"html/template"
	"net/http"
	"slices"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
)

//go:embed status.gohtml
var defaultPageContent string
var defaultPageTemplate = template.Must(template.New("webpage").Funcs(template.FuncMap{
	"durationSince": func(t time.Time) string { return time.Since(t).Truncate(time.Second).String() },
}).Parse(defaultPageContent))

type jobQueue interface {
	ListPendingJobs() []JobWithMetadata
	ListInProgressJobs() []JobWithMetadata
}

type offsetReader interface {
	GroupLag(ctx context.Context, lookbackPeriod time.Duration) (map[int32]kadm.GroupMemberLag, error)
}

type partitionInfo struct {
	Partition       int32
	Lag             int64
	EndOffset       int64
	CommittedOffset int64
}

type statusPageHandler struct {
	jobQueue       jobQueue
	offsetReader   offsetReader
	lookbackPeriod time.Duration
}

func newStatusPageHandler(jobQueue jobQueue, offsetReader offsetReader, lookbackPeriod time.Duration) *statusPageHandler {
	return &statusPageHandler{jobQueue: jobQueue, offsetReader: offsetReader, lookbackPeriod: lookbackPeriod}
}

func (h *statusPageHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	offsets, err := h.offsetReader.GroupLag(context.Background(), h.lookbackPeriod)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	pendingJobs := h.jobQueue.ListPendingJobs()
	slices.SortFunc(pendingJobs, func(a, b JobWithMetadata) int {
		return b.Priority - a.Priority // Higher priority first
	})

	inProgressJobs := h.jobQueue.ListInProgressJobs()
	slices.SortFunc(inProgressJobs, func(a, b JobWithMetadata) int {
		return int(a.StartTime.Sub(b.StartTime)) // Earlier start time First
	})

	data := struct {
		PendingJobs    []JobWithMetadata
		InProgressJobs []JobWithMetadata
		Now            time.Time
		PartitionInfo  []partitionInfo
	}{
		Now:            time.Now(),
		PendingJobs:    pendingJobs,
		InProgressJobs: inProgressJobs,
	}

	for _, partitionOffset := range offsets {
		// only include partitions having lag
		if partitionOffset.Lag > 0 {
			data.PartitionInfo = append(data.PartitionInfo, partitionInfo{
				Partition:       partitionOffset.Partition,
				Lag:             partitionOffset.Lag,
				EndOffset:       partitionOffset.End.Offset,
				CommittedOffset: partitionOffset.Commit.At,
			})
		}
	}
	slices.SortFunc(data.PartitionInfo, func(a, b partitionInfo) int {
		return int(a.Partition - b.Partition)
	})

	w.Header().Set("Content-Type", "text/html")
	if err := defaultPageTemplate.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
