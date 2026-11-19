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

package decimal

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/decimal256"
	"github.com/apache/arrow-go/v18/arrow/internal/debug"
)

// DecimalTypes is a generic constraint representing the implemented decimal types
// in this package, and a single point of update for future additions. Everything
// else is constrained by this.
type DecimalTypes interface {
	Decimal32 | Decimal64 | Decimal128 | Decimal256
}

// Num is an interface that is useful for building generic types for all decimal
// type implementations. It presents all the methods and interfaces necessary to
// operate on the decimal objects without having to care about the bit width.
type Num[T DecimalTypes] interface {
	Negate() T
	Add(T) T
	Sub(T) T
	Mul(T) T
	Div(T) (res, rem T)
	Pow(T) T

	FitsInPrecision(int32) bool
	Abs() T
	Sign() int
	Rescale(int32, int32) (T, error)
	Cmp(T) int

	IncreaseScaleBy(int32) T
	ReduceScaleBy(int32, bool) T

	ToFloat32(int32) float32
	ToFloat64(int32) float64
	ToBigFloat(int32) *big.Float

	ToString(int32) string
}

type (
	Decimal32  int32
	Decimal64  int64
	Decimal128 = decimal128.Num
	Decimal256 = decimal256.Num
)

func MaxPrecision[T DecimalTypes]() int {
	// Floor(log10(2^(8*Sizeof(T)-1) - 1)), as literals so the call inlines.
	var z T
	switch unsafe.Sizeof(z) {
	case 4:
		return 9
	case 8:
		return 18
	case 16:
		return 38
	default:
		return 76
	}
}

func (d Decimal32) Negate() Decimal32 { return -d }
func (d Decimal64) Negate() Decimal64 { return -d }

func (d Decimal32) Add(rhs Decimal32) Decimal32 { return d + rhs }
func (d Decimal64) Add(rhs Decimal64) Decimal64 { return d + rhs }

func (d Decimal32) Sub(rhs Decimal32) Decimal32 { return d - rhs }
func (d Decimal64) Sub(rhs Decimal64) Decimal64 { return d - rhs }

func (d Decimal32) Mul(rhs Decimal32) Decimal32 { return d * rhs }
func (d Decimal64) Mul(rhs Decimal64) Decimal64 { return d * rhs }

func (d Decimal32) Div(rhs Decimal32) (res, rem Decimal32) {
	return d / rhs, d % rhs
}

func (d Decimal64) Div(rhs Decimal64) (res, rem Decimal64) {
	return d / rhs, d % rhs
}

// about 4-5x faster than using math.Pow which requires converting to float64
// and back to integers
func intPow[T int32 | int64](base, exp T) T {
	result := T(1)
	for {
		if exp&1 == 1 {
			result *= base
		}
		exp >>= 1
		if exp == 0 {
			break
		}
		base *= base
	}

	return result
}

func (d Decimal32) Pow(rhs Decimal32) Decimal32 {
	return Decimal32(intPow(int32(d), int32(rhs)))
}

func (d Decimal64) Pow(rhs Decimal64) Decimal64 {
	return Decimal64(intPow(int64(d), int64(rhs)))
}

func (d Decimal32) Sign() int {
	if d == 0 {
		return 0
	}
	return int(1 | (d >> 31))
}

func (d Decimal64) Sign() int {
	if d == 0 {
		return 0
	}
	return int(1 | (d >> 63))
}

func (n Decimal32) Abs() Decimal32 {
	if n < 0 {
		return -n
	}
	return n
}

func (n Decimal64) Abs() Decimal64 {
	if n < 0 {
		return -n
	}
	return n
}

func (n Decimal32) FitsInPrecision(prec int32) bool {
	debug.Assert(prec > 0, "precision must be > 0")
	debug.Assert(prec <= 9, "precision must be <= 9")
	return n.Abs() < Decimal32(math.Pow10(int(prec)))
}

func (n Decimal64) FitsInPrecision(prec int32) bool {
	debug.Assert(prec > 0, "precision must be > 0")
	debug.Assert(prec <= 18, "precision must be <= 18")
	return n.Abs() < Decimal64(math.Pow10(int(prec)))
}

func (n Decimal32) ToString(scale int32) string {
	return n.ToBigFloat(scale).Text('f', int(scale))
}

func (n Decimal64) ToString(scale int32) string {
	return n.ToBigFloat(scale).Text('f', int(scale))
}

