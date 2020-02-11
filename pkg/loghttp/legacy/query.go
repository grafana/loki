package loghttp

import (
	model "github.com/grafana/loki/model"
)

// QueryResponse represents the http json response to a label query
type QueryResponse struct {
	Streams []*Stream `json:"streams,omitempty"`
}

// Stream represents a log stream.  It includes a set of log entries and their labels.
type Stream struct {
	Labels  string  `json:"labels"`
	Entries []model.Entry `json:"entries"`
}
