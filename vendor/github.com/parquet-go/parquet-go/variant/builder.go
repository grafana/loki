package variant

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/google/uuid"
)

// ValueBuilder is a streaming event sink for variant values. Callers describe
// a value as a sequence of events — scalars, or containers bracketed by
// Begin/End calls — and implementations consume the events without ever
// materializing the value as a tree.
//
// Exactly one value must be written: a single scalar event, or a single
// balanced container. Inside an object, every value must be preceded by a
// Field call naming it, and field names must be unique within their object
// (duplicates are an error). Errors (both misuse of the event sequence and
// implementation-specific failures) are sticky and reported by Err; events
// after an error are ignored.
//
// ValueBuilder is implemented by Builder, which encodes the events to variant
// binary, and by parquet.VariantColumnWriter, which shreds the events
// directly into parquet column buffers.
type ValueBuilder interface {
	Null()
	Bool(v bool)
	Int8(v int8)
	Int16(v int16)
	Int32(v int32)
	Int64(v int64)
	Float(v float32)
	Double(v float64)
	String(v string)
	Binary(v []byte)
	Date(days int32)
	Time(micros int64)
	Timestamp(micros int64)
	TimestampNTZ(micros int64)
	TimestampNanos(nanos int64)
	TimestampNTZNanos(nanos int64)
	UUID(v uuid.UUID)
	Decimal4(unscaled int32, scale byte)
	Decimal8(unscaled int64, scale byte)
	Decimal16(unscaled [16]byte, scale byte)

	// BeginObject starts an object value. Calls until the matching EndObject
	// must alternate Field and value events.
	BeginObject()

	// Field names the next value written as a field of the innermost open
	// object.
	Field(name string)

	// EndObject closes the innermost open object.
	EndObject()

	// BeginArray starts an array value. Every value written until the
	// matching EndArray is one element.
	BeginArray()

	// EndArray closes the innermost open array.
	EndArray()

	// Err returns the first error encountered, or nil.
	Err() error
}

// Write replays the value as a sequence of events on w. It is the bridge
// from the tree representation to any ValueBuilder.
func (v Value) Write(w ValueBuilder) {
	switch v.basic {
	case BasicObject:
		w.BeginObject()
		for _, f := range v.object.Fields {
			w.Field(f.Name)
			f.Value.Write(w)
		}
		w.EndObject()
	case BasicArray:
		w.BeginArray()
		for _, e := range v.array.Elements {
			e.Write(w)
		}
		w.EndArray()
	case BasicShortString:
		w.String(v.str)
	default:
		switch v.primitive {
		case PrimitiveNull:
			w.Null()
		case PrimitiveTrue:
			w.Bool(true)
		case PrimitiveFalse:
			w.Bool(false)
		case PrimitiveInt8:
			w.Int8(int8(v.i64))
		case PrimitiveInt16:
			w.Int16(int16(v.i64))
		case PrimitiveInt32:
			w.Int32(int32(v.i64))
		case PrimitiveInt64:
			w.Int64(v.i64)
		case PrimitiveFloat:
			w.Float(float32(v.f64))
		case PrimitiveDouble:
			w.Double(v.f64)
		case PrimitiveString:
			w.String(v.str)
		case PrimitiveBinary:
			w.Binary(v.bytes)
		case PrimitiveDate:
			w.Date(int32(v.i64))
		case PrimitiveTime:
			w.Time(v.i64)
		case PrimitiveTimestamp:
			w.Timestamp(v.i64)
		case PrimitiveTimestampNTZ:
			w.TimestampNTZ(v.i64)
		case PrimitiveTimestampNanos:
			w.TimestampNanos(v.i64)
		case PrimitiveTimestampNTZNanos:
			w.TimestampNTZNanos(v.i64)
		case PrimitiveUUID:
			w.UUID(v.uuid)
		case PrimitiveDecimal4:
			w.Decimal4(int32(v.i64), v.scale)
		case PrimitiveDecimal8:
			w.Decimal8(v.i64, v.scale)
		case PrimitiveDecimal16:
			w.Decimal16(v.decimal16, v.scale)
		default:
			w.Null()
		}
	}
}

