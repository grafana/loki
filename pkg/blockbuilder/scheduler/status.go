package scheduler

import (
	"context"
	_ "embed"
	"html/template"
	"net/http"
	"slices"
	"time"

	"github.com/dustin/go-humanize"
)

//go:embed status.gohtml
var defaultPageContent string
var defaultPageTemplate = template.Must(template.New("webpage").Funcs(template.FuncMap{
	"durationSince": func(t time.Time) string { return time.Since(t).Truncate(time.Second).String() },
	"offsetsLen":    func(min, max int64) int64 { return max - min },
	"humanize":      humanize.Comma,
}).Parse(defaultPageContent))

type jobQueue interface {
	ListPendingJobs() []JobWithMetadata
	ListInProgressJobs() []JobWithMetadata
	ListCompletedJobs() []JobWithMetadata
}

type partitionInfo struct {
	Partition       int32
	Lag             int64
	EndOffset       int64
	CommittedOffset int64
}

type statusPageHandler struct {
	jobQueue             jobQueue
	offsetReader         OffsetReader
	fallbackOffsetMillis int64
}

func newStatusPageHandler(jobQueue jobQueue, offsetReader OffsetReader, fallbackOffsetMillis int64) *statusPageHandler {
	return &statusPageHandler{jobQueue: jobQueue, offsetReader: offsetReader, fallbackOffsetMillis: fallbackOffsetMillis}
}

func (h *statusPageHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	offsets, err := h.offsetReader.GroupLag(context.Background(), h.fallbackOffsetMillis)
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
		CompletedJobs  []JobWithMetadata
		Now            time.Time
		PartitionInfo  []partitionInfo
	}{
		Now:            time.Now(),
		PendingJobs:    pendingJobs,
		InProgressJobs: inProgressJobs,
		CompletedJobs:  h.jobQueue.ListCompletedJobs(),
	}

	for partition, l := range offsets {
		// only include partitions having lag that are in retention
		lag := l.Lag()
		if lag <= 0 {
			continue
		}

		data.PartitionInfo = append(data.PartitionInfo, partitionInfo{
			Partition:       partition,
			Lag:             lag,
			EndOffset:       l.NextAvailableOffset(),
			CommittedOffset: l.LastCommittedOffset(),
		})
	}
	slices.SortFunc(data.PartitionInfo, func(a, b partitionInfo) int {
		return int(a.Partition - b.Partition)
	})

	w.Header().Set("Content-Type", "text/html")
	if err := defaultPageTemplate.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
