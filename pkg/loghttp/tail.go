package loghttp

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	json "github.com/json-iterator/go"

	"github.com/grafana/loki/pkg/logproto"
)

const (
	maxDelayForInTailing = 5
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

// ParseTailQuery parses a TailRequest request from an http request.
func ParseTailQuery(r *http.Request) (*logproto.TailRequest, error) {
	var err error
	req := logproto.TailRequest{
		Query: query(r),
	}

	req.Limit, err = limit(r)
	if err != nil {
		return nil, err
	}

	req.Start, _, err = bounds(r)
	if err != nil {
		return nil, err
	}
	req.DelayFor, err = tailDelay(r)
	if err != nil {
		return nil, err
	}
	if req.DelayFor > maxDelayForInTailing {
		return nil, fmt.Errorf("delay_for can't be greater than %d", maxDelayForInTailing)
	}
	return &req, nil
}