var pt5 = big.NewFloat(0.5)

func decimalFromString[T interface {
	Decimal32 | Decimal64
	FitsInPrecision(int32) bool
}](v string, prec, scale int32) (n T, err error) {
	var nbits = uint(unsafe.Sizeof(T(0))) * 8

	var out *big.Float
	out, _, err = big.ParseFloat(v, 10, nbits, big.ToNearestEven)
	if err != nil {
		return
	}
	if out.IsInf() {
		return n, fmt.Errorf("cannot convert %s to decimal%d", v, nbits)
	}

	if scale < 0 {
		// Check the magnitude before converting, which would allocate as many
		// bits as the value has.
		if out.MantExp(nil) > int(nbits) {
			return n, fmt.Errorf("bitlen too large for decimal%d", nbits)
		}
		var tmp big.Int
		val, _ := out.Int(&tmp)

		// |val| < 2^64 < 10^20, so a larger divisor gives the same quotient.
		div := new(big.Int).Exp(big.NewInt(10), big.NewInt(min(-int64(scale), 20)), nil)
		n = T(val.Quo(val, div).Int64())
	} else {
		// Clamp before converting, since converting an out of range float to
		// uint is implementation specific. SetPrec caps at big.MaxPrec anyway.
		bitsNeeded := math.Round(float64(int64(prec)+int64(scale)+1) / math.Log10(2))
		var precInBits = uint(min(max(bitsNeeded, 0), big.MaxPrec-1)) + 1

		mulPow10(out.SetPrec(precInBits), int64(scale))
		// Reject before rounding and converting, which would allocate as many
		// bits as the value has.
		if out.IsInf() || out.MantExp(nil) > int(nbits) {
			return n, fmt.Errorf("bitlen too large for decimal%d", nbits)
		}
		// A magnitude below 1/4 rounds to zero. Clearing it skips the
		// rounding below, which allocates in proportion to the gap between
		// the value's exponent and 0.5's.
		if out.MantExp(nil) < -1 {
			out.SetInt64(0)
		}
		if out.Signbit() {
			out.Sub(out, pt5)
		} else {
			out.Add(out, pt5)
		}

		var tmp big.Int
		val, _ := out.Int(&tmp)
		// This also rejects the signed minimum, which fits the storage but has
		// more digits than the maximum precision allows.
		if val.BitLen() >= int(nbits) {
			return n, fmt.Errorf("bitlen too large for decimal%d", nbits)
		}
		n = T(val.Int64())
	}

	if !n.FitsInPrecision(prec) {
		err = fmt.Errorf("val %v doesn't fit in precision %d", n, prec)
	}
	return
}

func Decimal32FromString(v string, prec, scale int32) (n Decimal32, err error) {
	return decimalFromString[Decimal32](v, prec, scale)
}

func Decimal64FromString(v string, prec, scale int32) (n Decimal64, err error) {
	return decimalFromString[Decimal64](v, prec, scale)
}

func Decimal128FromString(v string, prec, scale int32) (n Decimal128, err error) {
	return decimal128.FromString(v, prec, scale)
}

func Decimal256FromString(v string, prec, scale int32) (n Decimal256, err error) {
	return decimal256.FromString(v, prec, scale)
}

func scalePositiveFloat64(v float64, prec, scale int32) (float64, error) {
	v *= math.Pow10(int(scale))
	v = math.RoundToEven(v)

	maxabs := math.Pow10(int(prec))
	if v >= maxabs {
		return 0, fmt.Errorf("cannot convert %f to decimal(precision=%d, scale=%d)", v, prec, scale)
	}
	return v, nil
}

func fromPositiveFloat[T Decimal32 | Decimal64, F float32 | float64](v F, prec, scale int32) (T, error) {
	if prec > int32(MaxPrecision[T]()) {
		return T(0), fmt.Errorf("invalid precision %d for converting float to Decimal", prec)
	}

	val, err := scalePositiveFloat64(float64(v), prec, scale)
	if err != nil {
		return T(0), err
	}

	return T(F(val)), nil
}

func Decimal32FromFloat[F float32 | float64](v F, prec, scale int32) (Decimal32, error) {
	if v < 0 {
		dec, err := fromPositiveFloat[Decimal32](-v, prec, scale)
		if err != nil {
			return dec, err
		}

		return -dec, nil
	}

	return fromPositiveFloat[Decimal32](v, prec, scale)
}

