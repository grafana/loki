package unmarshal

import (
	"io"
	"unsafe"

	jsoniter "github.com/json-iterator/go"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// Compile-time layout guard for the unsafe []loghttp.LogProtoStream -> []logproto.Stream cast in
// DecodePushRequest below.
//
// loghttp.LogProtoStream is a defined type *over* logproto.Stream ("type LogProtoStream
// logproto.Stream"), not a separate struct declaration, so the two always have identical memory
// layouts and fields added to logproto.Stream (such as SharedStructuredMetadataSets) are picked up
// automatically. Go only permits this pointer conversion while the underlying types stay
// identical, so this line stops compiling the moment LogProtoStream is redeclared as a struct of
// its own and the cast would become undefined behaviour.
//
// Note that LogProtoStream has a custom UnmarshalJSON that only reads the "stream" and "values"
// keys: the JSON push API has no way to express a shared structured metadata pool, so decoded
// streams always leave SharedStructuredMetadataSets nil and every entry's references at 0, which
// is the correct zero value.
var _ = (*logproto.Stream)((*loghttp.LogProtoStream)(nil))

// DecodePushRequest directly decodes json to a logproto.PushRequest
func DecodePushRequest(b io.Reader, r *logproto.PushRequest) error {
	var request loghttp.PushRequest

	if err := jsoniter.NewDecoder(b).Decode(&request); err != nil {
		return err
	}

	*r = logproto.PushRequest{
		Streams: *(*[]logproto.Stream)(unsafe.Pointer(&request.Streams)), //#nosec G103 -- Just preventing an allocation, safe, there's no chance of an incorrect type cast here. -- nosemgrep: use-of-unsafe-block
	}

	return nil
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