// Builder is a streaming encoder for variant values: a ValueBuilder that
// encodes events directly to the variant binary format in a single growing
// buffer, without building a Value tree.
//
// Values are encoded as their events arrive. When a container closes, its
// header (field IDs, offsets) is spliced in front of the already-encoded
// children with one memmove, so encoding is a single pass with no per-value
// allocations beyond buffer growth. Object field values are kept in event
// order and only the header entries are sorted by field name, which the
// encoding permits (field IDs must be sorted, field offsets need not be
// monotonic).
//
// The zero value is ready to use and owns its metadata dictionary; Finish
// returns both the metadata and the value. NewBuilderWithMetadata creates a
// builder that interns field names into a shared, caller-owned dictionary,
// which is how multiple values (e.g. all residuals of one shredded row)
// share one dictionary.
//
// A Builder may be reused after Reset. Reset retains internal buffers, so a
// long-lived Builder encodes a stream of values with amortized zero
// allocation. A Builder must not be copied after first use.
type Builder struct {
	meta    *MetadataBuilder
	ownMeta MetadataBuilder

	buf     []byte
	scratch []byte

	frames []builderFrame
	fields []builderField // arena shared by all open object frames
	elems  []int          // arena shared by all open array frames

	pendingField bool // inside an object, a Field awaits its value
	complete     bool // one complete top-level value has been written
	err          error
}

type builderFrame struct {
	isObject   bool
	start      int // buf offset where the container's first child begins
	fieldsBase int // objects: base of this frame's entries in the fields arena
	elemsBase  int // arrays: base of this frame's element starts in the elems arena
}

type builderField struct {
	id    int
	start int // buf offset where the field's value begins
}

// NewBuilderWithMetadata returns a Builder that interns object field names
// into the given shared metadata dictionary instead of an internal one.
// Finish builds the shared dictionary; callers encoding multiple values
// against one dictionary typically use Bytes and build the metadata
// themselves once.
func NewBuilderWithMetadata(meta *MetadataBuilder) *Builder {
	return &Builder{meta: meta}
}

func (b *Builder) metadata() *MetadataBuilder {
	if b.meta == nil {
		b.meta = &b.ownMeta
	}
	return b.meta
}

// Err returns the first error encountered, or nil.
func (b *Builder) Err() error { return b.err }

func (b *Builder) fail(format string, args ...any) {
	if b.err == nil {
		b.err = fmt.Errorf("variant builder: "+format, args...)
	}
}

// Reset clears the builder for reuse, retaining internal buffers. If the
// builder owns its metadata dictionary the dictionary is cleared too; a
// shared dictionary (NewBuilderWithMetadata) is left untouched.
func (b *Builder) Reset() {
	if b.meta == &b.ownMeta {
		b.ownMeta.Reset()
	}
	b.buf = b.buf[:0]
	b.frames = b.frames[:0]
	b.fields = b.fields[:0]
	b.elems = b.elems[:0]
	b.pendingField = false
	b.complete = false
	b.err = nil
}

// Finish returns the encoded metadata and value. It fails if no value or an
// incomplete value has been written. The value slice aliases the builder's
// internal buffer and is invalidated by Reset or further writes, the same
// as Bytes; the metadata slice is freshly allocated.
func (b *Builder) Finish() (metadata, value []byte, err error) {
	value, err = b.Bytes()
	if err != nil {
		return nil, nil, err
	}
	meta := b.metadata()
	// Dictionary offsets are at most 4 bytes wide; MetadataBuilder.AppendTo
	// would silently truncate a larger dictionary.
	if uint64(meta.Size()) > math.MaxUint32 {
		return nil, nil, fmt.Errorf("variant builder: metadata dictionary of %d bytes exceeds the variant format's 4 GiB limit", meta.Size())
	}
	metadata = meta.AppendTo(nil)
	return metadata, value, nil
}

// Bytes returns the encoded value bytes without building the metadata. The
// returned slice aliases the builder's internal buffer and is invalidated by
// Reset or further writes.
func (b *Builder) Bytes() ([]byte, error) {
	switch {
	case b.err != nil:
		return nil, b.err
	case len(b.frames) > 0:
		return nil, errors.New("variant builder: unclosed object or array")
	case !b.complete:
		return nil, errors.New("variant builder: no value written")
	}
	return b.buf, nil
}

