package loghttp

import (
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// DroppedEntry represents a dropped entry in a tail call
type DroppedEntry struct {
	Timestamp time.Time
	Labels    string
}

// TailResponse represents the http json response to a tail query
type TailResponse struct {
	Streams        []logproto.Stream `json:"streams"`
	DroppedEntries []DroppedEntry    `json:"dropped_entries"`
}
