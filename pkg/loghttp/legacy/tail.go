package legacy

import "time"

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
