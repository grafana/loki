package kmsg

import "github.com/twmb/franz-go/pkg/kmsg/internal/kbin"

// A Record is a Kafka v0.11.0.0 record. It corresponds to an individual
// message as it is written on the wire.
type Record struct {
	// Length is the length of this record on the wire of everything that
	// follows this field. It is an int32 encoded as a varint.
	Length int32

	// Attributes are record level attributes. This field currently is unused.
	Attributes int8

	// TimestampDelta is the millisecond delta of this record's timestamp
	// from the record's RecordBatch's FirstTimestamp.
	//
	// NOTE: this is actually an int64 but we cannot change the type for
	// backwards compatibility. Use TimestampDelta64.
	TimestampDelta   int32
	TimestampDelta64 int64

	// OffsetDelta is the delta of this record's offset from the record's
	// RecordBatch's FirstOffset.
	//
	// For producing, this is usually equal to the index of the record in
	// the record batch.
	OffsetDelta int32

	// Key is an blob of data for a record.
	//
	// Key's are usually used for hashing the record to specific Kafka partitions.
	Key []byte

	// Value is a blob of data. This field is the main "message" portion of a
	// record.
	Value []byte

	// Headers are optional user provided metadata for records. Unlike normal
	// arrays, the number of headers is encoded as a varint.
	Headers []Header
}

func (v *Record) AppendTo(dst []byte) []byte {
	{
		v := v.Length
		dst = kbin.AppendVarint(dst, v)
	}
	{
		v := v.Attributes
		dst = kbin.AppendInt8(dst, v)
	}
	{
		d := v.TimestampDelta64
		if d == 0 {
			d = int64(v.TimestampDelta)
		}
		dst = kbin.AppendVarlong(dst, d)
	}
	{
		v := v.OffsetDelta
		dst = kbin.AppendVarint(dst, v)
	}
	{
		v := v.Key
		dst = kbin.AppendVarintBytes(dst, v)
	}
	{
		v := v.Value
		dst = kbin.AppendVarintBytes(dst, v)
	}
	{
		v := v.Headers
		dst = kbin.AppendVarint(dst, int32(len(v)))
		for i := range v {
			v := &v[i]
			{
				v := v.Key
				dst = kbin.AppendVarintString(dst, v)
			}
			{
				v := v.Value
				dst = kbin.AppendVarintBytes(dst, v)
			}
		}
	}
	return dst
}

func (v *Record) ReadFrom(src []byte) error {
	return v.readFrom(src, false)
}

func (v *Record) UnsafeReadFrom(src []byte) error {
	return v.readFrom(src, true)
}

func (v *Record) readFrom(src []byte, unsafe bool) error {
	v.Default()
	b := kbin.Reader{Src: src}
	s := v
	{
		v := b.Varint()
		s.Length = v
	}
	{
		v := b.Int8()
		s.Attributes = v
	}
	{
		v := b.Varlong()
		s.TimestampDelta64 = v
		s.TimestampDelta = int32(v)
	}
	{
		v := b.Varint()
		s.OffsetDelta = v
	}
	{
		v := b.VarintBytes()
		s.Key = v
	}
	{
		v := b.VarintBytes()
		s.Value = v
	}
	{
		v := s.Headers
		a := v
		var l int32
		l = b.VarintArrayLen()
		if !b.Ok() {
			return b.Complete()
		}
		a = a[:0]
		if l > 0 {
			a = append(a, make([]Header, l)...)
		}
		for i := int32(0); i < l; i++ {
			v := &a[i]
			v.Default()
			s := v
			{
				var v string
				if unsafe {
					v = b.UnsafeVarintString()
				} else {
					v = b.VarintString()
				}
				s.Key = v
			}
			{
				v := b.VarintBytes()
				s.Value = v
			}
		}
		v = a
		s.Headers = v
	}
	return b.Complete()
}

// Default sets any default fields. Calling this allows for future compatibility
// if new fields are added to Record.
func (v *Record) Default() {
}

// NewRecord returns a default Record
// This is a shortcut for creating a struct and calling Default yourself.
func NewRecord() Record {
	var v Record
	v.Default()
	return v
}
