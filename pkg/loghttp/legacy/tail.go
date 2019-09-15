package legacy

import (
	"time"

	"github.com/grafana/loki/pkg/logproto"
)

//TailResponse represents the http json response to a tail query
type TailResponse struct {
	Stream         *Stream          `json:"stream,omitempty"`
	DroppedStreams []*DroppedStream `json:"droppedStreams,omitempty"`
}

type DroppedStream struct {
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
	Labels string    `json:"labels,omitempty"`
}

func NewTailResponse(r logproto.TailResponse) TailResponse {
	new := TailResponse{
		Stream:         NewStream(r.Stream),
		DroppedStreams: make([]*DroppedStream, len(r.DroppedStreams)),
	}

	for _, d := range r.DroppedStreams {
		new.DroppedStreams = append(new.DroppedStreams, NewDroppedStream(d))
	}

	return new
}

func NewDroppedStream(s *logproto.DroppedStream) *DroppedStream {
	return &DroppedStream{
		From:   s.From,
		To:     s.To,
		Labels: s.Labels,
	}
}
