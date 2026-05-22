//go:build js

package logqlmodel

// Minimal WASM shim for logqlmodel.
// The full server-side logqlmodel.go pulls in pkg/push (→ gogo/protobuf, grpc,
// k8s.io/apimachinery, cbor) and pkg/logqlmodel/stats (→ gogo/protobuf) via
// fields on Result and Streams. The analyzer's WASM call graph
// (pkg/logqlanalyzer → pkg/logql/{log,syntax}) only references the constants
// and the error helpers in error.go — not Result or Streams. Stripping those
// types here drops ~1000 transitive symbols and several MB from the binary.

// ValueTypeStreams is promql.ValueType for log streams.
const ValueTypeStreams = "streams"

// PackedEntryKey is a special JSON key used by the pack promtail stage and unpack parser.
const PackedEntryKey = "_entry"
