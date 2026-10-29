//go:build !slicelabels && !dedupelabels

package logs

import "github.com/grafana/loki/v3/pkg/dataobj/internal/util/symbolizer"

// metadataValue returns value as a string for a label builder.
//
// The string aliases value without a copy. This is safe because stringlabels
// copies every value into one string when it builds the labels, so the alias
// never outlives the call. The symbolizer is not used for the same reason.
func metadataValue(_ *symbolizer.Symbolizer, value []byte) string {
	return unsafeString(value)
}
