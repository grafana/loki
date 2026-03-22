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
/*
 * Content before git sha 34fdeebefcbf183ed7f916f931aa0586fdaa1b40
 * Copyright (c) 2012, The Gocql authors,
 * provided under the BSD-3-Clause License.
 * See the NOTICE file distributed with this work for additional information.
 */

package gocql

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/bits"
	"net"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gopkg.in/inf.v0"
)

var (
	bigOne = big.NewInt(1)
)

var (
	// Deprecated: Never used or returned by the driver.
	ErrorUDTUnavailable = errors.New("UDT are not available on protocols less than 3, please update config")
)

// Marshaler is the interface implemented by objects that can marshal
// themselves into values understood by Cassandra.
type Marshaler interface {
	MarshalCQL(info TypeInfo) ([]byte, error)
}

// Unmarshaler is the interface implemented by objects that can unmarshal
// a Cassandra specific description of themselves.
type Unmarshaler interface {
	UnmarshalCQL(info TypeInfo, data []byte) error
}

// Marshal returns the CQL encoding of the value for the Cassandra
// internal type described by the info parameter.
//
// nil is serialized as CQL null.
// If value implements Marshaler, its MarshalCQL method is called to marshal the data.
// If value is a pointer, the pointed-to value is marshaled.
//
// For supported Go to CQL type conversions, see Session.Query documentation.
func Marshal(info TypeInfo, value interface{}) ([]byte, error) {
	if valueRef := reflect.ValueOf(value); valueRef.Kind() == reflect.Ptr {
		if valueRef.IsNil() {
			return nil, nil
		} else if v, ok := value.(Marshaler); ok {
			return v.MarshalCQL(info)
		} else {
			return Marshal(info, valueRef.Elem().Interface())
		}
	}

	if v, ok := value.(Marshaler); ok {
		return v.MarshalCQL(info)
	}

	return info.Marshal(value)
}

// Unmarshal parses the CQL encoded data based on the info parameter that
// describes the Cassandra internal data type and stores the result in the
// value pointed by value.
//
// If value implements Unmarshaler, it's UnmarshalCQL method is called to
// unmarshal the data.
// If value is a pointer to pointer, it is set to nil if the CQL value is
// null. Otherwise, nulls are unmarshalled as zero value.
//
// For supported CQL to Go type conversions, see Iter.Scan documentation.
func Unmarshal(info TypeInfo, data []byte, value interface{}) error {
	if v, ok := value.(Unmarshaler); ok {
		return v.UnmarshalCQL(info, data)
	}

	// check for pointer
	// we don't error for non-pointers because certain types support unmarshalling
	// into maps/slices
	valueRef := reflect.ValueOf(value)
	if valueRef.Kind() == reflect.Ptr {
		// handle pointers and nil data
		valueElemRef := valueRef.Elem()
		switch valueElemRef.Kind() {
		case reflect.Ptr:
			if data == nil {
				if valueElemRef.IsNil() {
					return nil
				}
				valueRef.Elem().Set(reflect.Zero(valueElemRef.Type()))
				return nil
			}
			// we discussed wrapping this in valueElemRef.IsNil() since we don't need
			// to re-allocate if its non-nil but this was safer and what it was doing
			// before and we didn't want to surprise anyone that relies on this
			// in case the pointer is nil, we call type first then elem to get the type
			// of the underlying value regardless if the pointer is nil or not
			newValue := reflect.New(valueElemRef.Type().Elem())
			valueElemRef.Set(newValue)
			// call Unmarshal again to unwrap the value
			return Unmarshal(info, data, valueElemRef.Interface())
		case reflect.Slice, reflect.Map:
			if data == nil {
				if valueElemRef.IsNil() {
					return nil
				}
				valueRef.Elem().Set(reflect.Zero(valueElemRef.Type()))
				return nil
			}
		case reflect.Interface:
			// set to zero value of the the empty interface value
			if valueElemRef.NumMethod() == 0 && data == nil {
				// once we have a reflect.Type of interface{} we lose the underlying type
				// inside the interface, so we need to call Elem() on the value itself
				// first before calling Type() but first we make sure that it's not
				// an empty interface
				if valueElemRef.IsValid() {
					valueElemRef = valueElemRef.Elem()
				}
				valueRef.Elem().Set(reflect.Zero(valueElemRef.Type()))
				return nil
			}
			if valueElemRef.IsValid() && valueElemRef.Elem().Kind() == reflect.Ptr {
				// call Unmarshal again to unwrap the value
				return Unmarshal(info, data, valueElemRef.Interface())
			}
		}
	}

	return info.Unmarshal(data, value)
}

type varcharLikeTypeInfo struct {
	typ Type
}

// Type returns the underlying type itself.
func (v varcharLikeTypeInfo) Type() Type {
	return v.typ
}

// Zero returns the zero value for the varchar-like CQL type.
func (v varcharLikeTypeInfo) Zero() interface{} {
	if v.typ == TypeBlob {
		return []byte(nil)
	}
	return ""
}

func (v varcharLikeTypeInfo) typeString() string {
	switch v.typ {
	case TypeVarchar:
		return "varchar"
	case TypeAscii:
		return "ascii"
	case TypeBlob:
		return "blob"
	case TypeText:
		return "text"
	default:
		return "unknown"
	}
}

// Marshal marshals the value into a byte slice.
func (vt varcharLikeTypeInfo) Marshal(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case unsetColumn:
		return nil, nil
	case string:
		return []byte(v), nil
	case []byte:
		return v, nil
	}

	if value == nil {
		return nil, nil
	}

	rv := reflect.ValueOf(value)
	t := rv.Type()
	k := t.Kind()
	switch {
	case k == reflect.String:
		return []byte(rv.String()), nil
	case k == reflect.Slice && t.Elem().Kind() == reflect.Uint8:
		return rv.Bytes(), nil
	}
	return nil, marshalErrorf("can not marshal %T into %s. Accepted types: Marshaler, string, []byte, UnsetValue.", value, vt.typeString())
}

// Unmarshal unmarshals the byte slice into the value.
func (vt varcharLikeTypeInfo) Unmarshal(data []byte, value interface{}) error {
	switch v := value.(type) {
	case *string:
		*v = string(data)
		return nil
	case *[]byte:
		if data != nil {
			*v = append((*v)[:0], data...)
		} else {
			*v = nil
		}
		return nil
	case *interface{}:
		if data == nil {
			*v = nil
			return nil
		}
		if vt.typ == TypeBlob {
			*v = make([]byte, len(data))
			copy((*v).([]byte), data)
		} else {
			*v = string(data)
		}
		return nil
	}

	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Ptr {
		return unmarshalErrorf("can not unmarshal into non-pointer %T", value)
	}
	rv = rv.Elem()
	t := rv.Type()
	k := t.Kind()
	switch {
	case k == reflect.String:
		rv.SetString(string(data))
		return nil
	case k == reflect.Slice && t.Elem().Kind() == reflect.Uint8:
		var dataCopy []byte
		if data != nil {
			dataCopy = make([]byte, len(data))
			copy(dataCopy, data)
		}
		rv.SetBytes(dataCopy)
		return nil
	}
	return unmarshalErrorf("can not unmarshal %s into %T. Accepted types: *string, *[]byte", vt.typeString(), value)
}

type smallIntTypeInfo struct{}

// Type returns the type itself.
func (smallIntTypeInfo) Type() Type {
	return TypeSmallInt
}

// Zero returns the zero value for the smallint CQL type.
func (smallIntTypeInfo) Zero() interface{} {
	return int16(0)
}

// Marshal marshals the value into a byte slice.
func (smallIntTypeInfo) Marshal(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case unsetColumn:
		return nil, nil
	case int16:
		return encShort(v), nil
	case uint16:
		return encShort(int16(v)), nil
	case int8:
		return encShort(int16(v)), nil
	case uint8:
		return encShort(int16(v)), nil
	case int:
		if v > math.MaxInt16 || v < math.MinInt16 {
			return nil, marshalErrorf("marshal smallint: value %d out of range", v)
		}
		return encShort(int16(v)), nil
	case int32:
		if v > math.MaxInt16 || v < math.MinInt16 {
			return nil, marshalErrorf("marshal smallint: value %d out of range", v)
		}
		return encShort(int16(v)), nil
	case int64:
		if v > math.MaxInt16 || v < math.MinInt16 {
			return nil, marshalErrorf("marshal smallint: value %d out of range", v)
		}
		return encShort(int16(v)), nil
	case uint:
		if v > math.MaxUint16 {
			return nil, marshalErrorf("marshal smallint: value %d out of range", v)
		}
		return encShort(int16(v)), nil
	case uint32:
		if v > math.MaxUint16 {
			return nil, marshalErrorf("marshal smallint: value %d out of range", v)
		}
		return encShort(int16(v)), nil
	case uint64:
		if v > math.MaxUint16 {
			return nil, marshalErrorf("marshal smallint: value %d out of range", v)
		}
		return encShort(int16(v)), nil
	case string:
		n, err := strconv.ParseInt(v, 10, 16)
		if err != nil {
			return nil, marshalErrorf("can not marshal %T into smallint: %v", value, err)
		}
		return encShort(int16(n)), nil
	}

	if value == nil {
		return nil, nil
	}

	switch rv := reflect.ValueOf(value); rv.Type().Kind() {
	case reflect.Int, reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8:
		v := rv.Int()
		if v > math.MaxInt16 || v < math.MinInt16 {
			return nil, marshalErrorf("marshal smallint: value %d out of range", v)
		}
		return encShort(int16(v)), nil
	case reflect.Uint, reflect.Uint64, reflect.Uint32, reflect.Uint16, reflect.Uint8:
		v := rv.Uint()
		if v > math.MaxUint16 {
			return nil, marshalErrorf("marshal smallint: value %d out of range", v)
		}
		return encShort(int16(v)), nil
	case reflect.Ptr:
		if rv.IsNil() {
			return nil, nil
		}
	}

	return nil, marshalErrorf("can not marshal %T into smallint. Accepted types: Marshaler, int16, uint16, int8, uint8, int, uint, int32, uint32, int64, uint64, string, UnsetValue.", value)
}

// Unmarshal unmarshals the byte slice into the value.
func (s smallIntTypeInfo) Unmarshal(data []byte, value interface{}) error {
	decodedData, err := decShort(data)
	if err != nil {
		return unmarshalErrorf("%s", err.Error())
	}
	if iptr, ok := value.(*interface{}); ok && iptr != nil {
		var v int16
		if err := unmarshalIntlike(TypeSmallInt, int64(decodedData), data, &v); err != nil {
			return err
		}
		*iptr = v
		return nil
	}
	return unmarshalIntlike(TypeSmallInt, int64(decodedData), data, value)
}

type tinyIntTypeInfo struct{}

// Type returns the type itself.
func (tinyIntTypeInfo) Type() Type {
	return TypeTinyInt
}

// Zero returns the zero value for the tinyint CQL type.
func (tinyIntTypeInfo) Zero() interface{} {
	return int8(0)
}