func Decimal64FromFloat[F float32 | float64](v F, prec, scale int32) (Decimal64, error) {
	if v < 0 {
		dec, err := fromPositiveFloat[Decimal64](-v, prec, scale)
		if err != nil {
			return dec, err
		}

		return -dec, nil
	}

	return fromPositiveFloat[Decimal64](v, prec, scale)
}

func Decimal128FromFloat(v float64, prec, scale int32) (Decimal128, error) {
	return decimal128.FromFloat64(v, prec, scale)
}

func Decimal256FromFloat(v float64, prec, scale int32) (Decimal256, error) {
	return decimal256.FromFloat64(v, prec, scale)
}

func (n Decimal32) ToFloat32(scale int32) float32 {
	return float32(n.ToFloat64(scale))
}

func (n Decimal64) ToFloat32(scale int32) float32 {
	return float32(n.ToFloat64(scale))
}

func (n Decimal32) ToFloat64(scale int32) float64 {
	return float64(n) * math.Pow10(-int(scale))
}

func (n Decimal64) ToFloat64(scale int32) float64 {
	return float64(n) * math.Pow10(-int(scale))
}

func (n Decimal32) ToBigFloat(scale int32) *big.Float {
	return mulPow10(new(big.Float).SetInt64(int64(n)).SetPrec(32), -int64(scale))
}

func (n Decimal64) ToBigFloat(scale int32) *big.Float {
	return mulPow10(new(big.Float).SetInt64(int64(n)).SetPrec(64), -int64(scale))
}

// mulPow10 sets f to f * 10^exp, rounded to f's precision, and returns f. For
// precisions up to 64 bits the result is within one unit in the last place.
// 10^exp is applied as an exact 2^exp and then 5^|exp|, computed at 128 bits
// by repeated squaring so the work stays bounded. Splitting the power keeps it
// from overflowing before the result does. A result outside big.Float's
// exponent range becomes ±Inf or 0.
func mulPow10(f *big.Float, exp int64) *big.Float {
	if f.Sign() == 0 {
		// 5^|exp| may overflow to infinity, and 0 * Inf panics.
		return f
	}

	k := exp
	if k < 0 {
		k = -k
	}
	pow := new(big.Float).SetPrec(128).SetInt64(1)
	for base := new(big.Float).SetPrec(128).SetInt64(5); k > 0; k >>= 1 {
		if k&1 == 1 {
			pow.Mul(pow, base)
		}
		base.Mul(base, base)
	}

	f.SetMantExp(f, int(exp))
	if exp < 0 {
		return f.Quo(f, pow)
	}
	return f.Mul(f, pow)
}

func cmpDec[T Decimal32 | Decimal64](lhs, rhs T) int {
	switch {
	case lhs > rhs:
		return 1
	case lhs < rhs:
		return -1
	}
	return 0
}

func (n Decimal32) Cmp(other Decimal32) int {
	return cmpDec(n, other)
}

func (n Decimal64) Cmp(other Decimal64) int {
	return cmpDec(n, other)
}

func (n Decimal32) IncreaseScaleBy(increase int32) Decimal32 {
	debug.Assert(increase >= 0, "invalid increase scale for decimal32")
	debug.Assert(increase <= 9, "invalid increase scale for decimal32")

	return n * Decimal32(intPow(10, increase))
}

func (n Decimal64) IncreaseScaleBy(increase int32) Decimal64 {
	debug.Assert(increase >= 0, "invalid increase scale for decimal64")
	debug.Assert(increase <= 18, "invalid increase scale for decimal64")

	return n * Decimal64(intPow(10, int64(increase)))
}

func reduceScale[T interface {
	Decimal32 | Decimal64
	Abs() T
}](n T, reduce int32, round bool) T {
	if reduce == 0 {
		return n
	}

	divisor := T(intPow(10, int64(reduce)))
	if !round {
		return n / divisor
	}

	quo, remainder := n/divisor, n%divisor
	divisorHalf := divisor / 2
	if remainder.Abs() >= divisorHalf {
		if n > 0 {
			quo++
		} else {
			quo--
		}
	}

	return quo
}

func (n Decimal32) ReduceScaleBy(reduce int32, round bool) Decimal32 {
	debug.Assert(reduce >= 0, "invalid reduce scale for decimal32")
	debug.Assert(reduce <= 9, "invalid reduce scale for decimal32")

	return reduceScale(n, reduce, round)
}

