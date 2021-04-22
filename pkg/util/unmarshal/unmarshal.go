package unmarshal

import (
	"io"
	"unsafe"

	jsoniter "github.com/json-iterator/go"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
)

// DecodePushRequest directly decodes json to a logproto.PushRequest
func DecodePushRequest(b io.Reader, r *logproto.PushRequest) error {
	var request loghttp.PushRequest

	if err := jsoniter.NewDecoder(b).Decode(&request); err != nil {
		return err
	}
	*r = NewPushRequest(request)

	return nil
}

// NewPushRequest constructs a logproto.PushRequest from a PushRequest
func NewPushRequest(r loghttp.PushRequest) logproto.PushRequest {
	ret := logproto.PushRequest{
		Streams: make([]logproto.Stream, len(r.Streams)),
	}

	for i, s := range r.Streams {
		ret.Streams[i] = NewStream(s)
	}

	return ret
}

// NewStream constructs a logproto.Stream from a Stream
func NewStream(s *loghttp.Stream) logproto.Stream {
	return logproto.Stream{
		Entries: *(*[]logproto.Entry)(unsafe.Pointer(&s.Entries)),
		Labels:  s.Labels.String(),
	}
}

// WebsocketReader knows how to read message to a websocket connection.
type WebsocketReader interface {
	ReadMessage() (int, []byte, error)
}

// ReadTailResponseJSON unmarshals the loghttp.TailResponse from a websocket reader.
func ReadTailResponseJSON(r *loghttp.TailResponse, reader WebsocketReader) error {
	_, data, err := reader.ReadMessage()
	if err != nil {
		return err
	}
	return jsoniter.Unmarshal(data, r)
}
