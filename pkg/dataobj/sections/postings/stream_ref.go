package postings

// StreamRef identifies a stream in postings rows by object path and stream ID.
//
// Stream IDs are only unique within a source logs object, so callers that join
// across postings rows from multiple objects must keep object context.
type StreamRef struct {
	ObjectPath string
	StreamID   int64
}