// Marshal marshals the value into a byte slice.
func (tinyIntTypeInfo) Marshal(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case unsetColumn:
		return nil, nil
	case int8:
		return []byte{byte(v)}, nil
	case uint8:
		return []byte{byte(v)}, nil
	case int16:
		if v > math.MaxInt8 || v < math.MinInt8 {
			return nil, marshalErrorf("marshal tinyint: value %d out of range", v)
		}
		return []byte{byte(v)}, nil
	case uint16:
		if v > math.MaxUint8 {
			return nil, marshalErrorf("marshal tinyint: value %d out of range", v)
		}
		return []byte{byte(v)}, nil
	case int:
		if v > math.MaxInt8 || v < math.MinInt8 {
			return nil, marshalErrorf("marshal tinyint: value %d out of range", v)
		}
		return []byte{byte(v)}, nil
	case int32:
		if v > math.MaxInt8 || v < math.MinInt8 {
			return nil, marshalErrorf("marshal tinyint: value %d out of range", v)
		}
		return []byte{byte(v)}, nil
	case int64:
		if v > math.MaxInt8 || v < math.MinInt8 {
			return nil, marshalErrorf("marshal tinyint: value %d out of range", v)
		}
		return []byte{byte(v)}, nil
	case uint:
		if v > math.MaxUint8 {
			return nil, marshalErrorf("marshal tinyint: value %d out of range", v)
		}
		return []byte{byte(v)}, nil
	case uint32:
		if v > math.MaxUint8 {
			return nil, marshalErrorf("marshal tinyint: value %d out of range", v)
		}
		return []byte{byte(v)}, nil
	case uint64:
		if v > math.MaxUint8 {
			return nil, marshalErrorf("marshal tinyint: value %d out of range", v)
		}
		return []byte{byte(v)}, nil
	case string:
		n, err := strconv.ParseInt(v, 10, 8)
		if err != nil {
			return nil, marshalErrorf("can not marshal %T into tinyint: %v", value, err)
		}
		return []byte{byte(n)}, nil
	}

	if value == nil {
		return nil, nil
	}

	switch rv := reflect.ValueOf(value); rv.Type().Kind() {
	case reflect.Int, reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8:
		v := rv.Int()
		if v > math.MaxInt8 || v < math.MinInt8 {
			return nil, marshalErrorf("marshal tinyint: value %d out of range", v)
		}
		return []byte{byte(v)}, nil
	case reflect.Uint, reflect.Uint64, reflect.Uint32, reflect.Uint16, reflect.Uint8:
		v := rv.Uint()
		if v > math.MaxUint8 {
			return nil, marshalErrorf("marshal tinyint: value %d out of range", v)
		}
		return []byte{byte(v)}, nil
	case reflect.Ptr:
		if rv.IsNil() {
			return nil, nil
		}
	}

	return nil, marshalErrorf("can not marshal %T into tinyint. Accepted types: int8, uint8, int16, uint16, int, uint, int32, uint32, int64, uint64, string, UnsetValue.", value)
}

// Unmarshal unmarshals the byte slice into the value.
func (t tinyIntTypeInfo) Unmarshal(data []byte, value interface{}) error {
	decodedData, err := decTiny(data)
	if err != nil {
		return unmarshalErrorf("%s", err.Error())
	}
	if iptr, ok := value.(*interface{}); ok && iptr != nil {
		var v int8
		if err := unmarshalIntlike(TypeTinyInt, int64(decodedData), data, &v); err != nil {
			return err
		}
		*iptr = v
		return nil
	}
	return unmarshalIntlike(TypeTinyInt, int64(decodedData), data, value)
}

type intTypeInfo struct{}

// Type returns the type itself.
func (intTypeInfo) Type() Type {
	return TypeInt
}

// Zero returns the zero value for the int CQL type.
func (intTypeInfo) Zero() interface{} {
	return int(0)
}

// Marshal marshals the value into a byte slice.
func (intTypeInfo) Marshal(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case unsetColumn:
		return nil, nil
	case int:
		if v > math.MaxInt32 || v < math.MinInt32 {
			return nil, marshalErrorf("marshal int: value %d out of range", v)
		}
		return encInt(int32(v)), nil
	case uint:
		if v > math.MaxUint32 {
			return nil, marshalErrorf("marshal int: value %d out of range", v)
		}
		return encInt(int32(v)), nil
	case int64:
		if v > math.MaxInt32 || v < math.MinInt32 {
			return nil, marshalErrorf("marshal int: value %d out of range", v)
		}
		return encInt(int32(v)), nil
	case uint64:
		if v > math.MaxUint32 {
			return nil, marshalErrorf("marshal int: value %d out of range", v)
		}
		return encInt(int32(v)), nil
	case int32:
		return encInt(v), nil
	case uint32:
		return encInt(int32(v)), nil
	case int16:
		return encInt(int32(v)), nil
	case uint16:
		return encInt(int32(v)), nil
	case int8:
		return encInt(int32(v)), nil
	case uint8:
		return encInt(int32(v)), nil
	case string:
		i, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			return nil, marshalErrorf("can not marshal string to int: %s", err)
		}
		return encInt(int32(i)), nil
	}

	if value == nil {
		return nil, nil
	}

	switch rv := reflect.ValueOf(value); rv.Type().Kind() {
	case reflect.Int, reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8:
		v := rv.Int()
		if v > math.MaxInt32 || v < math.MinInt32 {
			return nil, marshalErrorf("marshal int: value %d out of range", v)
		}
		return encInt(int32(v)), nil
	case reflect.Uint, reflect.Uint64, reflect.Uint32, reflect.Uint16, reflect.Uint8:
		v := rv.Uint()
		if v > math.MaxInt32 {
			return nil, marshalErrorf("marshal int: value %d out of range", v)
		}
		return encInt(int32(v)), nil
	case reflect.Ptr:
		if rv.IsNil() {
			return nil, nil
		}
	}

	return nil, marshalErrorf("can not marshal %T into int. Accepted types: int8, uint8, int16, uint16, int, uint, int32, uint32, int64, uint64, string, UnsetValue.", value)
}

// Unmarshal unmarshals the byte slice into the value.
func (i intTypeInfo) Unmarshal(data []byte, value interface{}) error {
	decodedData, err := decInt(data)
	if err != nil {
		return unmarshalErrorf("%s", err.Error())
	}
	if iptr, ok := value.(*interface{}); ok && iptr != nil {
		var v int
		if err := unmarshalIntlike(TypeInt, int64(decodedData), data, &v); err != nil {
			return err
		}
		*iptr = v
		return nil
	}
	return unmarshalIntlike(TypeInt, int64(decodedData), data, value)
}

func encInt(x int32) []byte {
	return []byte{byte(x >> 24), byte(x >> 16), byte(x >> 8), byte(x)}
}

func decInt(x []byte) (int32, error) {
	if x == nil || len(x) == 0 {
		// len(x)==0 is to keep old behavior from 1.x (empty values can be in the DB and are different from NULL)
		return 0, nil
	}
	if len(x) != 4 {
		return 0, fmt.Errorf("expected 4 bytes decoding int but got %v", len(x))
	}
	return int32(x[0])<<24 | int32(x[1])<<16 | int32(x[2])<<8 | int32(x[3]), nil
}

func encShort(x int16) []byte {
	p := make([]byte, 2)
	p[0] = byte(x >> 8)
	p[1] = byte(x)
	return p
}

func decShort(p []byte) (int16, error) {
	if p == nil || len(p) == 0 {
		// len(p)==0 is to keep old behavior from 1.x (empty values can be in the DB and are different from NULL)
		return 0, nil
	}
	if len(p) != 2 {
		return 0, fmt.Errorf("expected 2 bytes decoding short but got %v", len(p))
	}
	return int16(p[0])<<8 | int16(p[1]), nil
}

func decTiny(p []byte) (int8, error) {
	if p == nil || len(p) == 0 {
		// len(p)==0 is to keep old behavior from 1.x (empty values can be in the DB and are different from NULL)
		return 0, nil
	}
	if len(p) != 1 {
		return 0, fmt.Errorf("expected 1 byte decoding tinyint but got %v", len(p))
	}
	return int8(p[0]), nil
}

type bigIntLikeTypeInfo struct {
	typ Type
}

// Type returns the underlying type itself.
func (b bigIntLikeTypeInfo) Type() Type {
	return b.typ
}

// Zero returns the zero value for the bigint-like CQL type.
func (bigIntLikeTypeInfo) Zero() interface{} {
	return int64(0)
}

// Marshal marshals the value into a byte slice.
func (bigIntLikeTypeInfo) Marshal(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case unsetColumn:
		return nil, nil
	case int:
		return encBigInt(int64(v)), nil
	case uint:
		if uint64(v) > math.MaxInt64 {
			return nil, marshalErrorf("marshal bigint: value %d out of range", v)
		}
		return encBigInt(int64(v)), nil
	case int64:
		return encBigInt(v), nil
	case uint64:
		return encBigInt(int64(v)), nil
	case int32:
		return encBigInt(int64(v)), nil
	case uint32:
		return encBigInt(int64(v)), nil
	case int16:
		return encBigInt(int64(v)), nil
	case uint16:
		return encBigInt(int64(v)), nil
	case int8:
		return encBigInt(int64(v)), nil
	case uint8:
		return encBigInt(int64(v)), nil
	case big.Int:
		if !v.IsInt64() {
			return nil, marshalErrorf("marshal bigint: value %v out of range", &v)
		}
		return encBigInt(v.Int64()), nil
	case string:
		i, err := strconv.ParseInt(value.(string), 10, 64)
		if err != nil {
			return nil, marshalErrorf("can not marshal string to bigint: %s", err)
		}
		return encBigInt(i), nil
	}

	if value == nil {
		return nil, nil
	}

	rv := reflect.ValueOf(value)
	switch rv.Type().Kind() {
	case reflect.Int, reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8:
		v := rv.Int()
		return encBigInt(v), nil
	case reflect.Uint, reflect.Uint64, reflect.Uint32, reflect.Uint16, reflect.Uint8:
		v := rv.Uint()
		if v > math.MaxInt64 {
			return nil, marshalErrorf("marshal bigint: value %d out of range", v)
		}
		return encBigInt(int64(v)), nil
	}
	return nil, marshalErrorf("can not marshal %T into bigint. Accepted types: big.Int, int8, uint8, int16, uint16, int, uint, int32, uint32, int64, uint64, string, UnsetValue.", value)
}

func encBigInt(x int64) []byte {
	return []byte{byte(x >> 56), byte(x >> 48), byte(x >> 40), byte(x >> 32),
		byte(x >> 24), byte(x >> 16), byte(x >> 8), byte(x)}
}

func bytesToInt64(data []byte) (ret int64) {
	for i := range data {
		ret |= int64(data[i]) << (8 * uint(len(data)-i-1))
	}
	return ret
}

func bytesToUint64(data []byte) (ret uint64) {
	for i := range data {
		ret |= uint64(data[i]) << (8 * uint(len(data)-i-1))
	}
	return ret
}

// Unmarshal unmarshals the byte slice into the value.
func (b bigIntLikeTypeInfo) Unmarshal(data []byte, value interface{}) error {
	decodedData, err := decBigInt(data)
	if err != nil {
		return unmarshalErrorf("can not unmarshal bigint: %s", err.Error())
	}
	if iptr, ok := value.(*interface{}); ok && iptr != nil {
		var v int64
		if err := unmarshalIntlike(b.typ, decodedData, data, &v); err != nil {
			return err
		}
		*iptr = v
		return nil
	}
	return unmarshalIntlike(b.typ, decodedData, data, value)
}