// beginValue validates that a value event is legal in the current state and
// records array element boundaries. It reports whether to proceed.
func (b *Builder) beginValue() bool {
	if b.err != nil {
		return false
	}
	if n := len(b.frames); n == 0 {
		if b.complete {
			b.fail("more than one top-level value")
			return false
		}
	} else if f := &b.frames[n-1]; f.isObject {
		if !b.pendingField {
			b.fail("value written inside an object without a Field call")
			return false
		}
		b.pendingField = false
	} else {
		b.elems = append(b.elems, len(b.buf))
	}
	return true
}

func (b *Builder) endValue() {
	if len(b.frames) == 0 {
		b.complete = true
	}
}

func (b *Builder) appendScalar(data ...byte) {
	if b.beginValue() {
		b.buf = append(b.buf, data...)
		b.endValue()
	}
}

func (b *Builder) Null() { b.appendScalar(makeHeader(BasicPrimitive, byte(PrimitiveNull))) }

func (b *Builder) Bool(v bool) {
	p := PrimitiveFalse
	if v {
		p = PrimitiveTrue
	}
	b.appendScalar(makeHeader(BasicPrimitive, byte(p)))
}

func (b *Builder) Int8(v int8) {
	b.appendScalar(makeHeader(BasicPrimitive, byte(PrimitiveInt8)), byte(v))
}

func (b *Builder) Int16(v int16) { b.scalar16(PrimitiveInt16, uint16(v)) }

func (b *Builder) Int32(v int32) { b.scalar32(PrimitiveInt32, uint32(v)) }

func (b *Builder) Int64(v int64) { b.scalar64(PrimitiveInt64, uint64(v)) }

func (b *Builder) Float(v float32) { b.scalar32(PrimitiveFloat, math.Float32bits(v)) }

func (b *Builder) Double(v float64) { b.scalar64(PrimitiveDouble, math.Float64bits(v)) }

