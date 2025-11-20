package types

// This package provides type aliases and documentation for Loki push API types.
//
// The actual push types are defined in github.com/grafana/loki/pkg/push,
// which is a separate module with minimal dependencies. This package provides
// convenient re-exports and documentation.
//
// To use push types in your code:
//
//	import (
//		"github.com/grafana/loki/pkg/push"
//	)
//
//	req := &push.PushRequest{
//		Streams: []push.Stream{
//			{
//				Labels: `{job="test"}`,
//				Entries: []push.Entry{
//					{
//						Timestamp: time.Now(),
//						Line:      "log line",
//					},
//				},
//			},
//		},
//	}

// PushRequest represents a request to push logs to Loki.
// Re-exported from github.com/grafana/loki/pkg/push for convenience.
type PushRequest = interface {
	// Streams contains the log streams to push
	Streams() []Stream
	// Format indicates the ingestion format (loki or otlp)
	Format() string
}

// Stream represents a log stream with labels and entries.
// Re-exported from github.com/grafana/loki/pkg/push for convenience.
type Stream = interface {
	// Labels is the label set as a string (e.g., `{job="test"}`)
	Labels() string
	// Entries contains the log entries for this stream
	Entries() []Entry
	// Hash is the hash of the stream labels
	Hash() uint64
}

// Entry represents a single log entry.
// Re-exported from github.com/grafana/loki/pkg/push for convenience.
type Entry = interface {
	// Timestamp is when the log entry was created
	Timestamp() interface{} // time.Time
	// Line is the log line content
	Line() string
	// StructuredMetadata contains structured metadata labels
	StructuredMetadata() []LabelAdapter
}

// LabelAdapter represents a label key-value pair.
// Re-exported from github.com/grafana/loki/pkg/push for convenience.
type LabelAdapter = interface {
	// Name is the label name
	Name() string
	// Value is the label value
	Value() string
}
