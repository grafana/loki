// Package jwriter provides an efficient mechanism for writing JSON data sequentially.
//
// The high-level API for this package, Writer, is designed to facilitate writing custom JSON
// marshaling logic concisely and reliably. Output is buffered in memory.
//
//     import (
//         "gopkg.in/launchdarkly/jsonstream.v1/jwriter"
//     )
//
//     type myStruct struct {
//         value int
//     }
//
//     func (s myStruct) WriteToJSONWriter(w *jwriter.Writer) {
//         obj := w.Object() // writing a JSON object structure like {"value":2}
//         obj.Property("value").Int(s.value)
//         obj.End()
//     }
//
//     func PrintMyStructJSON(s myStruct) {
//         w := jwriter.NewWriter()
//         s.WriteToJSONWriter(&w)
//         fmt.Println(string(w.Bytes())
//     }
//
// Output can optionally be dumped to an io.Writer at intervals to avoid allocating a large buffer:
//
//     func WriteToHTTPResponse(s myStruct, resp http.ResponseWriter) {
//         resp.Header.Add("Content-Type", "application/json")
//         w := jwriter.NewStreamingWriter(resp, 1000)
//         myStruct.WriteToJSONWriter(&w)
//     }
//
// The underlying low-level token writing mechanism has two available implementations. The default
// implementation has no external dependencies. For interoperability with the easyjson library
// (https://github.com/mailru/easyjson), there is also an implementation that delegates to the
// easyjson streaming writer; this is enabled by setting the build tag "launchdarkly_easyjson".
// Be aware that by default, easyjson uses Go's "unsafe" package (https://pkg.go.dev/unsafe),
// which may not be available on all platforms.
//
// Setting the "launchdarkly_easyjson" tag also adds a new constructor function,
// NewWriterFromEasyJSONWriter, allowing Writer-based code to send output directly to an
// existing EasyJSON jwriter.Writer. This may be desirable in order to define common marshaling
// logic that may be used with or without EasyJSON. For example:
//
//     import (
//         ej_jwriter "github.com/mailru/easyjson/jwriter"
//     )
//
//     func (s myStruct) MarshalEasyJSON(w *ej_jwriter.Writer) {
//         ww := jwriter.NewWriterFromEasyJSONWriter(w)
//         s.WriteToJSONWriter(&ww)
//     }
package jwriter
