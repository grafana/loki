package v1

import "time"

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
