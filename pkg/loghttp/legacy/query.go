package loghttp

import (
	"time"
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
