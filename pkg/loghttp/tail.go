package loghttp

import (
	time "time"

	"github.com/grafana/loki/pkg/logproto"
)

// jpe - add comments about why this exists

type DroppedEntry struct {
	Timestamp time.Time
	Labels    string
}

// TailResponse holds response sent by tailer
type TailResponse struct {
	Streams        []logproto.Stream `json:"streams"`
	DroppedEntries []DroppedEntry    `json:"dropped_entries"`
}
