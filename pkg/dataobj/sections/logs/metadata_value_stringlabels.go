//go:build !slicelabels && !dedupelabels

package logs

// metadataValue returns value as a string for a label builder.
//
// The string aliases value without a copy. This is safe because stringlabels
// copies every value into one string when it builds the labels, so the alias
// never outlives the call.
func metadataValue(value []byte) string {
	return unsafeString(value)
}
