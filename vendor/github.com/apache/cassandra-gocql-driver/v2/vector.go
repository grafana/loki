/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package gocql

import (
	"bytes"
	"errors"
	"fmt"
	"math/bits"
	"reflect"
	"strconv"
)

type vectorCQLType struct {
	types *RegisteredTypes
}

// Params returns nil
func (vectorCQLType) Params(int) []interface{} {
	// we don't support frame params for custom types
	return nil
}

// TypeInfoFromParams builds a TypeInfo implementation for the composite type with
// the given parameters.
func (vectorCQLType) TypeInfoFromParams(int, []interface{}) (TypeInfo, error) {
	return nil, errors.New("unsupported for vector")
}

// TypeInfoFromString builds the VectorType from the given string.
func (v vectorCQLType) TypeInfoFromString(proto int, name string) (TypeInfo, error) {
	params := splitCompositeTypes(name)
	if len(params) != 2 {
		return nil, fmt.Errorf("expected 2 params for vector, got %d", len(params))
	}
	subType, err := v.types.typeInfoFromString(proto, params[0])
	if err != nil {
		return nil, err
	}
	dim, _ := strconv.Atoi(params[1])
	return VectorType{
		SubType:    subType,
		Dimensions: dim,
	}, nil
}

// VectorType represents a Cassandra vector type, which stores an array of values
// with a fixed dimension. It's commonly used for machine learning applications and
// similarity searches. The SubType defines the element type and Dimensions specifies
// the fixed size of the vector.
type VectorType struct {
	SubType    TypeInfo
	Dimensions int
}

func (VectorType) Type() Type {
	return TypeCustom
}

// Zero returns the zero value for the vector CQL type.
func (v VectorType) Zero() interface{} {
	return reflect.Zero(reflect.SliceOf(reflect.TypeOf(v.SubType.Zero()))).Interface()
}

func (t VectorType) String() string {
	return fmt.Sprintf("vector(%s, %d)", t.SubType, t.Dimensions)
}

// Marshal marshals the value into a byte slice.
func (v VectorType) Marshal(value interface{}) ([]byte, error) {
	if value == nil {
		return nil, nil
	} else if _, ok := value.(unsetColumn); ok {
		return nil, nil
	}

	rv := reflect.ValueOf(value)
	t := rv.Type()
	k := t.Kind()
	if k == reflect.Slice && rv.IsNil() {
		return nil, nil
	}

	switch k {
	case reflect.Slice, reflect.Array:
		buf := &bytes.Buffer{}
		n := rv.Len()
		if n != v.Dimensions {
			return nil, marshalErrorf("expected vector with %d dimensions, received %d", v.Dimensions, n)
		}

		for i := 0; i < n; i++ {
			item, err := Marshal(v.SubType, rv.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			if isVectorVariableLengthType(v.SubType) {
				writeUnsignedVInt(buf, uint64(len(item)))
			}
			buf.Write(item)
		}
		return buf.Bytes(), nil
	}
	return nil, marshalErrorf("can not marshal %T into %s. Accepted types: slice, array.", value, v)
}

// Unmarshal unmarshals the byte slice into the value.
func (v VectorType) Unmarshal(data []byte, value interface{}) error {
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Ptr {
		return unmarshalErrorf("can not unmarshal into non-pointer %T", value)
	}
	rv = rv.Elem()
	t := rv.Type()
	if t.Kind() == reflect.Interface {
		if t.NumMethod() != 0 {
			return unmarshalErrorf("can not unmarshal into non-empty interface %T", value)
		}
		t = reflect.TypeOf(v.Zero())
	}

	k := t.Kind()
	switch k {
	case reflect.Slice, reflect.Array:
		if data == nil {
			if k == reflect.Array {
				return unmarshalErrorf("unmarshal vector: can not store nil in array value")
			}
			if rv.IsNil() {
				return nil
			}
			rv.Set(reflect.Zero(t))
			return nil
		}
		if k == reflect.Array {
			if rv.Len() != v.Dimensions {
				return unmarshalErrorf("unmarshal vector: array of size %d cannot store vector of %d dimensions", rv.Len(), v.Dimensions)
			}
		} else {
			rv.Set(reflect.MakeSlice(t, v.Dimensions, v.Dimensions))
			if rv.Kind() == reflect.Interface {
				rv = rv.Elem()
			}
		}
		elemSize := len(data) / v.Dimensions
		for i := 0; i < v.Dimensions; i++ {
			offset := 0
			if isVectorVariableLengthType(v.SubType) {
				m, p, err := readUnsignedVInt(data)
				if err != nil {
					return err
				}
				elemSize = int(m)
				offset = p
			}
			if offset > 0 {
				data = data[offset:]
			}
			var unmarshalData []byte
			if elemSize >= 0 {
				if len(data) < elemSize {
					return unmarshalErrorf("unmarshal vector: unexpected eof")
				}
				unmarshalData = data[:elemSize]
				data = data[elemSize:]
			}
			err := Unmarshal(v.SubType, unmarshalData, rv.Index(i).Addr().Interface())
			if err != nil {
				return unmarshalErrorf("failed to unmarshal %s into %T: %s", v.SubType, unmarshalData, err.Error())
			}
		}
		return nil
	}
	return unmarshalErrorf("can not unmarshal %s into %T. Accepted types: *slice, *array, *interface{}.", v, value)
}

// isVectorVariableLengthType determines if a type requires explicit length serialization within a vector.
// Variable-length types need their length encoded before the actual data to allow proper deserialization.
// Fixed-length types, on the other hand, don't require this kind of length prefix.
func isVectorVariableLengthType(elemType TypeInfo) bool {
	switch elemType.Type() {
	case TypeBigInt, TypeBoolean, TypeTimestamp, TypeDouble, TypeFloat, TypeInt,
		TypeTimeUUID, TypeUUID:
		return false
	case TypeCustom:
		// vectors are special in that they rely on the underlying type
		if vecType, ok := elemType.(VectorType); ok {
			return isVectorVariableLengthType(vecType.SubType)
		}
	}
	return true
}

func writeUnsignedVInt(buf *bytes.Buffer, v uint64) {
	numBytes := computeUnsignedVIntSize(v)
	if numBytes <= 1 {
		buf.WriteByte(byte(v))
		return
	}

	extraBytes := numBytes - 1
	var tmp = make([]byte, numBytes)
	for i := extraBytes; i >= 0; i-- {
		tmp[i] = byte(v)
		v >>= 8
	}
	tmp[0] |= byte(^(0xff >> uint(extraBytes)))
	buf.Write(tmp)
}

func readUnsignedVInt(data []byte) (uint64, int, error) {
	if len(data) <= 0 {
		return 0, 0, errors.New("unexpected eof")
	}
	firstByte := data[0]
	if firstByte&0x80 == 0 {
		return uint64(firstByte), 1, nil
	}
	numBytes := bits.LeadingZeros32(uint32(^firstByte)) - 24
	ret := uint64(firstByte & (0xff >> uint(numBytes)))
	if len(data) < numBytes+1 {
		return 0, 0, fmt.Errorf("data expect to have %d bytes, but it has only %d", numBytes+1, len(data))
	}
	for i := 0; i < numBytes; i++ {
		ret <<= 8
		ret |= uint64(data[i+1] & 0xff)
	}
	return ret, numBytes + 1, nil
}

func computeUnsignedVIntSize(v uint64) int {
	lead0 := bits.LeadingZeros64(v)
	return (639 - lead0*9) >> 6
}