func (b *Builder) String(v string) {
	if !b.beginValue() {
		return
	}
	if uint64(len(v)) > math.MaxUint32 {
		b.fail("string of %d bytes exceeds the variant format's 4 GiB limit", len(v))
		return
	}
	if len(v) <= 63 {
		b.buf = append(b.buf, makeHeader(BasicShortString, byte(len(v))))
	} else {
		b.buf = append(b.buf, makeHeader(BasicPrimitive, byte(PrimitiveString)), 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(b.buf[len(b.buf)-4:], uint32(len(v)))
	}
	b.buf = append(b.buf, v...)
	b.endValue()
}

func (b *Builder) Binary(v []byte) {
	if !b.beginValue() {
		return
	}
	if uint64(len(v)) > math.MaxUint32 {
		b.fail("binary value of %d bytes exceeds the variant format's 4 GiB limit", len(v))
		return
	}
	b.buf = append(b.buf, makeHeader(BasicPrimitive, byte(PrimitiveBinary)), 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(b.buf[len(b.buf)-4:], uint32(len(v)))
	b.buf = append(b.buf, v...)
	b.endValue()
}

func (b *Builder) Date(days int32) { b.scalar32(PrimitiveDate, uint32(days)) }

func (b *Builder) Time(micros int64) { b.scalar64(PrimitiveTime, uint64(micros)) }

func (b *Builder) Timestamp(micros int64) { b.scalar64(PrimitiveTimestamp, uint64(micros)) }

func (b *Builder) TimestampNTZ(micros int64) { b.scalar64(PrimitiveTimestampNTZ, uint64(micros)) }

func (b *Builder) TimestampNanos(nanos int64) { b.scalar64(PrimitiveTimestampNanos, uint64(nanos)) }

func (b *Builder) TimestampNTZNanos(nanos int64) {
	b.scalar64(PrimitiveTimestampNTZNanos, uint64(nanos))
}

func (b *Builder) UUID(v uuid.UUID) {
	if !b.beginValue() {
		return
	}
	b.buf = append(b.buf, makeHeader(BasicPrimitive, byte(PrimitiveUUID)))
	b.buf = append(b.buf, v[:]...)
	b.endValue()
}

func (b *Builder) Decimal4(unscaled int32, scale byte) {
	if !b.beginValue() {
		return
	}
	b.buf = append(b.buf, makeHeader(BasicPrimitive, byte(PrimitiveDecimal4)), scale, 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(b.buf[len(b.buf)-4:], uint32(unscaled))
	b.endValue()
}

func (b *Builder) Decimal8(unscaled int64, scale byte) {
	if !b.beginValue() {
		return
	}
	b.buf = append(b.buf, makeHeader(BasicPrimitive, byte(PrimitiveDecimal8)), scale, 0, 0, 0, 0, 0, 0, 0, 0)
	binary.LittleEndian.PutUint64(b.buf[len(b.buf)-8:], uint64(unscaled))
	b.endValue()
}

func (b *Builder) Decimal16(unscaled [16]byte, scale byte) {
	if !b.beginValue() {
		return
	}
	b.buf = append(b.buf, makeHeader(BasicPrimitive, byte(PrimitiveDecimal16)), scale)
	b.buf = append(b.buf, unscaled[:]...)
	b.endValue()
}

func (b *Builder) scalar16(p PrimitiveType, v uint16) {
	if !b.beginValue() {
		return
	}
	b.buf = append(b.buf, makeHeader(BasicPrimitive, byte(p)), 0, 0)
	binary.LittleEndian.PutUint16(b.buf[len(b.buf)-2:], v)
	b.endValue()
}

func (b *Builder) scalar32(p PrimitiveType, v uint32) {
	if !b.beginValue() {
		return
	}
	b.buf = append(b.buf, makeHeader(BasicPrimitive, byte(p)), 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(b.buf[len(b.buf)-4:], v)
	b.endValue()
}

func (b *Builder) scalar64(p PrimitiveType, v uint64) {
	if !b.beginValue() {
		return
	}
	b.buf = append(b.buf, makeHeader(BasicPrimitive, byte(p)), 0, 0, 0, 0, 0, 0, 0, 0)
	binary.LittleEndian.PutUint64(b.buf[len(b.buf)-8:], v)
	b.endValue()
}

func (b *Builder) BeginObject() {
	if !b.beginValue() {
		return
	}
	b.frames = append(b.frames, builderFrame{
		isObject:   true,
		start:      len(b.buf),
		fieldsBase: len(b.fields),
	})
}

func (b *Builder) Field(name string) {
	if b.err != nil {
		return
	}
	n := len(b.frames)
	if n == 0 || !b.frames[n-1].isObject {
		b.fail("Field %q outside of an object", name)
		return
	}
	if b.pendingField {
		b.fail("Field %q: previous field has no value", name)
		return
	}
	id := b.metadata().Add(name)
	b.fields = append(b.fields, builderField{
		id:    id,
		start: len(b.buf),
	})
	b.pendingField = true
}

func (b *Builder) EndObject() {
	if b.err != nil {
		return
	}
	n := len(b.frames)
	if n == 0 || !b.frames[n-1].isObject {
		b.fail("EndObject without matching BeginObject")
		return
	}
	meta := b.metadata()
	if b.pendingField {
		b.fail("EndObject: field %q has no value", meta.stringAt(b.fields[len(b.fields)-1].id))
		return
	}
	f := b.frames[n-1]
	entries := b.fields[f.fieldsBase:]

	// The encoding requires field IDs sorted lexicographically by name;
	// field values stay where they were written, offsets need not be
	// monotonic. Names are resolved from the metadata slab at close time
	// so Field does not retain views that a later Add could invalidate.
	slices.SortFunc(entries, func(a, c builderField) int {
		return strings.Compare(meta.stringAt(a.id), meta.stringAt(c.id))
	})
	for i := 1; i < len(entries); i++ {
		// Names are interned, so equal names always share one id.
		if entries[i].id == entries[i-1].id {
			b.fail("duplicate object field %q", meta.stringAt(entries[i].id))
			return
		}
	}

	numFields := len(entries)
	contentSize := len(b.buf) - f.start
	// Offsets are at most 4 bytes wide, so a container whose encoded
	// children exceed math.MaxUint32 bytes is unrepresentable; failing
	// beats the silent truncation of writeUint. Field offsets and counts
	// are bounded by contentSize, so this one check covers them too.
	if uint64(contentSize) > math.MaxUint32 {
		b.fail("object content of %d bytes exceeds the variant format's 4 GiB limit", contentSize)
		return
	}
	maxID := 0
	for i := range entries {
		if entries[i].id > maxID {
			maxID = entries[i].id
		}
	}

	fieldIDSzCode := offsetSizeCode(maxID)
	fieldIDSz := offsetSize(fieldIDSzCode)
	offsetSzCode := offsetSizeCode(contentSize)
	offsetSz := offsetSize(offsetSzCode)

	isLarge := byte(0)
	numElemSize := 1
	if numFields > 255 {
		isLarge = 1
		numElemSize = 4
	}

	headerSize := 1 + numElemSize + numFields*fieldIDSz + (numFields+1)*offsetSz
	h := b.header(headerSize)
	h[0] = byte(BasicObject) | (offsetSzCode << 2) | (fieldIDSzCode << 4) | (isLarge << 6)
	pos := 1
	if isLarge == 1 {
		binary.LittleEndian.PutUint32(h[pos:], uint32(numFields))
		pos += 4
	} else {
		h[pos] = byte(numFields)
		pos++
	}
	for i := range entries {
		writeUint(h[pos:], entries[i].id, fieldIDSz)
		pos += fieldIDSz
	}
	for i := range entries {
		writeUint(h[pos:], entries[i].start-f.start, offsetSz)
		pos += offsetSz
	}
	writeUint(h[pos:], contentSize, offsetSz)

	b.splice(f.start, h)
	b.fields = b.fields[:f.fieldsBase]
	b.frames = b.frames[:n-1]
	b.endValue()
}

func (b *Builder) BeginArray() {
	if !b.beginValue() {
		return
	}
	b.frames = append(b.frames, builderFrame{
		start:     len(b.buf),
		elemsBase: len(b.elems),
	})
}

func (b *Builder) EndArray() {
	if b.err != nil {
		return
	}
	n := len(b.frames)
	if n == 0 || b.frames[n-1].isObject {
		b.fail("EndArray without matching BeginArray")
		return
	}
	f := b.frames[n-1]
	starts := b.elems[f.elemsBase:]

	numElems := len(starts)
	contentSize := len(b.buf) - f.start
	// See the matching check in EndObject.
	if uint64(contentSize) > math.MaxUint32 {
		b.fail("array content of %d bytes exceeds the variant format's 4 GiB limit", contentSize)
		return
	}

	offsetSzCode := offsetSizeCode(contentSize)
	offsetSz := offsetSize(offsetSzCode)

	isLarge := byte(0)
	numElemSize := 1
	if numElems > 255 {
		isLarge = 1
		numElemSize = 4
	}

	headerSize := 1 + numElemSize + (numElems+1)*offsetSz
	h := b.header(headerSize)
	h[0] = byte(BasicArray) | (offsetSzCode << 2) | (isLarge << 4)
	pos := 1
	if isLarge == 1 {
		binary.LittleEndian.PutUint32(h[pos:], uint32(numElems))
		pos += 4
	} else {
		h[pos] = byte(numElems)
		pos++
	}
	for _, start := range starts {
		writeUint(h[pos:], start-f.start, offsetSz)
		pos += offsetSz
	}
	writeUint(h[pos:], contentSize, offsetSz)

	b.splice(f.start, h)
	b.elems = b.elems[:f.elemsBase]
	b.frames = b.frames[:n-1]
	b.endValue()
}

// header returns a zeroed scratch slice of the given size for building a
// container header.
func (b *Builder) header(size int) []byte {
	if cap(b.scratch) < size {
		b.scratch = make([]byte, size)
	}
	h := b.scratch[:size]
	clear(h)
	return h
}

// splice inserts header at position start, shifting the container's encoded
// children right by one memmove.
func (b *Builder) splice(start int, header []byte) {
	n := len(header)
	b.buf = append(b.buf, header...)
	copy(b.buf[start+n:], b.buf[start:len(b.buf)-n])
	copy(b.buf[start:], header)
}
