package mmdbdata

import "github.com/oschwald/maxminddb-golang/v2/internal/decoder"

// Kind represents MMDB data kinds.
type Kind = decoder.Kind

// KindSet is an immutable set of MMDB kinds. Contains reports membership and
// String returns a stable human-readable representation.
//
//	func (KindSet) Contains(Kind) bool
//	func (KindSet) String() string
type KindSet = decoder.KindSet

// Decoder provides stateful methods for decoding MMDB data:
//
//	func (*Decoder) Cursor() Cursor
//	func (*Decoder) Advance(Cursor) error
//	func (*Decoder) ReadBool() (bool, error)
//	func (*Decoder) ReadString() (string, error)
//	func (*Decoder) ReadBytes() ([]byte, error)
//	func (*Decoder) ReadFloat32() (float32, error)
//	func (*Decoder) ReadFloat64() (float64, error)
//	func (*Decoder) ReadInt32() (int32, error)
//	func (*Decoder) ReadUint16() (uint16, error)
//	func (*Decoder) ReadUint32() (uint32, error)
//	func (*Decoder) ReadUint64() (uint64, error)
//	func (*Decoder) ReadUint128() (high uint64, low uint64, err error)
//	func (*Decoder) ReadMap() (iter.Seq2[[]byte, error], uint, error)
//	func (*Decoder) ReadSlice() (iter.Seq[error], uint, error)
//	func (*Decoder) SkipValue() error
//	func (*Decoder) PeekKind() (Kind, error)
//	func (*Decoder) Offset() uint
//
// Cursor returns a cursor at the decoder's current value. Advance accepts only
// a proven successor of that value, such as one returned by a successful cursor
// scalar read or completed container traversal. ReadBytes and keys yielded by
// ReadMap are read-only slices that alias the decoder input; copy their contents
// before retaining or modifying them.
type Decoder = decoder.Decoder

// Cursor identifies a value in MMDB data and allows a decoder to return a
// proven successor without rescanning that value. Its zero value is invalid.
// Successful scalar reads, Skip, Unmarshal, and UnmarshalCursor return a proven
// successor for the complete input value; container iterators require those
// successors to advance safely. ReadMapKey instead returns a cursor positioned
// at the corresponding map value. Obtain an initial cursor from
// NewDecoder(...).Cursor().
//
// Cursors obtained during maxminddb.Reader decoding remain backed by that
// Reader. This includes cursors passed to CursorUnmarshaler or returned by
// Decoder.Cursor during legacy Unmarshaler callbacks, as well as successor
// cursors and MapReader, MapCursor, and SliceCursor handles derived from them.
// None may be used after or concurrently with the Reader's Close method.
// Cursors obtained from NewDecoder are backed by its caller-provided buffer.
//
// Cursor provides these operations:
//
//	func (Cursor) Kind() (Kind, error)
//	func (Cursor) Skip() (Cursor, error)
//	func (Cursor) ReadBool() (bool, Cursor, error)
//	func (Cursor) ReadString() (string, Cursor, error)
//	func (Cursor) ReadBytes() ([]byte, Cursor, error)
//	func (Cursor) ReadFloat32() (float32, Cursor, error)
//	func (Cursor) ReadFloat64() (float64, Cursor, error)
//	func (Cursor) ReadFloat() (float64, Cursor, error)
//	func (Cursor) ReadInt32() (int32, Cursor, error)
//	func (Cursor) ReadUint() (uint64, Cursor, error)
//	func (Cursor) ReadInteger() (value uint64, signed bool, next Cursor, err error)
//	func (Cursor) ReadUint128() (high uint64, low uint64, next Cursor, err error)
//	func (Cursor) Map() (MapCursor, error)
//	func (Cursor) MapReader() (MapReader, error)
//	func (Cursor) Slice() (SliceCursor, error)
//	func (Cursor) ReadMapKey() ([]byte, Cursor, error)
//	func (Cursor) Unmarshal(Unmarshaler) (Cursor, error)
//	func (Cursor) UnmarshalCursor(CursorUnmarshaler) (Cursor, error)
//
// Kind resolves a valid pointer and reports its target kind without consuming
// the cursor.
//
// ReadFloat accepts either MMDB floating-point kind. ReadUint accepts the
// Uint16, Uint32, and Uint64 kinds. ReadInteger additionally accepts Int32 and
// reports signed values as the uint64 bit pattern of their int64 value.
// ReadUint128 returns high and low 64-bit words.
//
// ReadBytes and ReadMapKey return slices that alias the decoder's input buffer.
// They must not be modified and should be copied before being retained. Other
// scalar results do not alias the input. Unmarshal and UnmarshalCursor reject
// nil implementations. UnmarshalCursor validates its callback's returned
// successor; legacy Unmarshal derives a successor by rescanning the value.
type Cursor = decoder.Cursor

