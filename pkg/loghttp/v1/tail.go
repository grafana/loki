package v1

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/grafana/loki/pkg/logproto"
)

//TailResponse represents the http json response to a tail query
type TailResponse struct {
	Stream         *Stream          `json:"entry,omitempty"` // jpe - remove omitempty and write test
	DroppedStreams []*DroppedStream `json:"dropped_entries,omitempty"`
}

//DroppedStream
type DroppedStream struct {
	From   time.Time
	To     time.Time
	Labels LabelSet
}

//MarshalJSON converts an Entry object to be prom compatible for http queries
func (s *DroppedStream) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		From   string   `json:"from"`
		To     string   `json:"to"`
		Labels LabelSet `json:"labels,omitempty"`
	}{
		From:   fmt.Sprintf("%d", s.From.UnixNano()),
		To:     fmt.Sprintf("%d", s.To.UnixNano()),
		Labels: s.Labels,
	})
}

func NewTailResponse(r logproto.TailResponse) (TailResponse, error) {
	s, err := NewStream(r.Stream)
	if err != nil {
		return TailResponse{}, err
	}

	new := TailResponse{
		Stream:         &s,
		DroppedStreams: make([]*DroppedStream, len(r.DroppedStreams)),
	}

	for i, d := range r.DroppedStreams {
		new.DroppedStreams[i], err = NewDroppedStream(d)
		if err != nil {
			return TailResponse{}, err
		}
	}

	return new, nil
}

func NewDroppedStream(s *logproto.DroppedStream) (*DroppedStream, error) {
	l, err := NewLabelSet(s.Labels)
	if err != nil {
		return nil, err
	}

	return &DroppedStream{
		From:   s.From,
		To:     s.To,
		Labels: l,
	}, nil
}
