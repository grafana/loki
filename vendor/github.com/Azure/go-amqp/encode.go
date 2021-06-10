package amqp

import (
	"encoding/binary"
	"math"
	"time"
	"unicode/utf8"
)

// writesFrame encodes fr into buf.
func writeFrame(buf *buffer, fr frame) error {
	// write header
	buf.write([]byte{
		0, 0, 0, 0, // size, overwrite later
		2,        // doff, see frameHeader.DataOffset comment
		fr.type_, // frame type
	})
	buf.writeUint16(fr.channel) // channel

	// write AMQP frame body
	err := marshal(buf, fr.body)
	if err != nil {
		return err
	}

	// validate size
	if uint(buf.len()) > math.MaxUint32 {
		return errorNew("frame too large")
	}

	// retrieve raw bytes
	bufBytes := buf.bytes()

	// write correct size
	binary.BigEndian.PutUint32(bufBytes, uint32(len(bufBytes)))
	return nil
}

type marshaler interface {
	marshal(*buffer) error
}

func marshal(wr *buffer, i interface{}) error {
	switch t := i.(type) {
	case nil:
		wr.writeByte(byte(typeCodeNull))
	case bool:
		if t {
			wr.writeByte(byte(typeCodeBoolTrue))
		} else {
			wr.writeByte(byte(typeCodeBoolFalse))
		}
	case *bool:
		if *t {
			wr.writeByte(byte(typeCodeBoolTrue))
		} else {
			wr.writeByte(byte(typeCodeBoolFalse))
		}
	case uint:
		writeUint64(wr, uint64(t))
	case *uint:
		writeUint64(wr, uint64(*t))
	case uint64:
		writeUint64(wr, t)
	case *uint64:
		writeUint64(wr, *t)
	case uint32:
		writeUint32(wr, t)
	case *uint32:
		writeUint32(wr, *t)
	case uint16:
		wr.writeByte(byte(typeCodeUshort))
		wr.writeUint16(t)
	case *uint16:
		wr.writeByte(byte(typeCodeUshort))
		wr.writeUint16(*t)
	case uint8:
		wr.write([]byte{
			byte(typeCodeUbyte),
			t,
		})
	case *uint8:
		wr.write([]byte{
			byte(typeCodeUbyte),
			*t,
		})
	case int:
		writeInt64(wr, int64(t))
	case *int:
		writeInt64(wr, int64(*t))
	case int8:
		wr.write([]byte{
			byte(typeCodeByte),
			uint8(t),
		})
	case *int8:
		wr.write([]byte{
			byte(typeCodeByte),
			uint8(*t),
		})
	case int16:
		wr.writeByte(byte(typeCodeShort))
		wr.writeUint16(uint16(t))
	case *int16:
		wr.writeByte(byte(typeCodeShort))
		wr.writeUint16(uint16(*t))
	case int32:
		writeInt32(wr, t)
	case *int32:
		writeInt32(wr, *t)
	case int64:
		writeInt64(wr, t)
	case *int64:
		writeInt64(wr, *t)
	case float32:
		writeFloat(wr, t)
	case *float32:
		writeFloat(wr, *t)
	case float64:
		writeDouble(wr, t)
	case *float64:
		writeDouble(wr, *t)
	case string:
		return writeString(wr, t)
	case *string:
		return writeString(wr, *t)
	case []byte:
		return writeBinary(wr, t)
	case *[]byte:
		return writeBinary(wr, *t)
	case map[interface{}]interface{}:
		return writeMap(wr, t)
	case *map[interface{}]interface{}:
		return writeMap(wr, *t)
	case map[string]interface{}:
		return writeMap(wr, t)
	case *map[string]interface{}:
		return writeMap(wr, *t)
	case map[symbol]interface{}:
		return writeMap(wr, t)
	case *map[symbol]interface{}:
		return writeMap(wr, *t)
	case unsettled:
		return writeMap(wr, t)
	case *unsettled:
		return writeMap(wr, *t)
	case time.Time:
		writeTimestamp(wr, t)
	case *time.Time:
		writeTimestamp(wr, *t)
	case []int8:
		return arrayInt8(t).marshal(wr)
	case *[]int8:
		return arrayInt8(*t).marshal(wr)
	case []uint16:
		return arrayUint16(t).marshal(wr)
	case *[]uint16:
		return arrayUint16(*t).marshal(wr)
	case []int16:
		return arrayInt16(t).marshal(wr)
	case *[]int16:
		return arrayInt16(*t).marshal(wr)
	case []uint32:
		return arrayUint32(t).marshal(wr)
	case *[]uint32:
		return arrayUint32(*t).marshal(wr)
	case []int32:
		return arrayInt32(t).marshal(wr)
	case *[]int32:
		return arrayInt32(*t).marshal(wr)
	case []uint64:
		return arrayUint64(t).marshal(wr)
	case *[]uint64:
		return arrayUint64(*t).marshal(wr)
	case []int64:
		return arrayInt64(t).marshal(wr)
	case *[]int64:
		return arrayInt64(*t).marshal(wr)
	case []float32:
		return arrayFloat(t).marshal(wr)
	case *[]float32:
		return arrayFloat(*t).marshal(wr)
	case []float64:
		return arrayDouble(t).marshal(wr)
	case *[]float64:
		return arrayDouble(*t).marshal(wr)
	case []bool:
		return arrayBool(t).marshal(wr)
	case *[]bool:
		return arrayBool(*t).marshal(wr)
	case []string:
		return arrayString(t).marshal(wr)
	case *[]string:
		return arrayString(*t).marshal(wr)
	case []symbol:
		return arraySymbol(t).marshal(wr)
	case *[]symbol:
		return arraySymbol(*t).marshal(wr)
	case [][]byte:
		return arrayBinary(t).marshal(wr)
	case *[][]byte:
		return arrayBinary(*t).marshal(wr)
	case []time.Time:
		return arrayTimestamp(t).marshal(wr)
	case *[]time.Time:
		return arrayTimestamp(*t).marshal(wr)
	case []UUID:
		return arrayUUID(t).marshal(wr)
	case *[]UUID:
		return arrayUUID(*t).marshal(wr)
	case []interface{}:
		return list(t).marshal(wr)
	case *[]interface{}:
		return list(*t).marshal(wr)
	case marshaler:
		return t.marshal(wr)
	default:
		return errorErrorf("marshal not implemented for %T", i)
	}
	return nil
}