type varintTypeInfo struct{}

// Type returns the type itself.
func (varintTypeInfo) Type() Type {
	return TypeVarint
}

// Zero returns the zero value for the varint CQL type.
func (varintTypeInfo) Zero() interface{} {
	return new(big.Int)
}

// Marshal marshals the value into a byte slice.
func (varintTypeInfo) Marshal(value interface{}) ([]byte, error) {
	var (
		retBytes []byte
		err      error
	)

	switch v := value.(type) {
	case unsetColumn:
		return nil, nil
	case uint64:
		if v > uint64(math.MaxInt64) {
			retBytes = make([]byte, 9)
			binary.BigEndian.PutUint64(retBytes[1:], v)
		} else {
			retBytes = make([]byte, 8)
			binary.BigEndian.PutUint64(retBytes, v)
		}
	case big.Int:
		retBytes = encBigInt2C(&v)
	default:
		retBytes, err = (bigIntLikeTypeInfo{}).Marshal(value)
	}

	if err == nil {
		// trim down to most significant byte
		i := 0
		for ; i < len(retBytes)-1; i++ {
			b0 := retBytes[i]
			if b0 != 0 && b0 != 0xFF {
				break
			}

			b1 := retBytes[i+1]
			if b0 == 0 && b1 != 0 {
				if b1&0x80 == 0 {
					i++
				}
				break
			}

			if b0 == 0xFF && b1 != 0xFF {
				if b1&0x80 > 0 {
					i++
				}
				break
			}
		}
		retBytes = retBytes[i:]
	}

	return retBytes, err
}

// Unmarshal unmarshals the byte slice into the value.
func (varintTypeInfo) Unmarshal(data []byte, value interface{}) error {
	switch v := value.(type) {
	case *big.Int:
		return unmarshalIntlike(TypeVarint, 0, data, value)
	case *uint64:
		if len(data) == 9 && data[0] == 0 {
			*v = bytesToUint64(data[1:])
			return nil
		}
	case *interface{}:
		var bi big.Int
		if err := unmarshalIntlike(TypeVarint, 0, data, &bi); err != nil {
			return err
		}
		*v = &bi
		return nil
	}

	if len(data) > 8 {
		return unmarshalErrorf("unmarshal int: varint value %v out of range for %T (use big.Int)", data, value)
	}

	int64Val := bytesToInt64(data)
	if len(data) > 0 && len(data) < 8 && data[0]&0x80 > 0 {
		int64Val -= (1 << uint(len(data)*8))
	}
	return unmarshalIntlike(TypeVarint, int64Val, data, value)
}

func unmarshalIntlike(typ Type, int64Val int64, data []byte, value interface{}) error {
	switch v := value.(type) {
	case *int:
		if ^uint(0) == math.MaxUint32 && (int64Val < math.MinInt32 || int64Val > math.MaxInt32) {
			return unmarshalErrorf("unmarshal int: value %d out of range for %T", int64Val, *v)
		}
		*v = int(int64Val)
		return nil
	case *uint:
		unitVal := uint64(int64Val)
		switch typ {
		case TypeInt:
			*v = uint(unitVal) & 0xFFFFFFFF
		case TypeSmallInt:
			*v = uint(unitVal) & 0xFFFF
		case TypeTinyInt:
			*v = uint(unitVal) & 0xFF
		default:
			if ^uint(0) == math.MaxUint32 && (int64Val < 0 || int64Val > math.MaxUint32) {
				return unmarshalErrorf("unmarshal int: value %d out of range for %T", unitVal, *v)
			}
			*v = uint(unitVal)
		}
		return nil
	case *int64:
		*v = int64Val
		return nil
	case *uint64:
		switch typ {
		case TypeInt:
			*v = uint64(int64Val) & 0xFFFFFFFF
		case TypeSmallInt:
			*v = uint64(int64Val) & 0xFFFF
		case TypeTinyInt:
			*v = uint64(int64Val) & 0xFF
		default:
			*v = uint64(int64Val)
		}
		return nil
	case *int32:
		if int64Val < math.MinInt32 || int64Val > math.MaxInt32 {
			return unmarshalErrorf("unmarshal int: value %d out of range for %T", int64Val, *v)
		}
		*v = int32(int64Val)
		return nil
	case *uint32:
		switch typ {
		case TypeInt:
			*v = uint32(int64Val) & 0xFFFFFFFF
		case TypeSmallInt:
			*v = uint32(int64Val) & 0xFFFF
		case TypeTinyInt:
			*v = uint32(int64Val) & 0xFF
		default:
			if int64Val < 0 || int64Val > math.MaxUint32 {
				return unmarshalErrorf("unmarshal int: value %d out of range for %T", int64Val, *v)
			}
			*v = uint32(int64Val) & 0xFFFFFFFF
		}
		return nil
	case *int16:
		if int64Val < math.MinInt16 || int64Val > math.MaxInt16 {
			return unmarshalErrorf("unmarshal int: value %d out of range for %T", int64Val, *v)
		}
		*v = int16(int64Val)
		return nil
	case *uint16:
		switch typ {
		case TypeSmallInt:
			*v = uint16(int64Val) & 0xFFFF
		case TypeTinyInt:
			*v = uint16(int64Val) & 0xFF
		default:
			if int64Val < 0 || int64Val > math.MaxUint16 {
				return unmarshalErrorf("unmarshal int: value %d out of range for %T", int64Val, *v)
			}
			*v = uint16(int64Val) & 0xFFFF
		}
		return nil
	case *int8:
		if int64Val < math.MinInt8 || int64Val > math.MaxInt8 {
			return unmarshalErrorf("unmarshal int: value %d out of range for %T", int64Val, *v)
		}
		*v = int8(int64Val)
		return nil
	case *uint8:
		if typ != TypeTinyInt && (int64Val < 0 || int64Val > math.MaxUint8) {
			return unmarshalErrorf("unmarshal int: value %d out of range for %T", int64Val, *v)
		}
		*v = uint8(int64Val) & 0xFF
		return nil
	case *big.Int:
		decBigInt2C(data, v)
		return nil
	case *string:
		*v = strconv.FormatInt(int64Val, 10)
		return nil
	}

	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Ptr {
		return unmarshalErrorf("can not unmarshal into non-pointer %T", value)
	}
	rv = rv.Elem()

	switch rv.Type().Kind() {
	case reflect.Int:
		if ^uint(0) == math.MaxUint32 && (int64Val < math.MinInt32 || int64Val > math.MaxInt32) {
			return unmarshalErrorf("unmarshal int: value %d out of range", int64Val)
		}
		rv.SetInt(int64Val)
		return nil
	case reflect.Int64:
		rv.SetInt(int64Val)
		return nil
	case reflect.Int32:
		if int64Val < math.MinInt32 || int64Val > math.MaxInt32 {
			return unmarshalErrorf("unmarshal int: value %d out of range", int64Val)
		}
		rv.SetInt(int64Val)
		return nil
	case reflect.Int16:
		if int64Val < math.MinInt16 || int64Val > math.MaxInt16 {
			return unmarshalErrorf("unmarshal int: value %d out of range", int64Val)
		}
		rv.SetInt(int64Val)
		return nil
	case reflect.Int8:
		if int64Val < math.MinInt8 || int64Val > math.MaxInt8 {
			return unmarshalErrorf("unmarshal int: value %d out of range", int64Val)
		}
		rv.SetInt(int64Val)
		return nil
	case reflect.Uint:
		unitVal := uint64(int64Val)
		switch typ {
		case TypeInt:
			rv.SetUint(unitVal & 0xFFFFFFFF)
		case TypeSmallInt:
			rv.SetUint(unitVal & 0xFFFF)
		case TypeTinyInt:
			rv.SetUint(unitVal & 0xFF)
		default:
			if ^uint(0) == math.MaxUint32 && (int64Val < 0 || int64Val > math.MaxUint32) {
				return unmarshalErrorf("unmarshal int: value %d out of range for %s", unitVal, rv.Type())
			}
			rv.SetUint(unitVal)
		}
		return nil
	case reflect.Uint64:
		unitVal := uint64(int64Val)
		switch typ {
		case TypeInt:
			rv.SetUint(unitVal & 0xFFFFFFFF)
		case TypeSmallInt:
			rv.SetUint(unitVal & 0xFFFF)
		case TypeTinyInt:
			rv.SetUint(unitVal & 0xFF)
		default:
			rv.SetUint(unitVal)
		}
		return nil
	case reflect.Uint32:
		unitVal := uint64(int64Val)
		switch typ {
		case TypeInt:
			rv.SetUint(unitVal & 0xFFFFFFFF)
		case TypeSmallInt:
			rv.SetUint(unitVal & 0xFFFF)
		case TypeTinyInt:
			rv.SetUint(unitVal & 0xFF)
		default:
			if int64Val < 0 || int64Val > math.MaxUint32 {
				return unmarshalErrorf("unmarshal int: value %d out of range for %s", int64Val, rv.Type())
			}
			rv.SetUint(unitVal & 0xFFFFFFFF)
		}
		return nil
	case reflect.Uint16:
		unitVal := uint64(int64Val)
		switch typ {
		case TypeSmallInt:
			rv.SetUint(unitVal & 0xFFFF)
		case TypeTinyInt:
			rv.SetUint(unitVal & 0xFF)
		default:
			if int64Val < 0 || int64Val > math.MaxUint16 {
				return unmarshalErrorf("unmarshal int: value %d out of range for %s", int64Val, rv.Type())
			}
			rv.SetUint(unitVal & 0xFFFF)
		}
		return nil
	case reflect.Uint8:
		if typ != TypeTinyInt && (int64Val < 0 || int64Val > math.MaxUint8) {
			return unmarshalErrorf("unmarshal int: value %d out of range for %s", int64Val, rv.Type())
		}
		rv.SetUint(uint64(int64Val) & 0xff)
		return nil
	}
	return unmarshalErrorf("can not unmarshal int-like into %T. Accepted types: big.Int, int8, uint8, int16, uint16, int, uint, int32, uint32, int64, uint64, string, *interface{}.", value)
}

func decBigInt(data []byte) (int64, error) {
	if data == nil || len(data) == 0 {
		// len(data)==0 is to keep old behavior from 1.x (empty values can be in the DB and are different from NULL)
		return 0, nil
	}
	if len(data) != 8 {
		return 0, fmt.Errorf("expected 8 bytes, got %d", len(data))
	}
	return int64(data[0])<<56 | int64(data[1])<<48 |
		int64(data[2])<<40 | int64(data[3])<<32 |
		int64(data[4])<<24 | int64(data[5])<<16 |
		int64(data[6])<<8 | int64(data[7]), nil
}

type booleanTypeInfo struct{}

// Type returns the type itself.
func (booleanTypeInfo) Type() Type {
	return TypeBoolean
}

// Zero returns the zero value for the boolean CQL type.
func (booleanTypeInfo) Zero() interface{} {
	return false
}

