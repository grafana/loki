// +build launchdarkly_easyjson

package jwriter

import (
	ejwriter "github.com/mailru/easyjson/jwriter"
)

// NewWriterFromEasyJSONWriter creates a Writer that produces JSON output through the specified easyjson
// jwriter.Writer.
//
// This function is only available in code that was compiled with the build tag "launchdarkly_easyjson".
// Its purpose is to allow custom marshaling code that is based on the Reader API to be used as
// efficiently as possible within other data structures that are being marshaled with easyjson.
// Directly using the same Writer that is already being used is more efficient than producing an
// intermediate byte slice and then passing that data to the Writer.
func NewWriterFromEasyJSONWriter(writer *ejwriter.Writer) Writer {
	return Writer{
		tw: newTokenWriterFromEasyjsonWriter(writer),
	}
}
