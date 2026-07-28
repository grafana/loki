package unmarshal

import (
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// Test_DecodePushRequest_StreamLayout guards the unsafe []loghttp.LogProtoStream ->
// []logproto.Stream cast in DecodePushRequest.
//
// loghttp.LogProtoStream is a defined type over logproto.Stream, so the two are guaranteed to
// share a layout and any field added to logproto.Stream is inherited automatically. This test
// makes that guarantee explicit at run time as well, so a future redeclaration of LogProtoStream
// as its own struct cannot silently turn the cast into memory corruption.
func Test_DecodePushRequest_StreamLayout(t *testing.T) {
	httpType := reflect.TypeOf(loghttp.LogProtoStream{})
	protoType := reflect.TypeOf(logproto.Stream{})

	require.Equal(t, unsafe.Sizeof(loghttp.LogProtoStream{}), unsafe.Sizeof(logproto.Stream{}),
		"loghttp.LogProtoStream and logproto.Stream must have the same size for the unsafe cast in DecodePushRequest to be valid")
	require.Equal(t, protoType.NumField(), httpType.NumField())

	for i := 0; i < protoType.NumField(); i++ {
		protoField, httpField := protoType.Field(i), httpType.Field(i)
		require.Equal(t, protoField.Name, httpField.Name)
		require.Equal(t, protoField.Type, httpField.Type)
		require.Equal(t, protoField.Offset, httpField.Offset)
	}
}

// Test_DecodePushRequest_LeavesSharedStructuredMetadataUnset documents that the JSON push API has
// no way to express a shared structured metadata pool, so decoded streams keep the nil zero value
// for the pool, and their entries the 0 "no set" references, rather than whatever the unsafe cast
// happens to alias.
func Test_DecodePushRequest_LeavesSharedStructuredMetadataUnset(t *testing.T) {
	body := `{
		"streams": [{
			"stream": { "foo": "bar" },
			"values": [ [ "123456789012345678", "log line" ] ]
		}]
	}`

	var actual logproto.PushRequest
	require.NoError(t, DecodePushRequest(strings.NewReader(body), &actual))

	require.Len(t, actual.Streams, 1)
	require.Len(t, actual.Streams[0].Entries, 1)
	require.Nil(t, actual.Streams[0].SharedStructuredMetadataSets)
	require.Zero(t, actual.Streams[0].Entries[0].SharedResourceRef)
	require.Zero(t, actual.Streams[0].Entries[0].SharedScopeRef)
}