// Marshal marshals the value into a byte slice.
func (b booleanTypeInfo) Marshal(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case Marshaler:
		return v.MarshalCQL(b)
	case unsetColumn:
		return nil, nil
	case bool:
		return encBool(v), nil
	}

	if value == nil {
		return nil, nil
	}

	rv := reflect.ValueOf(value)
	switch rv.Type().Kind() {
	case reflect.Bool:
		return encBool(rv.Bool()), nil
	}
	return nil, marshalErrorf("can not marshal %T into boolean. Accepted types: bool, UnsetValue.", value)
}

// Unmarshal unmarshals the byte slice into the value.
func (b booleanTypeInfo) Unmarshal(data []byte, value interface{}) error {
	decodedData, err := decBool(data)
	if err != nil {
		return unmarshalErrorf("can not unmarshal boolean: %s", err.Error())
	}
	switch v := value.(type) {
	case *bool:
		*v = decodedData
		return nil
	case *interface{}:
		*v = decodedData
		return nil
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Ptr {
		return unmarshalErrorf("can not unmarshal into non-pointer %T", value)
	}
	rv = rv.Elem()
	switch rv.Type().Kind() {
	case reflect.Bool:
		rv.SetBool(decodedData)
		return nil
	}
	return unmarshalErrorf("can not unmarshal boolean into %T. Accepted types: *bool, *interface{}.", value)
}

func encBool(v bool) []byte {
	if v {
		return []byte{1}
	}
	return []byte{0}
}

func decBool(v []byte) (bool, error) {
	if v == nil || len(v) == 0 {
		// len(v)==0 is to keep old behavior from 1.x (empty values can be in the DB and are different from NULL)
		return false, nil
	}
	if len(v) != 1 {
		return false, fmt.Errorf("expected 1 byte, got %d", len(v))
	}
	return v[0] != 0, nil
}

type floatTypeInfo struct{}

// Type returns the type itself.
func (floatTypeInfo) Type() Type {
	return TypeFloat
}

// Zero returns the zero value for the float CQL type.
func (floatTypeInfo) Zero() interface{} {
	return float32(0)
}

// Marshal marshals the value into a byte slice.
func (floatTypeInfo) Marshal(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case unsetColumn:
		return nil, nil
	case float32:
		return encInt(int32(math.Float32bits(v))), nil
	}

	if value == nil {
		return nil, nil
	}

	rv := reflect.ValueOf(value)
	switch rv.Type().Kind() {
	case reflect.Float32:
		return encInt(int32(math.Float32bits(float32(rv.Float())))), nil
	}
	return nil, marshalErrorf("can not marshal %T into float. Accepted types: Marshaler, float32, UnsetValue.", value)
}

// Unmarshal unmarshals the byte slice into the value.
func (floatTypeInfo) Unmarshal(data []byte, value interface{}) error {
	decodedData, err := decInt(data)
	if err != nil {
		return err
	}
	switch v := value.(type) {
	case *float32:
		*v = math.Float32frombits(uint32(decodedData))
		return nil
	case *interface{}:
		*v = math.Float32frombits(uint32(decodedData))
		return nil
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Ptr {
		return unmarshalErrorf("can not unmarshal into non-pointer %T", value)
	}
	rv = rv.Elem()
	switch rv.Type().Kind() {
	case reflect.Float32:
		rv.SetFloat(float64(math.Float32frombits(uint32(decodedData))))
		return nil
	}
	return unmarshalErrorf("can not unmarshal float into %T. Accepted types: *float32, *interface{}, UnsetValue.", value)
}

type doubleTypeInfo struct{}

// Type returns the type itself.
func (doubleTypeInfo) Type() Type {
	return TypeDouble
}

// Zero returns the zero value for the double CQL type.
func (doubleTypeInfo) Zero() interface{} {
	return float64(0)
}

// Marshal marshals the value into a byte slice.
func (doubleTypeInfo) Marshal(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case unsetColumn:
		return nil, nil
	case float64:
		return encBigInt(int64(math.Float64bits(v))), nil
	}
	if value == nil {
		return nil, nil
	}
	rv := reflect.ValueOf(value)
	switch rv.Type().Kind() {
	case reflect.Float64:
		return encBigInt(int64(math.Float64bits(rv.Float()))), nil
	}
	return nil, marshalErrorf("can not marshal %T into double. Accepted types: Marshaler, float64, UnsetValue.", value)
}

// Unmarshal unmarshals the byte slice into the value.
func (doubleTypeInfo) Unmarshal(data []byte, value interface{}) error {
	decodedData, err := decBigInt(data)
	if err != nil {
		return unmarshalErrorf("can not unmarshal double: %s", err.Error())
	}
	decodedUint64 := uint64(decodedData)
	switch v := value.(type) {
	case *float64:
		*v = math.Float64frombits(decodedUint64)
		return nil
	case *interface{}:
		*v = math.Float64frombits(decodedUint64)
		return nil
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Ptr {
		return unmarshalErrorf("can not unmarshal into non-pointer %T", value)
	}
	rv = rv.Elem()
	switch rv.Type().Kind() {
	case reflect.Float64:
		rv.SetFloat(math.Float64frombits(decodedUint64))
		return nil
	}
	return unmarshalErrorf("can not unmarshal double into %T. Accepted types: *float64, *interface{}.", value)
}

type decimalTypeInfo struct{}

// Type returns the type itself.
func (decimalTypeInfo) Type() Type {
	return TypeDecimal
}

// Zero returns the zero value for the decimal CQL type.
func (decimalTypeInfo) Zero() interface{} {
	return new(inf.Dec)
}

// Marshal marshals the value into a byte slice.
func (decimalTypeInfo) Marshal(value interface{}) ([]byte, error) {
	if value == nil {
		return nil, nil
	}

	switch v := value.(type) {
	case unsetColumn:
		return nil, nil
	case inf.Dec:
		unscaled := encBigInt2C(v.UnscaledBig())
		if unscaled == nil {
			return nil, marshalErrorf("can not marshal %T into decimal", value)
		}

		buf := make([]byte, 4+len(unscaled))
		copy(buf[0:4], encInt(int32(v.Scale())))
		copy(buf[4:], unscaled)
		return buf, nil
	}
	return nil, marshalErrorf("can not marshal %T into decimal. Accepted types: inf.Dec, UnsetValue.", value)
}

// Unmarshal unmarshals the byte slice into the value.
func (decimalTypeInfo) Unmarshal(data []byte, value interface{}) error {
	if len(data) < 4 {
		return unmarshalErrorf("inf.Dec needs at least 4 bytes, while value has only %d", len(data))
	}

	decodedData, err := decInt(data[0:4])
	if err != nil {
		return err
	}
	switch v := value.(type) {
	case *inf.Dec:
		scale := decodedData
		unscaled := decBigInt2C(data[4:], nil)
		*v = *inf.NewDecBig(unscaled, inf.Scale(scale))
		return nil
	case *interface{}:
		scale := decodedData
		unscaled := decBigInt2C(data[4:], nil)
		*v = inf.NewDecBig(unscaled, inf.Scale(scale))
		return nil
	}
	return unmarshalErrorf("can not unmarshal decimal into %T. Accepted types: *inf.Dec, *interface{}.", value)
}

// decBigInt2C sets the value of n to the big-endian two's complement
// value stored in the given data. If data[0]&80 != 0, the number
// is negative. If data is empty, the result will be 0.
func decBigInt2C(data []byte, n *big.Int) *big.Int {
	if n == nil {
		n = new(big.Int)
	}
	n.SetBytes(data)
	if len(data) > 0 && data[0]&0x80 > 0 {
		n.Sub(n, new(big.Int).Lsh(bigOne, uint(len(data))*8))
	}
	return n
}

// encBigInt2C returns the big-endian two's complement
// form of n.
func encBigInt2C(n *big.Int) []byte {
	switch n.Sign() {
	case 0:
		return []byte{0}
	case 1:
		b := n.Bytes()
		if b[0]&0x80 > 0 {
			b = append([]byte{0}, b...)
		}
		return b
	case -1:
		length := uint(n.BitLen()/8+1) * 8
		b := new(big.Int).Add(n, new(big.Int).Lsh(bigOne, length)).Bytes()
		// When the most significant bit is on a byte
		// boundary, we can get some extra significant
		// bits, so strip them off when that happens.
		if len(b) >= 2 && b[0] == 0xff && b[1]&0x80 != 0 {
			b = b[1:]
		}
		return b
	}
	return nil
}

type timestampTypeInfo struct{}

// Type returns the type itself.
func (timestampTypeInfo) Type() Type {
	return TypeTimestamp
}

// Zero returns the zero value for the timestamp CQL type.
func (timestampTypeInfo) Zero() interface{} {
	return time.Time{}
}

// Marshal marshals the value into a byte slice.
func (timestampTypeInfo) Marshal(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case unsetColumn:
		return nil, nil
	case int64:
		return encBigInt(v), nil
	case time.Time:
		if v.IsZero() {
			return []byte{}, nil
		}
		x := int64(v.UTC().Unix()*1e3) + int64(v.UTC().Nanosecond()/1e6)
		return encBigInt(x), nil
	}

	if value == nil {
		return nil, nil
	}

	rv := reflect.ValueOf(value)
	switch rv.Type().Kind() {
	case reflect.Int64:
		return encBigInt(rv.Int()), nil
	}
	return nil, marshalErrorf("can not marshal %T into timestamp. Accepted types: int64, time.Time, UnsetValue.", value)
}

// Unmarshal unmarshals the byte slice into the value.
func (timestampTypeInfo) Unmarshal(data []byte, value interface{}) error {
	decodedData, err := decBigInt(data)
	if err != nil {
		return unmarshalErrorf("can not unmarshal timestamp: %s", err.Error())
	}
	switch v := value.(type) {
	case *int64:
		*v = decodedData
		return nil
	case *time.Time:
		if len(data) == 0 {
			*v = time.Time{}
			return nil
		}
		x := decodedData
		sec := x / 1000
		nsec := (x - sec*1000) * 1000000
		*v = time.Unix(sec, nsec).In(time.UTC)
		return nil
	case *interface{}:
		if len(data) == 0 {
			*v = time.Time{}
			return nil
		}
		x := decodedData
		sec := x / 1000
		nsec := (x - sec*1000) * 1000000
		*v = time.Unix(sec, nsec).In(time.UTC)
		return nil
	}

	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Ptr {
		return unmarshalErrorf("can not unmarshal into non-pointer %T", value)
	}
	rv = rv.Elem()
	switch rv.Type().Kind() {
	case reflect.Int64:
		rv.SetInt(decodedData)
		return nil
	}
	return unmarshalErrorf("can not unmarshal timestamp into %T. Accepted types: *int64, *time.Time, *interface{}.", value)
}

type timeTypeInfo struct{}

// Type returns the type itself.
func (timeTypeInfo) Type() Type {
	return TypeTime
}

// Zero returns the zero value for the time CQL type.
func (timeTypeInfo) Zero() interface{} {
	return time.Duration(0)
}

// Marshal marshals the value into a byte slice.
func (timeTypeInfo) Marshal(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case unsetColumn:
		return nil, nil
	case int64:
		return encBigInt(v), nil
	case time.Duration:
		return encBigInt(v.Nanoseconds()), nil
	}

	if value == nil {
		return nil, nil
	}

	rv := reflect.ValueOf(value)
	switch rv.Type().Kind() {
	case reflect.Int64:
		return encBigInt(rv.Int()), nil
	}
	return nil, marshalErrorf("can not marshal %T into time. Accepted types: int64, time.Duration, UnsetValue.", value)
}

