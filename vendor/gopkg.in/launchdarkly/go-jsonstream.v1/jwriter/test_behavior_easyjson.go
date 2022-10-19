// +build launchdarkly_easyjson

package jwriter

// This function tells the writer tests that we should expect to see hex escape sequences in the output
// for certain characters, because that's the behavior of easyjson.
func tokenWriterWillEncodeAsHex(ch rune) bool {
	return ch != '\t' && ch != '\n' && ch != '\r'
}
