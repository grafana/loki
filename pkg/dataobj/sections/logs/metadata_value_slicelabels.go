//go:build slicelabels || dedupelabels

package logs

// metadataValue returns an owned copy of value for a label builder, because
// these label implementations keep the strings they receive.
func metadataValue(value []byte) string {
	return string(value)
}