// Unmarshal unmarshals the byte slice into the value.
func (timeTypeInfo) Unmarshal(data []byte, value interface{}) error {
	decodedData, err := decBigInt(data)
	if err != nil {
		return unmarshalErrorf("can not unmarshal time: %s", err.Error())
	}
	switch v := value.(type) {
	case *int64:
		*v = decodedData
		return nil
	case *time.Duration:
		*v = time.Duration(decodedData)
		return nil
	case *interface{}:
		*v = time.Duration(decodedData)
		return nil
	}

	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Ptr {
		return unmarshalErrorf("can not unmarshal into non-pointer %T", value)
	}
	rv = rv.Elem()
	switch rv.Type().Kind() {
	case reflect.Int64:
		rv.SetInt(decodedData)
		return nil
	}
	return unmarshalErrorf("can not unmarshal time into %T. Accepted types: *int64, *time.Duration, *interface{}.", value)
}

type dateTypeInfo struct{}

// Type returns the type itself.
func (dateTypeInfo) Type() Type {
	return TypeDate
}

// Zero returns the zero value for the date CQL type.
func (dateTypeInfo) Zero() interface{} {
	return time.Time{}
}

const millisecondsInADay int64 = 24 * 60 * 60 * 1000

// Marshal marshals the value into a byte slice.
func (dateTypeInfo) Marshal(value interface{}) ([]byte, error) {
	var timestamp int64
	switch v := value.(type) {
	case unsetColumn:
		return nil, nil
	case int64:
		timestamp = v
		x := timestamp/millisecondsInADay + int64(1<<31)
		return encInt(int32(x)), nil
	case time.Time:
		if v.IsZero() {
			return []byte{}, nil
		}
		timestamp = int64(v.UTC().Unix()*1e3) + int64(v.UTC().Nanosecond()/1e6)
		x := timestamp/millisecondsInADay + int64(1<<31)
		return encInt(int32(x)), nil
	case *time.Time:
		if v.IsZero() {
			return []byte{}, nil
		}
		timestamp = int64(v.UTC().Unix()*1e3) + int64(v.UTC().Nanosecond()/1e6)
		x := timestamp/millisecondsInADay + int64(1<<31)
		return encInt(int32(x)), nil
	case string:
		if v == "" {
			return []byte{}, nil
		}
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return nil, marshalErrorf("can not marshal %T into date, date layout must be '2006-01-02'", value)
		}
		timestamp = int64(t.UTC().Unix()*1e3) + int64(t.UTC().Nanosecond()/1e6)
		x := timestamp/millisecondsInADay + int64(1<<31)
		return encInt(int32(x)), nil
	}

	if value == nil {
		return nil, nil
	}
	return nil, marshalErrorf("can not marshal %T into date. Accepted types: int64, time.Time, *time.Time, string, UnsetValue.", value)
}

// Unmarshal unmarshals the byte slice into the value.
func (dateTypeInfo) Unmarshal(data []byte, value interface{}) error {
	switch v := value.(type) {
	case *time.Time:
		if len(data) == 0 {
			*v = time.Time{}
			return nil
		}
		var origin uint32 = 1 << 31
		var current uint32 = binary.BigEndian.Uint32(data)
		timestamp := (int64(current) - int64(origin)) * millisecondsInADay
		*v = time.UnixMilli(timestamp).In(time.UTC)
		return nil
	case *interface{}:
		if len(data) == 0 {
			*v = time.Time{}
			return nil
		}
		var origin uint32 = 1 << 31
		var current uint32 = binary.BigEndian.Uint32(data)
		timestamp := (int64(current) - int64(origin)) * millisecondsInADay
		*v = time.UnixMilli(timestamp).In(time.UTC)
		return nil
	case *string:
		if len(data) == 0 {
			*v = ""
			return nil
		}
		var origin uint32 = 1 << 31
		var current uint32 = binary.BigEndian.Uint32(data)
		timestamp := (int64(current) - int64(origin)) * millisecondsInADay
		*v = time.UnixMilli(timestamp).In(time.UTC).Format("2006-01-02")
		return nil
	}
	return unmarshalErrorf("can not unmarshal date into %T. Accepted types: *time.Time, *interface{}, *string.", value)
}

type durationTypeInfo struct{}

// Type returns the type itself.
func (durationTypeInfo) Type() Type {
	return TypeDuration
}

// Zero returns the zero value for the duration CQL type.
func (durationTypeInfo) Zero() interface{} {
	return Duration{}
}

// Marshal marshals the value into a byte slice.
func (durationTypeInfo) Marshal(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case unsetColumn:
		return nil, nil
	case int64:
		return encVints(0, 0, v), nil
	case time.Duration:
		return encVints(0, 0, v.Nanoseconds()), nil
	case string:
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, err
		}
		return encVints(0, 0, d.Nanoseconds()), nil
	case Duration:
		return encVints(v.Months, v.Days, v.Nanoseconds), nil
	}

	if value == nil {
		return nil, nil
	}

	rv := reflect.ValueOf(value)
	switch rv.Type().Kind() {
	case reflect.Int64:
		return encBigInt(rv.Int()), nil
	}
	return nil, marshalErrorf("can not marshal %T into duration. Accepted types: int64, time.Duration, string, Duration, UnsetValue.", value)
}

// Unmarshal unmarshals the byte slice into the value.
func (durationTypeInfo) Unmarshal(data []byte, value interface{}) error {
	switch v := value.(type) {
	case *Duration:
		if len(data) == 0 {
			*v = Duration{
				Months:      0,
				Days:        0,
				Nanoseconds: 0,
			}
			return nil
		}
		months, days, nanos, err := decVints(data)
		if err != nil {
			return unmarshalErrorf("failed to unmarshal duration into %T: %s", value, err.Error())
		}
		*v = Duration{
			Months:      months,
			Days:        days,
			Nanoseconds: nanos,
		}
		return nil
	case *interface{}:
		if len(data) == 0 {
			*v = Duration{
				Months:      0,
				Days:        0,
				Nanoseconds: 0,
			}
			return nil
		}
		months, days, nanos, err := decVints(data)
		if err != nil {
			return unmarshalErrorf("failed to unmarshal duration into %T: %s", value, err.Error())
		}
		*v = Duration{
			Months:      months,
			Days:        days,
			Nanoseconds: nanos,
		}
		return nil
	}
	return unmarshalErrorf("can not unmarshal duration into %T. Accepted types: *Duration, *interface{}.", value)
}

func decVints(data []byte) (int32, int32, int64, error) {
	month, i, err := decVint(data, 0)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to extract month: %s", err.Error())
	}
	days, i, err := decVint(data, i)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to extract days: %s", err.Error())
	}
	nanos, _, err := decVint(data, i)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to extract nanoseconds: %s", err.Error())
	}
	return int32(month), int32(days), nanos, err
}

func decVint(data []byte, start int) (int64, int, error) {
	if len(data) <= start {
		return 0, 0, errors.New("unexpected eof")
	}
	firstByte := data[start]
	if firstByte&0x80 == 0 {
		return decIntZigZag(uint64(firstByte)), start + 1, nil
	}
	numBytes := bits.LeadingZeros32(uint32(^firstByte)) - 24
	ret := uint64(firstByte & (0xff >> uint(numBytes)))
	if len(data) < start+numBytes+1 {
		return 0, 0, fmt.Errorf("data expect to have %d bytes, but it has only %d", start+numBytes+1, len(data))
	}
	for i := start; i < start+numBytes; i++ {
		ret <<= 8
		ret |= uint64(data[i+1] & 0xff)
	}
	return decIntZigZag(ret), start + numBytes + 1, nil
}

func decIntZigZag(n uint64) int64 {
	return int64((n >> 1) ^ -(n & 1))
}

func encIntZigZag(n int64) uint64 {
	return uint64((n >> 63) ^ (n << 1))
}

func encVints(months int32, seconds int32, nanos int64) []byte {
	buf := append(encVint(int64(months)), encVint(int64(seconds))...)
	return append(buf, encVint(nanos)...)
}

func encVint(v int64) []byte {
	vEnc := encIntZigZag(v)
	lead0 := bits.LeadingZeros64(vEnc)
	numBytes := (639 - lead0*9) >> 6

	// It can be 1 or 0 is v ==0
	if numBytes <= 1 {
		return []byte{byte(vEnc)}
	}
	extraBytes := numBytes - 1
	var buf = make([]byte, numBytes)
	for i := extraBytes; i >= 0; i-- {
		buf[i] = byte(vEnc)
		vEnc >>= 8
	}
	buf[0] |= byte(^(0xff >> uint(extraBytes)))
	return buf
}

type listSetCQLType struct {
	typ   Type
	types *RegisteredTypes
}

// Params returns the types to build the slice of params for TypeInfoFromParams.
func (listSetCQLType) Params(proto int) []interface{} {
	return []interface{}{
		(*TypeInfo)(nil),
	}
}

// TypeInfoFromParams builds a TypeInfo implementation for the composite type with
// the given parameters.
func (t listSetCQLType) TypeInfoFromParams(proto int, params []interface{}) (TypeInfo, error) {
	if len(params) != 1 {
		return nil, fmt.Errorf("expected 1 param for list/set, got %d", len(params))
	}
	elem, ok := params[0].(TypeInfo)
	if !ok {
		return nil, fmt.Errorf("expected TypeInfo for list/set, got %T", params[0])
	}
	return CollectionType{
		typ:  t.typ,
		Elem: elem,
	}, nil
}

// TypeInfoFromString builds a TypeInfo implementation for the composite type with
// the given names/classes. Only the portion within the parantheses or arrows
// are passed to this function.
func (t listSetCQLType) TypeInfoFromString(proto int, name string) (TypeInfo, error) {
	elem, err := t.types.typeInfoFromString(proto, name)
	if err != nil {
		return nil, err
	}
	return CollectionType{
		typ:  t.typ,
		Elem: elem,
	}, nil
}

// CollectionType represents type information for Cassandra collection types (list, set, map).
// It provides marshaling and unmarshaling for collection types.
type CollectionType struct {
	typ  Type
	Key  TypeInfo // only used for TypeMap
	Elem TypeInfo // only used for TypeMap, TypeList and TypeSet
}

// Type returns the type of the collection.
func (c CollectionType) Type() Type {
	return c.typ
}

func (c CollectionType) zeroType() reflect.Type {
	switch c.typ {
	case TypeMap:
		return reflect.MapOf(reflect.TypeOf(c.Key.Zero()), reflect.TypeOf(c.Elem.Zero()))
	case TypeList, TypeSet:
		return reflect.SliceOf(reflect.TypeOf(c.Elem.Zero()))
	default:
		// we should never have any other types
		panic(fmt.Errorf("unsupported type for CollectionType: %d", c.typ))
	}
}

// Zero returns the zero value for the collection CQL type.
func (c CollectionType) Zero() interface{} {
	return reflect.Zero(c.zeroType()).Interface()
}

