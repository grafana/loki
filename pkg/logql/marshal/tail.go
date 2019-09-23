package marshal

import (
	"github.com/grafana/loki/pkg/loghttp"
	legacy "github.com/grafana/loki/pkg/loghttp/legacy"
)

// NewTailResponse constructs a TailResponse from a legacy.TailResponse
func NewTailResponse(r legacy.TailResponse) (loghttp.TailResponse, error) {
	var err error
	ret := loghttp.TailResponse{
		Streams:        make([]loghttp.Stream, len(r.Streams)),
		DroppedStreams: make([]loghttp.DroppedStream, len(r.DroppedEntries)),
	}

	for i, s := range r.Streams {
		ret.Streams[i], err = NewStream(&s)

		if err != nil {
			return loghttp.TailResponse{}, err
		}
	}

	for i, d := range r.DroppedEntries {
		ret.DroppedStreams[i], err = NewDroppedStream(&d)
		if err != nil {
			return loghttp.TailResponse{}, err
		}
	}

	return ret, nil
}

// NewDroppedStream constructs a DroppedStream from a legacy.DroppedEntry
func NewDroppedStream(s *legacy.DroppedEntry) (loghttp.DroppedStream, error) {
	l, err := NewLabelSet(s.Labels)
	if err != nil {
		return loghttp.DroppedStream{}, err
	}

	return loghttp.DroppedStream{
		Timestamp: s.Timestamp,
		Labels:    l,
	}, nil
}
