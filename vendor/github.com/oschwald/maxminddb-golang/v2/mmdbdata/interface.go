package mmdbdata

// Unmarshaler is implemented by types that can unmarshal MaxMind DB data. The
// Decoder and iterators returned by it are valid only for the duration of
// UnmarshalMaxMindDB and must not be retained by the implementation. Cursor
// values returned by Decoder.Cursor are the exception; they follow the backing
// lifetime documented for Cursor.
//
// Implementations control their own traversal and allocation. When decoding an
// untrusted database, nested calls should share one aggregate per-record work
// budget covering recursion, collection entries, repeated pointer targets, and
// produced string or byte payloads. The reflection decoder's expansion guard
// is not applied inside this callback.
//
// Deprecated: Implement CursorUnmarshaler instead. Unmarshaler remains
// supported throughout v2 but is planned for removal in v3.
type Unmarshaler interface {
	UnmarshalMaxMindDB(d *Decoder) error
}

// CursorUnmarshaler is implemented by generated and handwritten decoders that
// can unmarshal directly from a Cursor. Implementations must consume and
// validate the complete value and return its proven successor. Direct cursor
// kind mismatches should be passed to NormalizeUnmarshalError before adding any
// wrapping context so Reader.Decode retains its documented error categories.
// When a type implements both CursorUnmarshaler and Unmarshaler, reflection
// decoding invokes CursorUnmarshaler.
//
// Implementations control their own traversal and allocation. When decoding an
// untrusted database, nested calls should share one aggregate per-record work
// budget covering recursion, collection entries, repeated pointer targets, and
// produced string or byte payloads. The reflection decoder's expansion guard
// is not applied inside this callback.
type CursorUnmarshaler interface {
	UnmarshalMaxMindDBCursor(cursor Cursor) (Cursor, error)
}