// String returns the string representation of the collection.
func (c CollectionType) String() string {
	switch c.typ {
	case TypeMap:
		return fmt.Sprintf("map(%s, %s)", c.Key, c.Elem)
	case TypeList:
		return fmt.Sprintf("list(%s)", c.Elem)
	case TypeSet:
		return fmt.Sprintf("set(%s)", c.Elem)
	default:
		return "unknown"
	}
}

// Marshal marshals the value into a byte slice.
func (c CollectionType) Marshal(value interface{}) ([]byte, error) {
	switch c.typ {
	case TypeMap:
		return c.marshalMap(value)
	case TypeList, TypeSet:
		return c.marshalListSet(value)
	}
	return nil, marshalErrorf("unsupported collection type: %s. Accepted types: map, list, set.", c.String())
}

// Unmarshal unmarshals the byte slice into the value.
func (c CollectionType) Unmarshal(data []byte, value interface{}) error {
	switch c.typ {
	case TypeMap:
		return c.unmarshalMap(data, value)
	case TypeList, TypeSet:
		return c.unmarshalListSet(data, value)
	}
	return unmarshalErrorf("unsupported collection type: %s. Accepted types: map, list, set.", c.String())
}

func writeCollectionSize(n int, buf *bytes.Buffer) error {
	if n > math.MaxInt32 {
		return marshalErrorf("marshal: collection too large")
	}

	_, err := buf.Write([]byte{
		byte(n >> 24),
		byte(n >> 16),
		byte(n >> 8),
		byte(n),
	})
	return err
}

func (l CollectionType) marshalListSet(value interface{}) ([]byte, error) {
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

		if err := writeCollectionSize(n, buf); err != nil {
			return nil, err
		}

		for i := 0; i < n; i++ {
			item, err := Marshal(l.Elem, rv.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			itemLen := len(item)
			// Set the value to null for supported protocols
			if item == nil {
				itemLen = -1
			}
			if err := writeCollectionSize(itemLen, buf); err != nil {
				return nil, err
			}
			buf.Write(item)
		}
		return buf.Bytes(), nil
	case reflect.Map:
		elem := t.Elem()
		if elem.Kind() == reflect.Struct && elem.NumField() == 0 {
			rkeys := rv.MapKeys()
			keys := make([]interface{}, len(rkeys))
			for i := 0; i < len(keys); i++ {
				keys[i] = rkeys[i].Interface()
			}
			return l.Marshal(keys)
		}
	}
	return nil, marshalErrorf("can not marshal %T into collection. Accepted types: slice, array, map[]struct.", value)
}

func readCollectionSize(data []byte) (int, int, error) {
	if len(data) < 4 {
		return 0, 0, unmarshalErrorf("unmarshal list: unexpected eof")
	}
	return int(int32(data[0])<<24 | int32(data[1])<<16 | int32(data[2])<<8 | int32(data[3])),
		4,
		nil
}

func (c CollectionType) unmarshalListSet(data []byte, value interface{}) error {
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
		t = c.zeroType()
	}

	k := t.Kind()
	switch k {
	case reflect.Slice, reflect.Array:
		if data == nil {
			if k == reflect.Array {
				return unmarshalErrorf("unmarshal list: can not store nil in array value")
			}
			if rv.IsNil() {
				return nil
			}
			rv.Set(reflect.Zero(t))
			return nil
		}
		n, p, err := readCollectionSize(data)
		if err != nil {
			return err
		}
		data = data[p:]
		if k == reflect.Array {
			if rv.Len() != n {
				return unmarshalErrorf("unmarshal list: array with wrong size")
			}
		} else {
			rv.Set(reflect.MakeSlice(t, n, n))
			if rv.Kind() == reflect.Interface {
				rv = rv.Elem()
			}
		}
		for i := 0; i < n; i++ {
			m, p, err := readCollectionSize(data)
			if err != nil {
				return err
			}
			data = data[p:]
			// In case m < 0, the value is null, and unmarshalData should be nil.
			var unmarshalData []byte
			if m >= 0 {
				if len(data) < m {
					return unmarshalErrorf("unmarshal list: unexpected eof")
				}
				unmarshalData = data[:m]
				data = data[m:]
			}
			if err := Unmarshal(c.Elem, unmarshalData, rv.Index(i).Addr().Interface()); err != nil {
				return err
			}
		}
		return nil
	}
	return unmarshalErrorf("can not unmarshal collection into %T. Accepted types: *slice, *array, *interface{}.", value)
}

type mapCQLType struct {
	types *RegisteredTypes
}

// Params returns the types to build the slice of params for TypeInfoFromParams.
func (mapCQLType) Params(proto int) []interface{} {
	return []interface{}{
		(*TypeInfo)(nil),
		(*TypeInfo)(nil),
	}
}

// TypeInfoFromParams builds a TypeInfo implementation for the composite type with
// the given parameters.
func (mapCQLType) TypeInfoFromParams(proto int, params []interface{}) (TypeInfo, error) {
	if len(params) != 2 {
		return nil, fmt.Errorf("expected 2 param for map, got %d", len(params))
	}
	key, ok := params[0].(TypeInfo)
	if !ok {
		return nil, fmt.Errorf("expected TypeInfo for map, got %T", params[0])
	}
	elem, ok := params[1].(TypeInfo)
	if !ok {
		return nil, fmt.Errorf("expected TypeInfo for map, got %T", params[1])
	}
	return CollectionType{
		typ:  TypeMap,
		Key:  key,
		Elem: elem,
	}, nil
}

// TypeInfoFromString builds a TypeInfo implementation for the composite type with
// the given names/classes. Only the portion within the parantheses or arrows
// are passed to this function.
func (m mapCQLType) TypeInfoFromString(proto int, name string) (TypeInfo, error) {
	names := splitCompositeTypes(name)
	if len(names) != 2 {
		return nil, fmt.Errorf("expected 2 elements for map, got %v", names)
	}
	kt, err := m.types.typeInfoFromString(proto, names[0])
	if err != nil {
		return nil, err
	}
	et, err := m.types.typeInfoFromString(proto, names[1])
	if err != nil {
		return nil, err
	}
	return CollectionType{
		typ:  TypeMap,
		Key:  kt,
		Elem: et,
	}, nil
}

func (c CollectionType) marshalMap(value interface{}) ([]byte, error) {
	if value == nil {
		return nil, nil
	} else if _, ok := value.(unsetColumn); ok {
		return nil, nil
	}

	rv := reflect.ValueOf(value)

	t := rv.Type()
	if t.Kind() != reflect.Map {
		return nil, marshalErrorf("can not marshal %T into map", value)
	}

	if rv.IsNil() {
		return nil, nil
	}

	buf := &bytes.Buffer{}
	n := rv.Len()

	if err := writeCollectionSize(n, buf); err != nil {
		return nil, err
	}

	keys := rv.MapKeys()
	for i := range keys {
		item, err := Marshal(c.Key, keys[i].Interface())
		if err != nil {
			return nil, err
		}
		itemLen := len(item)
		// Set the key to null for supported protocols
		if item == nil {
			itemLen = -1
		}
		if err := writeCollectionSize(itemLen, buf); err != nil {
			return nil, err
		}
		buf.Write(item)

		item, err = Marshal(c.Elem, rv.MapIndex(keys[i]).Interface())
		if err != nil {
			return nil, err
		}
		itemLen = len(item)
		// Set the value to null for supported protocols
		if item == nil {
			itemLen = -1
		}
		if err := writeCollectionSize(itemLen, buf); err != nil {
			return nil, err
		}
		buf.Write(item)
	}
	return buf.Bytes(), nil
}

func (c CollectionType) unmarshalMap(data []byte, value interface{}) error {
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Ptr {
		return unmarshalErrorf("can not unmarshal map into non-pointer %T", value)
	}
	rv = rv.Elem()
	t := rv.Type()
	if t.Kind() == reflect.Interface {
		if t.NumMethod() != 0 {
			return unmarshalErrorf("can not unmarshal map into non-empty interface %T", value)
		}
		t = c.zeroType()
	} else if t.Kind() != reflect.Map {
		return unmarshalErrorf("can not unmarshal map into %T", value)
	}
	if data == nil {
		rv.Set(reflect.Zero(t))
		return nil
	}
	n, p, err := readCollectionSize(data)
	if err != nil {
		return err
	}
	if n < 0 {
		return unmarshalErrorf("negative map size %d", n)
	}
	rv.Set(reflect.MakeMapWithSize(t, n))
	if rv.Kind() == reflect.Interface {
		rv = rv.Elem()
	}
	data = data[p:]
	for i := 0; i < n; i++ {
		m, p, err := readCollectionSize(data)
		if err != nil {
			return err
		}
		data = data[p:]
		key := reflect.New(t.Key())
		// In case m < 0, the key is null, and unmarshalData should be nil.
		var unmarshalData []byte
		if m >= 0 {
			if len(data) < m {
				return unmarshalErrorf("unmarshal map: unexpected eof")
			}
			unmarshalData = data[:m]
			data = data[m:]
		}
		if err := Unmarshal(c.Key, unmarshalData, key.Interface()); err != nil {
			return err
		}

		m, p, err = readCollectionSize(data)
		if err != nil {
			return err
		}
		data = data[p:]
		val := reflect.New(t.Elem())

		// In case m < 0, the value is null, and unmarshalData should be nil.
		unmarshalData = nil
		if m >= 0 {
			if len(data) < m {
				return unmarshalErrorf("unmarshal map: unexpected eof")
			}
			unmarshalData = data[:m]
			data = data[m:]
		}
		if err := Unmarshal(c.Elem, unmarshalData, val.Interface()); err != nil {
			return err
		}

		rv.SetMapIndex(key.Elem(), val.Elem())
	}
	return nil
}

type uuidType struct{}

// Type returns the type itself.
func (uuidType) Type() Type {
	return TypeUUID
}

// Zero returns the zero value for the uuid CQL type.
func (uuidType) Zero() interface{} {
	return UUID{}
}

// Marshal marshals the value into a byte slice.
func (uuidType) Marshal(value interface{}) ([]byte, error) {
	return uuidMarshal("UUID", value)
}

func uuidMarshal(kind string, value interface{}) ([]byte, error) {
	switch val := value.(type) {
	case unsetColumn:
		return nil, nil
	case UUID:
		return val.Bytes(), nil
	case [16]byte:
		return val[:], nil
	case []byte:
		if len(val) != 16 {
			return nil, marshalErrorf("can not marshal []byte %d bytes long into %s, must be exactly 16 bytes long", len(val), kind)
		}
		return val, nil
	case string:
		b, err := ParseUUID(val)
		if err != nil {
			return nil, err
		}
		return b[:], nil
	}

	if value == nil {
		return nil, nil
	}

	return nil, marshalErrorf("can not marshal %T into %s. Accepted types: UUID, [16]byte, string, UnsetValue.", value, kind)
}

func (uuidType) Unmarshal(data []byte, value interface{}) error {
	return uuidUnmarshal("UUID", data, value)
}

