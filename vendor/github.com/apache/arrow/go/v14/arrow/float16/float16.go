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

package float16

import (
	"math"
	"strconv"
)

// Num represents a half-precision floating point value (float16)
// stored on 16 bits.
//
// See https://en.wikipedia.org/wiki/Half-precision_floating-point_format for more informations.
type Num struct {
	bits uint16
}

// New creates a new half-precision floating point value from the provided
// float32 value.
func New(f float32) Num {
	b := math.Float32bits(f)
	sn := uint16((b >> 31) & 0x1)
	exp := (b >> 23) & 0xff
	res := int16(exp) - 127 + 15
	fc := uint16(b>>13) & 0x3ff
	switch {
	case exp == 0:
		res = 0
	case exp == 0xff:
		res = 0x1f
	case res > 0x1e:
		res = 0x1f
		fc = 0
	case res < 0x01:
		res = 0
		fc = 0
	}
	return Num{bits: (sn << 15) | uint16(res<<10) | fc}
}

func (f Num) Float32() float32 {
	sn := uint32((f.bits >> 15) & 0x1)
	exp := (f.bits >> 10) & 0x1f
	res := uint32(exp) + 127 - 15
	fc := uint32(f.bits & 0x3ff)
	switch {
	case exp == 0:
		res = 0
	case exp == 0x1f:
		res = 0xff
	}
	return math.Float32frombits((sn << 31) | (res << 23) | (fc << 13))
}

func (n Num) Negate() Num {
	return Num{bits: n.bits ^ 0x8000}
}

func (n Num) Add(rhs Num) Num {
	return New(n.Float32() + rhs.Float32())
}

func (n Num) Sub(rhs Num) Num {
	return New(n.Float32() - rhs.Float32())
}

func (n Num) Mul(rhs Num) Num {
	return New(n.Float32() * rhs.Float32())
}

func (n Num) Div(rhs Num) Num {
	return New(n.Float32() / rhs.Float32())
}

// Greater returns true if the value represented by n is > other
func (n Num) Greater(other Num) bool {
	return n.Float32() > other.Float32()
}

// GreaterEqual returns true if the value represented by n is >= other
func (n Num) GreaterEqual(other Num) bool {
	return n.Float32() >= other.Float32()
}

// Less returns true if the value represented by n is < other
func (n Num) Less(other Num) bool {
	return n.Float32() < other.Float32()
}

// LessEqual returns true if the value represented by n is <= other
func (n Num) LessEqual(other Num) bool {
	return n.Float32() <= other.Float32()
}

// Max returns the largest Decimal128 that was passed in the arguments
func Max(first Num, rest ...Num) Num {
	answer := first
	for _, number := range rest {
		if number.Greater(answer) {
			answer = number
		}
	}
	return answer
}

// Min returns the smallest Decimal128 that was passed in the arguments
func Min(first Num, rest ...Num) Num {
	answer := first
	for _, number := range rest {
		if number.Less(answer) {
			answer = number
		}
	}
	return answer
}

// Cmp compares the numbers represented by n and other and returns:
//
//	+1 if n > other
//	 0 if n == other
//	-1 if n < other
func (n Num) Cmp(other Num) int {
	switch {
	case n.Greater(other):
		return 1
	case n.Less(other):
		return -1
	}
	return 0
}

func (n Num) Abs() Num {
	switch n.Sign() {
	case -1:
		return n.Negate()
	}
	return n
}

func (n Num) Sign() int {
	f := n.Float32()
	if f > 0 {
		return 1
	} else if f == 0 {
		return 0
	}
	return -1
}

func (f Num) Uint16() uint16 { return f.bits }
func (f Num) String() string { return strconv.FormatFloat(float64(f.Float32()), 'g', -1, 32) }
