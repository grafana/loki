package legacy

import (
	"time"

	"github.com/grafana/loki/pkg/logproto"
)

type DroppedEntry struct {
	Timestamp time.Time
	Labels    string
}

//TailResponse represents the http json response to a tail query
type TailResponse struct {
	Streams        []logproto.Stream `json:"streams"`
	DroppedEntries []DroppedEntry    `json:"dropped_entries"`
}
