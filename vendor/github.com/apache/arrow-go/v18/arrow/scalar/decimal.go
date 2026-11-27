// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package scalar

import (
	"fmt"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/decimal"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/decimal256"
	"github.com/apache/arrow-go/v18/internal/utils"
)

type decimalScalar[T interface {
	decimal.DecimalTypes
	decimal.Num[T]
}] struct {
	scalar
	Value T
}

type (
	Decimal32 = decimalScalar[decimal.Decimal32]
	Decimal64 = decimalScalar[decimal.Decimal64]
)

func NewDecimal32Scalar(val decimal.Decimal32, typ arrow.DataType) *Decimal32 {
	return &Decimal32{scalar{typ, true}, val}
}

func NewDecimal64Scalar(val decimal.Decimal64, typ arrow.DataType) *Decimal64 {
	return &Decimal64{scalar{typ, true}, val}
}

func (s *decimalScalar[T]) Data() []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(&s.Value)), unsafe.Sizeof(s.Value))
}

func (s *decimalScalar[T]) value() interface{} { return s.Value }

func (s *decimalScalar[T]) equals(rhs Scalar) bool {
	return s.Value == rhs.(*decimalScalar[T]).Value
}

func (s *decimalScalar[T]) String() string {
	if !s.Valid {
		return "null"
	}
	val, err := s.CastTo(arrow.BinaryTypes.String)
	if err != nil {
		return "..."
	}
	return string(val.(*String).Value.Bytes())
}

// CastTo casts to string or to a decimal type at least as wide as the
// scalar's own type.
func (s *decimalScalar[T]) CastTo(to arrow.DataType) (Scalar, error) {
	if !s.Valid {
		return MakeNullScalar(to), nil
	}

	dt := s.Type.(arrow.DecimalType)
	if to.ID() == arrow.STRING {
		str, err := utils.FormatDecimalG(s.Value, dt)
		if err != nil {
			return nil, err
		}
		return NewStringScalar(str), nil
	}

	toDec, ok := to.(arrow.DecimalType)
	if !ok || toDec.BitWidth() < dt.BitWidth() {
		return nil, fmt.Errorf("cannot cast non-nil %s scalar to type %s", s.Type, to)
	}
	newVal, err := toDecimal256(s.Value).Rescale(dt.GetScale(), toDec.GetScale())
	if err != nil {
		return nil, err
	}
	if !newVal.FitsInPrecision(toDec.GetPrecision()) {
		return nil, fmt.Errorf("%s value %v will not fit in new precision %d", to, newVal, toDec.GetPrecision())
	}

	switch to.ID() {
	case arrow.DECIMAL32:
		return NewDecimal32Scalar(decimal.Decimal32(newVal.LowBits()), to), nil
	case arrow.DECIMAL64:
		return NewDecimal64Scalar(decimal.Decimal64(newVal.LowBits()), to), nil
	case arrow.DECIMAL128:
		return NewDecimal128Scalar(decimal128.New(int64(newVal.Array()[1]), newVal.LowBits()), to), nil
	default:
		return NewDecimal256Scalar(newVal, to), nil
	}
}

func toDecimal256[T decimal.DecimalTypes](v T) decimal256.Num {
	switch v := any(v).(type) {
	case decimal.Decimal32:
		return decimal256.FromI64(int64(v))
	case decimal.Decimal64:
		return decimal256.FromI64(int64(v))
	case decimal128.Num:
		return decimal256.FromDecimal128(v)
	default:
		return v.(decimal256.Num)
	}
}