func (n Decimal64) ReduceScaleBy(reduce int32, round bool) Decimal64 {
	debug.Assert(reduce >= 0, "invalid reduce scale for decimal32")
	debug.Assert(reduce <= 18, "invalid reduce scale for decimal32")

	return reduceScale(n, reduce, round)
}

func rescale[T Decimal32 | Decimal64](n T, originalScale, newScale int32) (T, error) {
	if originalScale == newScale || n == 0 {
		return n, nil
	}

	deltaScale := int64(newScale) - int64(originalScale)
	absDeltaScale := deltaScale
	if absDeltaScale < 0 {
		absDeltaScale = -absDeltaScale
	}
	// Scaling any nonzero value by 10^(MaxPrecision+1) or more overflows or drops every digit.
	if absDeltaScale > int64(MaxPrecision[T]()) {
		return 0, errors.New("rescale data loss")
	}

	multiplier := T(intPow(10, absDeltaScale))
	if deltaScale < 0 {
		if n%multiplier != 0 {
			return 0, errors.New("rescale data loss")
		}
		return n / multiplier, nil
	}

	maxValue := T(int64(math.MaxInt64) >> (64 - 8*unsafe.Sizeof(n)))
	if n > maxValue/multiplier || n < (-maxValue-1)/multiplier {
		return 0, errors.New("rescale data loss")
	}
	return n * multiplier, nil
}

func (n Decimal32) Rescale(originalScale, newScale int32) (out Decimal32, err error) {
	return rescale(n, originalScale, newScale)
}

func (n Decimal64) Rescale(originalScale, newScale int32) (out Decimal64, err error) {
	return rescale(n, originalScale, newScale)
}

var (
	_ Num[Decimal32]  = Decimal32(0)
	_ Num[Decimal64]  = Decimal64(0)
	_ Num[Decimal128] = Decimal128{}
	_ Num[Decimal256] = Decimal256{}
)

type decComponents struct {
	wholeDigits string
	fractDigits string
	exp         int32
	sign        byte
	hasExponent bool
}

func isSign(s byte) bool {
	return s == '-' || s == '+'
}

func isDot(s byte) bool {
	return s == '.'
}

func startsExponent(s byte) bool {
	return s == 'e' || s == 'E'
}

func parseDigitsRun(s string) (digits, rest string) {
	pos := strings.IndexFunc(s, func(r rune) bool {
		return r < '0' || r > '9'
	})
	if pos == -1 {
		return s, ""
	}

	return s[:pos], s[pos:]
}

func parseDecimalComponents(s string) (d decComponents, valid bool) {
	if len(s) == 0 {
		return
	}

	if isSign(s[0]) {
		d.sign = s[0]
		s = s[1:]
	}

	// first run of digits
	d.wholeDigits, s = parseDigitsRun(s)
	if len(s) == 0 {
		return d, len(d.wholeDigits) > 0
	}

	if isDot(s[0]) {
		s = s[1:]
		// second run of digits
		d.fractDigits, s = parseDigitsRun(s)
	}

	if len(d.wholeDigits) == 0 && len(d.fractDigits) == 0 {
		// need at least some digits (whole or fractional)
		return
	}

	if len(s) == 0 {
		return d, true
	}

	// optional exponent
	if startsExponent(s[0]) {
		s = s[1:]
		if len(s) > 0 && s[0] == '+' {
			s = s[1:]
		}
		d.hasExponent = true
		exp, err := strconv.Atoi(s)
		if err != nil {
			return d, false
		}
		d.exp = int32(exp)
	}
	return d, len(s) == 0
}

func PrecScaleFromString(s string) (prec, scale int32, err error) {
	if len(s) == 0 {
		return 0, 0, errors.New("empty string cannot be parsed as decimal")
	}

	// parse the string into components
	d, valid := parseDecimalComponents(s)
	if !valid {
		return 0, 0, fmt.Errorf("the string '%s' is not a valid decimal number", s)
	}

	// remove leading zeros
	digits := strings.TrimLeft(d.wholeDigits, "0")
	significantDigits := len(d.fractDigits) + len(digits)
	prec = int32(significantDigits)

	if d.hasExponent {
		adjustedExponent := d.exp
		scale = -adjustedExponent + int32(len(d.fractDigits))
	} else {
		scale = int32(len(d.fractDigits))
	}

	return
}
