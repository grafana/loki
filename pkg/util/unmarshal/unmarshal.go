package unmarshal

import (
	"io"
	"reflect"
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

	req, err := NewPushRequest(request)
	if err != nil {
		return err
	}
	*r = *req

	return nil
}

// NewPushRequest constructs a logproto.PushRequest from a PushRequest
func NewPushRequest(r loghttp.PushRequest) (*logproto.PushRequest, error) {
	ret := logproto.PushRequest{
		Streams: make([]logproto.Stream, len(r.Streams)),
	}

	for i, s := range r.Streams {
		stream, err := NewStream(s)
		if err != nil {
			return nil, err
		}

		ret.Streams[i] = *stream
	}

	return &ret, nil
}

// NewStream constructs a logproto.Stream from a Stream
func NewStream(s *loghttp.Stream) (*logproto.Stream, error) {
	stream := logproto.Stream{
		Entries: *(*[]logproto.Entry)(unsafe.Pointer(&s.Entries)),
		Labels:  s.Labels.String(),
	}

	for i, entry := range stream.Entries {
		if entry.Labels == "" {
			continue
		}
		// labels in v1 HTTP push endpoint are in json format({"foo":"bar"}) while for proto it is key=value format({foo="bar"})
		// So here we need to convert metadata labels from json to proto format.
		// ToDo(Sandeep): Find a way to either not do the conversion or efficiently do it since
		// metadata labels can be attached to each log line.
		labels := loghttp.LabelSet{}
		if err := labels.UnmarshalJSON(yoloBytes(entry.Labels)); err != nil {
			return nil, err
		}
		stream.Entries[i].Labels = labels.String()
	}
	return &stream, nil
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

func yoloBytes(s string) (b []byte) {
	*(*string)(unsafe.Pointer(&b)) = s
	(*reflect.SliceHeader)(unsafe.Pointer(&b)).Cap = len(s)
	return
}