// Unmarshal unmarshals the byte slice into the value.
func uuidUnmarshal(kind string, data []byte, value interface{}) error {
	if len(data) == 0 {
		switch v := value.(type) {
		case *string:
			*v = ""
		case *[]byte:
			*v = nil
		case *UUID:
			*v = UUID{}
		case *interface{}:
			*v = UUID{}
		default:
			return unmarshalErrorf("can not unmarshal %s into %T. Accepted types: *UUID, *[]byte, *string, *interface{}.", kind, value)
		}

		return nil
	}

	if len(data) != 16 {
		return unmarshalErrorf("unable to parse %s: UUIDs must be exactly 16 bytes long", kind)
	}

	switch v := value.(type) {
	case *[16]byte:
		copy((*v)[:], data)
		return nil
	case *UUID:
		copy((*v)[:], data)
		return nil
	case *interface{}:
		var u UUID
		copy(u[:], data)
		*v = u
		return nil
	}

	u, err := UUIDFromBytes(data)
	if err != nil {
		return unmarshalErrorf("unable to parse %s: %s", kind, err)
	}

	switch v := value.(type) {
	case *string:
		*v = u.String()
		return nil
	case *[]byte:
		*v = u[:]
		return nil
	}
	return unmarshalErrorf("can not unmarshal %s into %T. Accepted types: *UUID, *[]byte, *string, *interface{}.", kind, value)
}

type timeUUIDType struct{}

// Type returns the type itself.
func (timeUUIDType) Type() Type {
	return TypeTimeUUID
}

// Zero returns the zero value for the timeuuid CQL type.
func (timeUUIDType) Zero() interface{} {
	return UUID{}
}

// Marshal marshals the value into a byte slice.
func (t timeUUIDType) Marshal(value interface{}) ([]byte, error) {
	switch val := value.(type) {
	case time.Time:
		return UUIDFromTime(val).Bytes(), nil
	}
	return uuidMarshal("timeuuid", value)
}

// Unmarshal unmarshals the byte slice into the value.
func (t timeUUIDType) Unmarshal(data []byte, value interface{}) error {
	switch v := value.(type) {
	case *time.Time:
		id, err := UUIDFromBytes(data)
		if err != nil {
			return err
		} else if id.Version() != 1 {
			return unmarshalErrorf("invalid timeuuid")
		}
		*v = id.Time()
		return nil
	default:
		return uuidUnmarshal("timeuuid", data, value)
	}
}

type inetType struct{}

// Type returns the type itself.
func (inetType) Type() Type {
	return TypeInet
}

// Zero returns the zero value for the inet CQL type.
func (inetType) Zero() interface{} {
	return net.IP(nil)
}

// Marshal marshals the value into a byte slice.
func (inetType) Marshal(value interface{}) ([]byte, error) {
	// we return either the 4 or 16 byte representation of an
	// ip address here otherwise the db value will be prefixed
	// with the remaining byte values e.g. ::ffff:127.0.0.1 and not 127.0.0.1
	switch val := value.(type) {
	case unsetColumn:
		return nil, nil
	case net.IP:
		t := val.To4()
		if t == nil {
			return val.To16(), nil
		}
		return t, nil
	case string:
		b := net.ParseIP(val)
		if b != nil {
			t := b.To4()
			if t == nil {
				return b.To16(), nil
			}
			return t, nil
		}
		return nil, marshalErrorf("cannot marshal. invalid ip string %s", val)
	}

	if value == nil {
		return nil, nil
	}

	return nil, marshalErrorf("cannot marshal %T into inet. Accepted types: net.IP, string.", value)
}

// Unmarshal unmarshals the byte slice into the value.
func (inetType) Unmarshal(data []byte, value interface{}) error {
	switch v := value.(type) {
	case *net.IP:
		if len(data) == 0 {
			*v = nil
			return nil
		}
		if x := len(data); !(x == 4 || x == 16) {
			return unmarshalErrorf("cannot unmarshal inet into %T: invalid sized IP: got %d bytes not 4 or 16", value, x)
		}
		buf := copyBytes(data)
		ip := net.IP(buf)
		if v4 := ip.To4(); v4 != nil {
			*v = v4
			return nil
		}
		*v = ip
		return nil
	case *interface{}:
		if len(data) == 0 {
			*v = net.IP(nil)
			return nil
		}
		if x := len(data); !(x == 4 || x == 16) {
			return unmarshalErrorf("cannot unmarshal inet into %T: invalid sized IP: got %d bytes not 4 or 16", value, x)
		}
		buf := copyBytes(data)
		ip := net.IP(buf)
		if v4 := ip.To4(); v4 != nil {
			*v = v4
			return nil
		}
		*v = ip
		return nil
	case *string:
		if len(data) == 0 {
			*v = ""
			return nil
		}
		ip := net.IP(data)
		if v4 := ip.To4(); v4 != nil {
			*v = v4.String()
			return nil
		}
		*v = ip.String()
		return nil
	}
	return unmarshalErrorf("cannot unmarshal inet into %T. Accepted types: *net.IP, *string, *interface{}.", value)
}

type tupleCQLType struct {
	types *RegisteredTypes
}

// Params returns the types to build the slice of params for TypeInfoFromParams.
func (tupleCQLType) Params(proto int) []interface{} {
	return []interface{}{
		[]TypeInfo(nil),
	}
}

// TypeInfoFromParams builds a TypeInfo implementation for the composite type with
// the given parameters.
func (tupleCQLType) TypeInfoFromParams(proto int, params []interface{}) (TypeInfo, error) {
	if len(params) != 1 {
		return nil, fmt.Errorf("expected 1 param for tuple, got %d", len(params))
	}
	elems, ok := params[0].([]TypeInfo)
	if !ok {
		return nil, fmt.Errorf("expected []TypeInfo for tuple, got %T", params[0])
	}
	return TupleTypeInfo{
		Elems: elems,
	}, nil
}

// TypeInfoFromString builds a TypeInfo implementation for the composite type with
// the given names/classes. Only the portion within the parantheses or arrows
// are passed to this function.
func (t tupleCQLType) TypeInfoFromString(proto int, name string) (TypeInfo, error) {
	names := splitCompositeTypes(name)
	types := make([]TypeInfo, len(names))
	var err error
	for i, name := range names {
		types[i], err = t.types.typeInfoFromString(proto, name)
		if err != nil {
			return nil, err
		}
	}
	return TupleTypeInfo{
		Elems: types,
	}, nil
}

// TupleTypeInfo represents type information for Cassandra tuple types.
// It contains information about the element types in the tuple.
type TupleTypeInfo struct {
	Elems []TypeInfo
}

func (TupleTypeInfo) Type() Type {
	return TypeTuple
}

// Zero returns the zero value for the tuple CQL type.
func (t TupleTypeInfo) Zero() interface{} {
	s := make([]interface{}, len(t.Elems), len(t.Elems))
	for i := range s {
		s[i] = t.Elems[i].Zero()
	}
	return s
}

// Marshal marshals the value into a byte slice.
func (tuple TupleTypeInfo) Marshal(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case unsetColumn:
		return nil, unmarshalErrorf("Invalid request: UnsetValue is unsupported for tuples")
	case []interface{}:
		if len(v) != len(tuple.Elems) {
			return nil, unmarshalErrorf("cannont marshal tuple: wrong number of elements")
		}

		var buf []byte
		for i := range v {
			if v[i] == nil {
				buf = appendInt(buf, int32(-1))
				continue
			}

			data, err := Marshal(tuple.Elems[i], v[i])
			if err != nil {
				return nil, err
			}

			n := len(data)
			buf = appendInt(buf, int32(n))
			buf = append(buf, data...)
		}

		return buf, nil
	}

	rv := reflect.ValueOf(value)
	typ := rv.Type()
	k := typ.Kind()

	switch k {
	case reflect.Struct:
		if v := typ.NumField(); v != len(tuple.Elems) {
			return nil, marshalErrorf("can not marshal tuple into struct %v, not enough fields have %d need %d", typ, v, len(tuple.Elems))
		}

		var buf []byte
		for i := range tuple.Elems {
			field := rv.Field(i)

			if field.Kind() == reflect.Ptr && field.IsNil() {
				buf = appendInt(buf, int32(-1))
				continue
			}

			data, err := Marshal(tuple.Elems[i], field.Interface())
			if err != nil {
				return nil, err
			}

			n := len(data)
			buf = appendInt(buf, int32(n))
			buf = append(buf, data...)
		}

		return buf, nil
	case reflect.Slice, reflect.Array:
		size := rv.Len()
		if size != len(tuple.Elems) {
			return nil, marshalErrorf("can not marshal tuple into %v of length %d need %d elements", k, size, len(tuple.Elems))
		}

		var buf []byte
		for i := range tuple.Elems {
			item := rv.Index(i)

			if item.Kind() == reflect.Ptr && item.IsNil() {
				buf = appendInt(buf, int32(-1))
				continue
			}

			data, err := Marshal(tuple.Elems[i], item.Interface())
			if err != nil {
				return nil, err
			}

			n := len(data)
			buf = appendInt(buf, int32(n))
			buf = append(buf, data...)
		}

		return buf, nil
	}

	return nil, marshalErrorf("cannot marshal %T into tuple. Accepted types: struct, []interface{}, array, slice, UnsetValue.", value)
}

func readBytes(p []byte) ([]byte, []byte) {
	// TODO: really should use a framer
	size := readInt(p)
	p = p[4:]
	if size < 0 {
		return nil, p
	}
	return p[:size], p[size:]
}

// Unmarshal unmarshals the byte slice into the value.
// currently only support unmarshal into a list of values, this makes it possible
// to support tuples without changing the query API. In the future this can be extend
// to allow unmarshalling into custom tuple types.
func (tuple TupleTypeInfo) Unmarshal(data []byte, value interface{}) error {
	switch v := value.(type) {
	case []interface{}:
		if len(v) != len(tuple.Elems) {
			return unmarshalErrorf("can not unmarshal tuple into slice of length %d need %d elements", len(v), len(tuple.Elems))
		}
		for i := range tuple.Elems {
			// each element inside data is a [bytes]
			var p []byte
			if len(data) >= 4 {
				p, data = readBytes(data)
			}
			err := Unmarshal(tuple.Elems[i], p, v[i])
			if err != nil {
				return err
			}
		}
		return nil
	case *interface{}:
		s := make([]interface{}, len(tuple.Elems))
		for i := range tuple.Elems {
			// each element inside data is a [bytes]
			var p []byte
			if len(data) >= 4 {
				p, data = readBytes(data)
			}
			err := Unmarshal(tuple.Elems[i], p, &s[i])
			if err != nil {
				return err
			}
		}
		*v = s
		return nil
	}

	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Ptr {
		return unmarshalErrorf("can not unmarshal into non-pointer %T", value)
	}

	rv = rv.Elem()
	t := rv.Type()
	k := t.Kind()

	switch k {
	case reflect.Struct:
		// TODO: should we ignore private fields?
		if v := t.NumField(); v != len(tuple.Elems) {
			return unmarshalErrorf("can not unmarshal tuple into struct %v, not enough fields have %d need %d", t, v, len(tuple.Elems))
		}

		for i := range tuple.Elems {
			var p []byte
			if len(data) >= 4 {
				p, data = readBytes(data)
			}

			// handle null data
			if p == nil && rv.Field(i).Kind() == reflect.Ptr {
				rv.Field(i).Set(reflect.Zero(rv.Field(i).Type()))
				continue
			}

			if err := Unmarshal(tuple.Elems[i], p, rv.Field(i).Addr().Interface()); err != nil {
				return err
			}
		}

		return nil
	case reflect.Slice, reflect.Array:
		if k == reflect.Array {
			size := rv.Len()
			if size != len(tuple.Elems) {
				return unmarshalErrorf("can not unmarshal tuple into array of length %d need %d elements", size, len(tuple.Elems))
			}
		} else {
			rv.Set(reflect.MakeSlice(t, len(tuple.Elems), len(tuple.Elems)))
		}

		for i := range tuple.Elems {
			var p []byte
			if len(data) >= 4 {
				p, data = readBytes(data)
			}

			// handle null data
			if p == nil && rv.Index(i).Kind() == reflect.Ptr {
				rv.Index(i).Set(reflect.Zero(rv.Index(i).Type()))
				continue
			}

			if err := Unmarshal(tuple.Elems[i], p, rv.Index(i).Addr().Interface()); err != nil {
				return err
			}
		}

		return nil
	}

	return unmarshalErrorf("cannot unmarshal tuple into %T. Accepted types: *struct, []interface{}, *array, *slice, *interface{}.", value)
}