// MapReader provides counted map traversal. Its zero value is invalid.
//
//	func (MapReader) Len() uint
//	func (MapReader) Size() (uint, error)
//	func (MapReader) SizeForAllocation() (size uint, ok bool)
//	func (MapReader) First() Cursor
//	func (MapReader) End(Cursor) (Cursor, error)
//
// Len is the declared entry count for traversal. Before using that count as an
// allocation hint, call SizeForAllocation; if it returns false, call Size to
// perform the complete allocation preflight or retrieve a reader error.
//
// Start at First, call Cursor.ReadMapKey and decode its value exactly Len times,
// using each value's successor as the next key cursor. Pass the final cursor to
// End; for an empty map, pass First directly. The caller is responsible for
// supplying the cursor after exactly Len entries. End checks decoder identity
// and that nonempty traversal produced a read successor, but it cannot verify
// map membership or the iteration count.
type MapReader = decoder.MapReader

// MapCursor incrementally reads a map using proven value successors. Its zero
// value is invalid; obtain one by calling Cursor.Map.
//
//	func (*MapCursor) Size() uint
//	func (*MapCursor) Next(Cursor) ([]byte, Cursor, bool)
//	func (*MapCursor) Err() error
//	func (*MapCursor) End() (Cursor, error)
//
// Pass a zero Cursor to the first Next call and the decoded value's successor
// to each later call. Each returned key aliases the decoder input and follows
// the same ownership rules as Cursor.ReadMapKey. After Next returns false, call
// End to report iteration errors and obtain the map successor. Size returns the
// entry count validated when the map was opened.
type MapCursor = decoder.MapCursor

// SliceCursor incrementally reads a slice using proven value successors. Its
// zero value is invalid; obtain one by calling Cursor.Slice.
//
//	func (*SliceCursor) Size() (uint, error)
//	func (*SliceCursor) SizeForCapacity(int) (size uint, ok bool)
//	func (*SliceCursor) Next(Cursor) (uint, Cursor, bool)
//	func (*SliceCursor) Err() error
//	func (*SliceCursor) End() (Cursor, error)
//
// Size validates the declared element count. SizeForCapacity is an allocation
// fast path: when it returns false, call Size to validate the size before
// allocation or retrieve the iterator error. Pass a zero Cursor to the first
// Next call and the decoded element's successor to each later call. After Next
// returns false, call End to report iteration errors and obtain the slice
// successor.
type SliceCursor = decoder.SliceCursor

// UnexpectedKindError is returned when a decoder operation encounters an MMDB
// kind that it does not accept. Its Expected set contains every kind accepted
// by the failed operation. It has these fields:
//
//	Actual Kind      // kind encountered in the MMDB data
//	Expected KindSet // kinds accepted by the failed operation
//
// UnexpectedKindError implements error through Error.
type UnexpectedKindError = decoder.UnexpectedKindError

// DecoderOption configures a Decoder.
type DecoderOption = decoder.DecoderOption

// NewDecoder creates a new Decoder with the given buffer, offset, and options.
// Error messages include contextual offset information.
func NewDecoder(buffer []byte, offset uint, options ...DecoderOption) *Decoder {
	d := decoder.NewDataDecoder(buffer)
	return decoder.NewDecoder(d, offset, options...)
}

// NewKindSet returns a set containing kinds.
func NewKindSet(kinds ...Kind) KindSet {
	return decoder.NewKindSet(kinds...)
}

// Kind constants for MMDB data.
const (
	KindExtended  = decoder.KindExtended
	KindPointer   = decoder.KindPointer
	KindString    = decoder.KindString
	KindFloat64   = decoder.KindFloat64
	KindBytes     = decoder.KindBytes
	KindUint16    = decoder.KindUint16
	KindUint32    = decoder.KindUint32
	KindMap       = decoder.KindMap
	KindInt32     = decoder.KindInt32
	KindUint64    = decoder.KindUint64
	KindUint128   = decoder.KindUint128
	KindSlice     = decoder.KindSlice
	KindContainer = decoder.KindContainer
	KindEndMarker = decoder.KindEndMarker
	KindBool      = decoder.KindBool
	KindFloat32   = decoder.KindFloat32
)
