package v1

import (
	"time"

	"github.com/grafana/loki/pkg/logproto"
)

//TailResponse represents the http json response to a tail query
type TailResponse struct {
	Stream         *Stream          `json:"stream,omitempty"`
	DroppedStreams []*DroppedStream `json:"droppedStreams,omitempty"`
}

//DroppedStream
type DroppedStream struct {
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
	Labels LabelSet  `json:"labels,omitempty"`
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
