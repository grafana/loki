//go:build slicelabels || dedupelabels

package logs

import "github.com/grafana/loki/v3/pkg/dataobj/internal/util/symbolizer"

// metadataValue returns an owned copy of value for a label builder, because
// these label implementations keep the strings they receive.
//
// If sym is not nil, metadataValue reuses an earlier string with the same
// content.
func metadataValue(sym *symbolizer.Symbolizer, value []byte) string {
	if sym != nil {
		return sym.Get(unsafeString(value))
	}
	return string(value)
}
