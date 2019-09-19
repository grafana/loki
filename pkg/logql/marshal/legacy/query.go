package marshal

import "github.com/grafana/loki/pkg/logproto"

// NewStream constructs a Stream from a logproto.Stream
func NewStream(s *logproto.Stream) *logproto.Stream {
	ret := &logproto.Stream{
		Labels:  s.Labels,
		Entries: make([]logproto.Entry, len(s.Entries)),
	}

	for i, e := range s.Entries {
		ret.Entries[i] = NewEntry(e)
	}

	return ret
}

// NewEntry constructs an Entry from a logproto.Entry
func NewEntry(e logproto.Entry) logproto.Entry {
	return logproto.Entry{
		Timestamp: e.Timestamp,
		Line:      e.Line,
	}
}
