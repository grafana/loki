package legacy

import (
	"time"

	"github.com/grafana/loki/pkg/logproto"
)

// QueryResponse represents the http json response to a label query
type QueryResponse struct {
	Streams []*Stream `json:"streams,omitempty"`
}

// Stream represents a log stream.  It includes a set of log entries and their labels.
type Stream struct {
	Labels  string  `json:"labels"`
	Entries []Entry `json:"entries"`
}

// Entry represents a log entry.  It includes a log message and the time it occurred at.
type Entry struct {
	Timestamp time.Time `json:"ts"`
	Line      string    `json:"line"`
}

// NewStream constructs a Stream from a logproto.Stream
func NewStream(s *logproto.Stream) *Stream {
	ret := &Stream{
		Labels:  s.Labels,
		Entries: make([]Entry, len(s.Entries)),
	}

	for i, e := range s.Entries {
		ret.Entries[i] = NewEntry(e)
	}

	return ret
}

// NewEntry constructs an Entry from a logproto.Entry
func NewEntry(e logproto.Entry) Entry {
	return Entry{
		Timestamp: e.Timestamp,
		Line:      e.Line,
	}
}
