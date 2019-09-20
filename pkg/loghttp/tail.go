package loghttp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// TailResponse represents the http json response to a tail query
type TailResponse struct {
	Streams        []Stream        `json:"streams,omitempty"`
	DroppedStreams []DroppedStream `json:"dropped_entries,omitempty"`
}

// DroppedStream represents a dropped stream in tail call
type DroppedStream struct {
	Timestamp time.Time
	Labels    LabelSet
}

// MarshalJSON implements json.Marshaller
func (s *DroppedStream) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Timestamp string   `json:"timestamp"`
		Labels    LabelSet `json:"labels,omitempty"`
	}{
		Timestamp: fmt.Sprintf("%d", s.Timestamp.UnixNano()),
		Labels:    s.Labels,
	})
}

// UnmarshalJSON implements json.UnMarshaller
func (s *DroppedStream) UnmarshalJSON(data []byte) error {
	unmarshal := struct {
		Timestamp string   `json:"timestamp"`
		Labels    LabelSet `json:"labels,omitempty"`
	}{}

	err := json.Unmarshal(data, &unmarshal)

	if err != nil {
		return err
	}

	t, err := strconv.ParseInt(unmarshal.Timestamp, 10, 64)
	if err != nil {
		return err
	}

	s.Timestamp = time.Unix(0, t)
	s.Labels = unmarshal.Labels

	return nil
}
