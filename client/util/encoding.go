package util

// Re-export encoding utilities from the main v3 module.
// NOTE: This package depends on github.com/prometheus/prometheus/tsdb/encoding
// which is a lightweight TSDB package (not the full Prometheus server).
// This maintains code reuse while accepting a minimal Prometheus dependency.

import (
	"github.com/grafana/loki/v3/pkg/util/encoding"
)

// Encbuf extends encoding.Encbuf with support for multi byte encoding
// This is a type alias to the v3 module's Encbuf to maintain code reuse.
type Encbuf = encoding.Encbuf

// EncWith creates a new Encbuf with the given byte slice
func EncWith(b []byte) Encbuf {
	return encoding.EncWith(b)
}

// Decbuf extends encoding.Decbuf with support for multi byte decoding
// This is a type alias to the v3 module's Decbuf to maintain code reuse.
type Decbuf = encoding.Decbuf

// DecWith creates a new Decbuf with the given byte slice
func DecWith(b []byte) Decbuf {
	return encoding.DecWith(b)
}

// Note: EncWrap and DecWrap are not re-exported because they have type
// compatibility issues when re-exporting. If you need these functions,
// import them directly from the v3 module:
//   import "github.com/grafana/loki/v3/pkg/util/encoding"
