package unmarshal

import (
	"encoding/json"
	"io"

	"github.com/grafana/loki/pkg/loghttp"

	"github.com/grafana/loki/pkg/logproto"
)

// DecodePushRequest directly decodes json to a logproto.PushRequest
func DecodePushRequest(b io.Reader, r *logproto.PushRequest) error {
	var request loghttp.PushRequest

	err := json.NewDecoder(b).Decode(&request)

	if err != nil {
		return err
	}

	*r = NewPushRequest(request)

	return nil
}

// NewPushRequest constructs a logproto.PushRequest from a PushRequest
func NewPushRequest(r loghttp.PushRequest) logproto.PushRequest {
	ret := logproto.PushRequest{
		Streams: make([]*logproto.Stream, len(r.Streams)),
	}

	for i, s := range r.Streams {
		ret.Streams[i] = NewStream(s)
	}

	return ret
}

// NewStream constructs a logproto.Stream from a Stream
func NewStream(s *loghttp.Stream) *logproto.Stream {
	ret := &logproto.Stream{
		Entries: make([]logproto.Entry, len(s.Entries)),
		Labels:  s.Labels.String(),
	}

	for i, e := range s.Entries {
		ret.Entries[i] = NewEntry(e)
	}

	return ret
}

// NewEntry constructs a logproto.Entry from a Entry
func NewEntry(e loghttp.Entry) logproto.Entry {
	return logproto.Entry{
		Timestamp: e.Timestamp,
		Line:      e.Line,
	}
}
