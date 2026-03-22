package molecule

import (
	"fmt"

	"github.com/richardartoul/molecule/src/codec"
)

// MessageEachFn is a function that will be called for each top-level field in a
// message passed to MessageEach.
type MessageEachFn func(fieldNum int32, value Value) (bool, error)

// MessageEach iterates over each top-level field in the message stored in buffer
// and calls fn on each one.
func MessageEach(buffer *codec.Buffer, fn MessageEachFn) error {
	for !buffer.EOF() {
		v, err := buffer.DecodeVarint()
		if err != nil {
			return err
		}
		fieldNum, wireType, err := codec.AsTagAndWireType(v)
		if err != nil {
			return err
		}

		value := Value{
			WireType: wireType,
		}

		switch wireType {
		case codec.WireVarint:
			value.Number, err = buffer.DecodeVarint()
		case codec.WireFixed32:
			value.Number, err = buffer.DecodeFixed32()
		case codec.WireFixed64:
			value.Number, err = buffer.DecodeFixed64()
		case codec.WireBytes:
			value.Bytes, err = buffer.DecodeRawBytes(false)
		case codec.WireStartGroup, codec.WireEndGroup:
			err = fmt.Errorf("MessageEach: encountered group wire type: %d. Groups not supported", wireType)
		default:
			err = fmt.Errorf("MessageEach: unknown wireType: %d", wireType)
		}

		if err != nil {
			return fmt.Errorf("MessageEach: error reading value from buffer: %v", err)
		}

		shouldContinue, err := fn(fieldNum, value)
		if err != nil || !shouldContinue {
			return err
		}
	}
	return nil
}

// Next populates the given value with the next value in the field and returns the field number or an error if one
// was encountered while reading the next field value
func Next(buffer *codec.Buffer, value *Value) (fieldNum int32, err error) {
	var v uint64
	var wireType codec.WireType
	v, err = buffer.DecodeVarint()
	if err != nil {
		return
	}

	fieldNum, wireType, err = codec.AsTagAndWireType(v)
	if err != nil {
		return
	}

	value.WireType = wireType

	switch wireType {
	case codec.WireVarint:
		value.Number, err = buffer.DecodeVarint()
	case codec.WireFixed32:
		value.Number, err = buffer.DecodeFixed32()
	case codec.WireFixed64:
		value.Number, err = buffer.DecodeFixed64()
	case codec.WireBytes:
		value.Bytes, err = buffer.DecodeRawBytes(false)
	case codec.WireStartGroup, codec.WireEndGroup:
		err = fmt.Errorf("MessageEach: encountered group wire type: %d. Groups not supported", wireType)
	default:
		err = fmt.Errorf("MessageEach: unknown wireType: %d", wireType)
	}

	if err != nil {
		err = fmt.Errorf("MessageEach: error reading value from buffer: %v", err)
		return
	}

	return
}

// PackedRepeatedEachFn is a function that is called for each value in a repeated field.
type PackedRepeatedEachFn func(value Value) (bool, error)

// PackedRepeatedEach iterates over each value in the packed repeated field stored in buffer
// and calls fn on each one.
//
// The fieldType argument should match the type of the value stored in the repeated field.
//
// PackedRepeatedEach only supports repeated fields encoded using packed encoding.
func PackedRepeatedEach(buffer *codec.Buffer, fieldType codec.FieldType, fn PackedRepeatedEachFn) error {
	var wireType codec.WireType
	switch fieldType {
	case codec.FieldType_INT32,
		codec.FieldType_INT64,
		codec.FieldType_UINT32,
		codec.FieldType_UINT64,
		codec.FieldType_SINT32,
		codec.FieldType_SINT64,
		codec.FieldType_BOOL,
		codec.FieldType_ENUM:
		wireType = codec.WireVarint
	case codec.FieldType_FIXED64,
		codec.FieldType_SFIXED64,
		codec.FieldType_DOUBLE:
		wireType = codec.WireFixed64
	case codec.FieldType_FIXED32,
		codec.FieldType_SFIXED32,
		codec.FieldType_FLOAT:
		wireType = codec.WireFixed32
	case codec.FieldType_STRING,
		codec.FieldType_MESSAGE,
		codec.FieldType_BYTES:
		wireType = codec.WireBytes
	default:
		return fmt.Errorf(
			"PackedRepeatedEach: unknown field type: %v", fieldType)
	}

	for !buffer.EOF() {
		var err error
		value := Value{
			WireType: wireType,
		}

		switch wireType {
		case codec.WireVarint:
			value.Number, err = buffer.DecodeVarint()
		case codec.WireFixed32:
			value.Number, err = buffer.DecodeFixed32()
		case codec.WireFixed64:
			value.Number, err = buffer.DecodeFixed64()
		case codec.WireBytes:
			value.Bytes, err = buffer.DecodeRawBytes(false)
		case codec.WireStartGroup, codec.WireEndGroup:
			err = fmt.Errorf("PackedRepeatedEach: encountered group wire type: %d. Groups not supported", wireType)
		default:
			err = fmt.Errorf("PackedRepeatedEach: unknown wireType: %d", wireType)
		}

		if err != nil {
			return fmt.Errorf("PackedRepeatedEach: error reading value from buffer: %v", err)
		}

		if shouldContinue, err := fn(value); err != nil || !shouldContinue {
			return err
		}
	}

	return nil
}