func writeInt32(wr *buffer, n int32) {
	if n < 128 && n >= -128 {
		wr.write([]byte{
			byte(typeCodeSmallint),
			byte(n),
		})
		return
	}

	wr.writeByte(byte(typeCodeInt))
	wr.writeUint32(uint32(n))
}

func writeInt64(wr *buffer, n int64) {
	if n < 128 && n >= -128 {
		wr.write([]byte{
			byte(typeCodeSmalllong),
			byte(n),
		})
		return
	}

	wr.writeByte(byte(typeCodeLong))
	wr.writeUint64(uint64(n))
}

func writeUint32(wr *buffer, n uint32) {
	if n == 0 {
		wr.writeByte(byte(typeCodeUint0))
		return
	}

	if n < 256 {
		wr.write([]byte{
			byte(typeCodeSmallUint),
			byte(n),
		})
		return
	}

	wr.writeByte(byte(typeCodeUint))
	wr.writeUint32(n)
}

func writeUint64(wr *buffer, n uint64) {
	if n == 0 {
		wr.writeByte(byte(typeCodeUlong0))
		return
	}

	if n < 256 {
		wr.write([]byte{
			byte(typeCodeSmallUlong),
			byte(n),
		})
		return
	}

	wr.writeByte(byte(typeCodeUlong))
	wr.writeUint64(n)
}

func writeFloat(wr *buffer, f float32) {
	wr.writeByte(byte(typeCodeFloat))
	wr.writeUint32(math.Float32bits(f))
}

