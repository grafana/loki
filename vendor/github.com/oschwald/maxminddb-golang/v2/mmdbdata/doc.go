// Package mmdbdata provides low-level types and interfaces for custom MaxMind DB decoding.
//
// This package allows custom decoding logic for applications that need fine-grained
// control over how MaxMind DB data is processed. For most use cases, the high-level
// maxminddb.Reader API is recommended instead.
//
// # Manual Decoding Example
//
// New custom types should implement CursorUnmarshaler:
//
//	type Label string
//
//	func (label *Label) UnmarshalMaxMindDBCursor(
//		cursor mmdbdata.Cursor,
//	) (mmdbdata.Cursor, error) {
//		value, next, err := cursor.ReadString()
//		if err != nil {
//			return mmdbdata.Cursor{}, mmdbdata.NormalizeUnmarshalError[Label](err)
//		}
//		*label = Label(value)
//		return next, nil
//	}
//
// Types implementing CursorUnmarshaler automatically use custom decoding logic
// instead of reflection when decoded by maxminddb.Reader. The older Unmarshaler
// interface remains supported throughout v2 but is deprecated and planned for
// removal in v3. When a type implements both interfaces, CursorUnmarshaler takes
// precedence.
//
// The Cursor, MapReader, MapCursor, and SliceCursor APIs support code generated
// by the maxminddb-gen command. Scalar and container reads use opaque successor
// cursors to prove that a complete value was consumed without walking it a
// second time. Applications should normally generate this code rather than use
// the cursor API directly. Handwritten CursorUnmarshaler implementations return
// that successor so they can also decode nested fields without rescanning.
// Bridging a legacy Unmarshaler through Cursor.Unmarshal requires a rescan to
// determine its successor.
//
// # Direct Decoder Usage
//
// For even more control, you can use the Decoder directly:
//
//	decoder := mmdbdata.NewDecoder(buffer, offset)
//	value, err := decoder.ReadString()
//	if err != nil {
//		return err
//	}
package mmdbdata
