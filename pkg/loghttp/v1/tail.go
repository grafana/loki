package v1

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/grafana/loki/pkg/loghttp/legacy"
)

//TailResponse represents the http json response to a tail query
type TailResponse struct {
	Streams        []Stream         `json:"streams,omitempty"` // jpe - remove omitempty and write test
	DroppedStreams []*DroppedStream `json:"dropped_entries,omitempty"`
}

//DroppedStream
type DroppedStream struct {
	Timestamp time.Time
	Labels    LabelSet
}

//MarshalJSON converts an Entry object to be prom compatible for http queries
func (s *DroppedStream) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Timestamp string   `json:"timestamp"`
		Labels    LabelSet `json:"labels,omitempty"`
	}{
		Timestamp: fmt.Sprintf("%d", s.Timestamp.UnixNano()),
		Labels:    s.Labels,
	})
}

func NewTailResponse(r legacy.TailResponse) (TailResponse, error) {
	var err error
	ret := TailResponse{
		Streams:        make([]Stream, len(r.Streams)),
		DroppedStreams: make([]*DroppedStream, len(r.DroppedEntries)),
	}

	for i, s := range r.Streams {
		ret.Streams[i], err = NewStream(&s)

		if err != nil {
			return TailResponse{}, err
		}
	}

	for i, d := range r.DroppedEntries {
		ret.DroppedStreams[i], err = NewDroppedStream(&d)
		if err != nil {
			return TailResponse{}, err
		}
	}

	return ret, nil
}

func NewDroppedStream(s *legacy.DroppedEntry) (*DroppedStream, error) {
	l, err := NewLabelSet(s.Labels)
	if err != nil {
		return nil, err
	}

	return &DroppedStream{
		Timestamp: s.Timestamp,
		Labels:    l,
	}, nil
}