func writeDouble(wr *buffer, f float64) {
	wr.writeByte(byte(typeCodeDouble))
	wr.writeUint64(math.Float64bits(f))
}

func writeTimestamp(wr *buffer, t time.Time) {
	wr.writeByte(byte(typeCodeTimestamp))
	ms := t.UnixNano() / int64(time.Millisecond)
	wr.writeUint64(uint64(ms))
}

// marshalField is a field to be marshaled
type marshalField struct {
	value interface{} // value to be marshaled, use pointers to avoid interface conversion overhead
	omit  bool        // indicates that this field should be omitted (set to null)
}

// marshalComposite is a helper for us in a composite's marshal() function.
//
// The returned bytes include the composite header and fields. Fields with
// omit set to true will be encoded as null or omitted altogether if there are
// no non-null fields after them.
func marshalComposite(wr *buffer, code amqpType, fields []marshalField) error {
	// lastSetIdx is the last index to have a non-omitted field.
	// start at -1 as it's possible to have no fields in a composite
	lastSetIdx := -1

	// marshal each field into it's index in rawFields,
	// null fields are skipped, leaving the index nil.
	for i, f := range fields {
		if f.omit {
			continue
		}
		lastSetIdx = i
	}

	// write header only
	if lastSetIdx == -1 {
		wr.write([]byte{
			0x0,
			byte(typeCodeSmallUlong),
			byte(code),
			byte(typeCodeList0),
		})
		return nil
	}

	// write header
	writeDescriptor(wr, code)

	// write fields
	wr.writeByte(byte(typeCodeList32))

	// write temp size, replace later
	sizeIdx := wr.len()
	wr.write([]byte{0, 0, 0, 0})
	preFieldLen := wr.len()

	// field count
	wr.writeUint32(uint32(lastSetIdx + 1))

	// write null to each index up to lastSetIdx
	for _, f := range fields[:lastSetIdx+1] {
		if f.omit {
			wr.writeByte(byte(typeCodeNull))
			continue
		}
		err := marshal(wr, f.value)
		if err != nil {
			return err
		}
	}

	// fix size
	size := uint32(wr.len() - preFieldLen)
	buf := wr.bytes()
	binary.BigEndian.PutUint32(buf[sizeIdx:], size)

	return nil
}

func writeDescriptor(wr *buffer, code amqpType) {
	wr.write([]byte{
		0x0,
		byte(typeCodeSmallUlong),
		byte(code),
	})
}

func writeString(wr *buffer, str string) error {
	if !utf8.ValidString(str) {
		return errorNew("not a valid UTF-8 string")
	}
	l := len(str)

	switch {
	// Str8
	case l < 256:
		wr.write([]byte{
			byte(typeCodeStr8),
			byte(l),
		})
		wr.writeString(str)
		return nil

	// Str32
	case uint(l) < math.MaxUint32:
		wr.writeByte(byte(typeCodeStr32))
		wr.writeUint32(uint32(l))
		wr.writeString(str)
		return nil

	default:
		return errorNew("too long")
	}
}

func writeBinary(wr *buffer, bin []byte) error {
	l := len(bin)

	switch {
	// List8
	case l < 256:
		wr.write([]byte{
			byte(typeCodeVbin8),
			byte(l),
		})
		wr.write(bin)
		return nil

	// List32
	case uint(l) < math.MaxUint32:
		wr.writeByte(byte(typeCodeVbin32))
		wr.writeUint32(uint32(l))
		wr.write(bin)
		return nil

	default:
		return errorNew("too long")
	}
}

