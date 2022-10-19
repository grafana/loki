// +build !launchdarkly_easyjson

package jwriter

// This function tells the writer tests that we shouldn't expect to see hex escape sequences in the output.
// Our default implementation doesn't use them, whereas easyjson does; either way is valid in JSON.
func tokenWriterWillEncodeAsHex(ch rune) bool {
	return false
}