// UDTMarshaler is an interface which should be implemented by users wishing to
// handle encoding UDT types to sent to Cassandra. Note: due to current implentations
// methods defined for this interface must be value receivers not pointer receivers.
type UDTMarshaler interface {
	// MarshalUDT will be called for each field in the the UDT returned by Cassandra,
	// the implementor should marshal the type to return by for example calling
	// Marshal.
	MarshalUDT(name string, info TypeInfo) ([]byte, error)
}

// UDTUnmarshaler should be implemented by users wanting to implement custom
// UDT unmarshaling.
type UDTUnmarshaler interface {
	// UnmarshalUDT will be called for each field in the UDT return by Cassandra,
	// the implementor should unmarshal the data into the value of their chosing,
	// for example by calling Unmarshal.
	UnmarshalUDT(name string, info TypeInfo, data []byte) error
}

type udtCQLType struct {
	types *RegisteredTypes
}

// Params returns the types to build the slice of params for TypeInfoFromParams.
func (udtCQLType) Params(proto int) []interface{} {
	return []interface{}{
		"",
		"",
		[]UDTField(nil),
	}
}

// TypeInfoFromParams builds a TypeInfo implementation for the composite type with
// the given parameters.
func (udtCQLType) TypeInfoFromParams(proto int, params []interface{}) (TypeInfo, error) {
	if len(params) != 3 {
		return nil, fmt.Errorf("expected 3 param for udt, got %d", len(params))
	}
	keyspace, ok := params[0].(string)
	if !ok {
		return nil, fmt.Errorf("expected string for udt, got %T", params[0])
	}
	name, ok := params[1].(string)
	if !ok {
		return nil, fmt.Errorf("expected string for udt, got %T", params[1])
	}
	elements, ok := params[2].([]UDTField)
	if !ok {
		return nil, fmt.Errorf("expected []UDTField for udt, got %T", params[2])
	}
	return UDTTypeInfo{
		Keyspace: keyspace,
		Name:     name,
		Elements: elements,
	}, nil
}

// TypeInfoFromString builds a TypeInfo implementation for the composite type with
// the given names/classes. Only the portion within the parantheses or arrows are
// passed to this function.
func (u udtCQLType) TypeInfoFromString(proto int, name string) (TypeInfo, error) {
	parts := splitCompositeTypes(name)
	// let's check to see if its java or not because if its java then we can get
	// everything we need
	if strings.Contains(name, ":") {
		if len(parts) < 3 {
			return nil, fmt.Errorf("expected 3 parts for udt, got %s", name)
		}
		// first is keyspace, second is hex(name), third is elements
		name, _ := hex.DecodeString(parts[1])
		ti := UDTTypeInfo{
			Keyspace: parts[0],
			Name:     string(name),
		}
		ti.Elements = make([]UDTField, 0, len(parts)-2)
		for i := 2; i < len(parts); i++ {
			colonIdx := strings.Index(parts[i], ":")
			var name string
			var typ string
			if colonIdx == -1 {
				typ = parts[i]
			} else {
				// name is hex(name)
				nameb, _ := hex.DecodeString(parts[i][:colonIdx])
				name = string(nameb)
				if len(parts[i]) > colonIdx+1 {
					typ = parts[i][colonIdx+1:]
				}
			}
			et, err := u.types.typeInfoFromString(proto, typ)
			if err != nil {
				return nil, err
			}
			ti.Elements = append(ti.Elements, UDTField{
				Name: name,
				Type: et,
			})
		}
		return ti, nil
	}
	// we can't get the name or anything so we'll just try to parse the elements
	ti := UDTTypeInfo{}
	ti.Elements = make([]UDTField, 0, len(parts))
	for _, part := range parts {
		et, err := u.types.typeInfoFromString(proto, part)
		if err != nil {
			return nil, err
		}
		ti.Elements = append(ti.Elements, UDTField{
			Type: et,
		})
	}
	return ti, nil
}

// UDTField represents a field in a User Defined Type.
// It contains the field name and its type information.
type UDTField struct {
	Name string
	Type TypeInfo
}

// UDTTypeInfo represents type information for Cassandra User Defined Types (UDT).
// It contains the keyspace, type name, and field definitions.
type UDTTypeInfo struct {
	Keyspace string
	Name     string
	Elements []UDTField
}

func (u UDTTypeInfo) Type() Type {
	return TypeUDT
}

// Zero returns the zero value for the UDT CQL type.
func (UDTTypeInfo) Zero() interface{} {
	return map[string]interface{}(nil)
}

// Marshal marshals the value into a byte slice.
func (udt UDTTypeInfo) Marshal(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case unsetColumn:
		return nil, unmarshalErrorf("invalid request: UnsetValue is unsupported for user defined types")
	case UDTMarshaler:
		var buf []byte
		for i := range udt.Elements {
			data, err := v.MarshalUDT(udt.Elements[i].Name, udt.Elements[i].Type)
			if err != nil {
				return nil, err
			}

			buf = appendBytes(buf, data)
		}

		return buf, nil
	case map[string]interface{}:
		var buf []byte
		for i := range udt.Elements {
			val, ok := v[udt.Elements[i].Name]

			var data []byte

			if ok {
				var err error
				data, err = Marshal(udt.Elements[i].Type, val)
				if err != nil {
					return nil, err
				}
			}

			buf = appendBytes(buf, data)
		}

		return buf, nil
	}

	k := reflect.ValueOf(value)
	if k.Kind() == reflect.Ptr {
		if k.IsNil() {
			return nil, marshalErrorf("cannot marshal %T into UDT", value)
		}
		k = k.Elem()
	}

	if k.Kind() != reflect.Struct || !k.IsValid() {
		return nil, marshalErrorf("cannot marshal %T into UDT. Accepted types: UDTMarshaler, map[string]interface{}, struct, UnsetValue.", value)
	}

	fields := make(map[string]reflect.Value)
	t := reflect.TypeOf(value)
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)

		if tag := sf.Tag.Get("cql"); tag != "" {
			fields[tag] = k.Field(i)
		}
	}

	var buf []byte
	for i := range udt.Elements {
		f, ok := fields[udt.Elements[i].Name]
		if !ok {
			f = k.FieldByName(udt.Elements[i].Name)
		}

		var data []byte
		if f.IsValid() && f.CanInterface() {
			var err error
			data, err = Marshal(udt.Elements[i].Type, f.Interface())
			if err != nil {
				return nil, err
			}
		}

		buf = appendBytes(buf, data)
	}

	return buf, nil
}

// Unmarshal unmarshals the byte slice into the value.
func (udt UDTTypeInfo) Unmarshal(data []byte, value interface{}) error {
	// do this up here so we don't need to duplicate all of the map logic below
	if iptr, ok := value.(*interface{}); ok && iptr != nil {
		v := map[string]interface{}{}
		*iptr = v
		value = &v
	}
	switch v := value.(type) {
	case UDTUnmarshaler:
		for id, e := range udt.Elements {
			if len(data) == 0 {
				return nil
			}
			if len(data) < 4 {
				return unmarshalErrorf("can not unmarshal UDT: field [%d]%s: unexpected eof", id, e.Name)
			}

			var p []byte
			p, data = readBytes(data)
			if err := v.UnmarshalUDT(e.Name, e.Type, p); err != nil {
				return err
			}
		}

		return nil
	case *map[string]interface{}:
		if data == nil {
			*v = nil
			return nil
		}

		m := map[string]interface{}{}
		*v = m

		for id, e := range udt.Elements {
			if len(data) == 0 {
				return nil
			}
			if len(data) < 4 {
				return unmarshalErrorf("can not unmarshal UDT: field [%d]%s: unexpected eof", id, e.Name)
			}

			var p []byte
			p, data = readBytes(data)

			v := reflect.New(reflect.TypeOf(e.Type.Zero()))
			if err := Unmarshal(e.Type, p, v.Interface()); err != nil {
				return err
			}
			m[e.Name] = v.Elem().Interface()
		}

		return nil
	}

	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Ptr {
		return unmarshalErrorf("can not unmarshal into non-pointer %T", value)
	}
	k := rv.Elem()
	if k.Kind() != reflect.Struct || !k.IsValid() {
		return unmarshalErrorf("cannot unmarshal UDT into %T. Accepted types: UDTUnmarshaler, *map[string]interface{}, *struct.", value)
	}

	if len(data) == 0 {
		if k.CanSet() {
			k.Set(reflect.Zero(k.Type()))
		}

		return nil
	}

	t := k.Type()
	fields := make(map[string]reflect.Value, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)

		if tag := sf.Tag.Get("cql"); tag != "" {
			fields[tag] = k.Field(i)
		}
	}

	for id, e := range udt.Elements {
		if len(data) == 0 {
			return nil
		}
		if len(data) < 4 {
			// UDT def does not match the column value
			return unmarshalErrorf("can not unmarshal UDT: field [%d]%s: unexpected eof", id, e.Name)
		}

		var p []byte
		p, data = readBytes(data)

		f, ok := fields[e.Name]
		if !ok {
			f = k.FieldByName(e.Name)
			if !f.IsValid() {
				// skip fields which exist in the UDT but not in
				// the struct passed in
				continue
			}
		}

		if !f.IsValid() || !f.CanAddr() {
			return unmarshalErrorf("cannot unmarshal UDT into %T: field %v is not valid", value, e.Name)
		}

		fk := f.Addr().Interface()
		if err := Unmarshal(e.Type, p, fk); err != nil {
			return err
		}
	}

	return nil
}

// MarshalError represents an error that occurred during marshaling.
type MarshalError string

func (m MarshalError) Error() string {
	return string(m)
}

func marshalErrorf(format string, args ...interface{}) MarshalError {
	return MarshalError(fmt.Sprintf(format, args...))
}

// UnmarshalError represents an error that occurred during unmarshaling.
type UnmarshalError string

func (m UnmarshalError) Error() string {
	return string(m)
}

func unmarshalErrorf(format string, args ...interface{}) UnmarshalError {
	return UnmarshalError(fmt.Sprintf(format, args...))
}