func writeMap(wr *buffer, m interface{}) error {
	startIdx := wr.len()
	wr.write([]byte{
		byte(typeCodeMap32), // type
		0, 0, 0, 0,          // size placeholder
		0, 0, 0, 0, // length placeholder
	})

	var pairs int
	switch m := m.(type) {
	case map[interface{}]interface{}:
		pairs = len(m) * 2
		for key, val := range m {
			err := marshal(wr, key)
			if err != nil {
				return err
			}
			err = marshal(wr, val)
			if err != nil {
				return err
			}
		}
	case map[string]interface{}:
		pairs = len(m) * 2
		for key, val := range m {
			err := writeString(wr, key)
			if err != nil {
				return err
			}
			err = marshal(wr, val)
			if err != nil {
				return err
			}
		}
	case map[symbol]interface{}:
		pairs = len(m) * 2
		for key, val := range m {
			err := key.marshal(wr)
			if err != nil {
				return err
			}
			err = marshal(wr, val)
			if err != nil {
				return err
			}
		}
	case unsettled:
		pairs = len(m) * 2
		for key, val := range m {
			err := writeString(wr, key)
			if err != nil {
				return err
			}
			err = marshal(wr, val)
			if err != nil {
				return err
			}
		}
	case filter:
		pairs = len(m) * 2
		for key, val := range m {
			err := key.marshal(wr)
			if err != nil {
				return err
			}
			err = val.marshal(wr)
			if err != nil {
				return err
			}
		}
	case Annotations:
		pairs = len(m) * 2
		for key, val := range m {
			switch key := key.(type) {
			case string:
				err := symbol(key).marshal(wr)
				if err != nil {
					return err
				}
			case symbol:
				err := key.marshal(wr)
				if err != nil {
					return err
				}
			case int64:
				writeInt64(wr, key)
			case int:
				writeInt64(wr, int64(key))
			default:
				return errorErrorf("unsupported Annotations key type %T", key)
			}

			err := marshal(wr, val)
			if err != nil {
				return err
			}
		}
	default:
		return errorErrorf("unsupported map type %T", m)
	}

	if uint(pairs) > math.MaxUint32-4 {
		return errorNew("map contains too many elements")
	}

	// overwrite placeholder size and length
	bytes := wr.bytes()[startIdx+1 : startIdx+9]
	_ = bytes[7] // bounds check hint

	length := wr.len() - startIdx - 1 - 4 // -1 for type, -4 for length
	binary.BigEndian.PutUint32(bytes[:4], uint32(length))
	binary.BigEndian.PutUint32(bytes[4:8], uint32(pairs))

	return nil
}

// type length sizes
const (
	array8TLSize  = 2
	array32TLSize = 5
)

func writeArrayHeader(wr *buffer, length, typeSize int, type_ amqpType) {
	size := length * typeSize

	// array type
	if size+array8TLSize <= math.MaxUint8 {
		wr.write([]byte{
			byte(typeCodeArray8),      // type
			byte(size + array8TLSize), // size
			byte(length),              // length
			byte(type_),               // element type
		})
	} else {
		wr.writeByte(byte(typeCodeArray32))          //type
		wr.writeUint32(uint32(size + array32TLSize)) // size
		wr.writeUint32(uint32(length))               // length
		wr.writeByte(byte(type_))                    // element type
	}
}

func writeVariableArrayHeader(wr *buffer, length, elementsSizeTotal int, type_ amqpType) {
	// 0xA_ == 1, 0xB_ == 4
	// http://docs.oasis-open.org/amqp/core/v1.0/os/amqp-core-types-v1.0-os.html#doc-idp82960
	elementTypeSize := 1
	if type_&0xf0 == 0xb0 {
		elementTypeSize = 4
	}

	size := elementsSizeTotal + (length * elementTypeSize) // size excluding array length
	if size+array8TLSize <= math.MaxUint8 {
		wr.write([]byte{
			byte(typeCodeArray8),      // type
			byte(size + array8TLSize), // size
			byte(length),              // length
			byte(type_),               // element type
		})
	} else {
		wr.writeByte(byte(typeCodeArray32))          // type
		wr.writeUint32(uint32(size + array32TLSize)) // size
		wr.writeUint32(uint32(length))               // length
		wr.writeByte(byte(type_))                    // element type
	}
}
