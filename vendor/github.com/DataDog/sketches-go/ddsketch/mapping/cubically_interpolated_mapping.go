// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2021 Datadog, Inc.

package mapping

import (
	"bytes"
	"errors"
	"fmt"
	"math"

	enc "github.com/DataDog/sketches-go/ddsketch/encoding"
	"github.com/DataDog/sketches-go/ddsketch/pb/sketchpb"
)

const (
	A = 6.0 / 35.0
	B = -3.0 / 5.0
	C = 10.0 / 7.0
)

// CubicallyInterpolatedMapping is a fast IndexMapping that approximates the
// memory-optimal LogarithmicMapping by extracting the floor value of the
// logarithm to the base 2 from the binary representations of floating-point
// values and cubically interpolating the logarithm in-between. More detailed
// documentation of this method can be found in: <a
// href="https://github.com/DataDog/sketches-java/">sketches-java</a>
type CubicallyInterpolatedMapping struct {
	gamma             float64 // base
	indexOffset       float64
	multiplier        float64 // precomputed for performance
	minIndexableValue float64
	maxIndexableValue float64
}

func NewCubicallyInterpolatedMapping(relativeAccuracy float64) (*CubicallyInterpolatedMapping, error) {
	if relativeAccuracy <= 0 || relativeAccuracy >= 1 {
		return nil, errors.New("The relative accuracy must be between 0 and 1.")
	}
	gamma := math.Pow((1+relativeAccuracy)/(1-relativeAccuracy), 10*math.Ln2/7) // > 1
	m, _ := NewCubicallyInterpolatedMappingWithGamma(gamma, 0)
	return m, nil
}

func NewCubicallyInterpolatedMappingWithGamma(gamma, indexOffset float64) (*CubicallyInterpolatedMapping, error) {
	if gamma <= 1 {
		return nil, errors.New("Gamma must be greater than 1.")
	}
	multiplier := 1 / math.Log2(gamma)
	adjustedGamma := math.Pow(gamma, 7/(10*math.Ln2))
	m := CubicallyInterpolatedMapping{
		gamma:       gamma,
		indexOffset: indexOffset,
		multiplier:  multiplier,
		minIndexableValue: math.Max(
			math.Exp2((math.MinInt32-indexOffset)/multiplier+1), // so that index >= MinInt32
			minNormalFloat64*adjustedGamma,
		),
		maxIndexableValue: math.Min(
			math.Exp2((math.MaxInt32-indexOffset)/multiplier-1),       // so that index <= MaxInt32
			math.Exp(expOverflow)/(2*adjustedGamma)*(adjustedGamma+1), // so that math.Exp does not overflow
		),
	}
	return &m, nil
}

func (m *CubicallyInterpolatedMapping) Equals(other IndexMapping) bool {
	o, ok := other.(*CubicallyInterpolatedMapping)
	if !ok {
		return false
	}
	tol := 1e-12
	return withinTolerance(m.gamma, o.gamma, tol) && withinTolerance(m.indexOffset, o.indexOffset, tol)
}

func (m *CubicallyInterpolatedMapping) Index(value float64) int {
	index := m.approximateLog(value)*m.multiplier + m.indexOffset
	if index >= 0 {
		return int(index)
	} else {
		return int(index) - 1
	}
}

func (m *CubicallyInterpolatedMapping) Value(index int) float64 {
	return m.LowerBound(index) * (1 + m.RelativeAccuracy())
}

func (m *CubicallyInterpolatedMapping) LowerBound(index int) float64 {
	return m.approximateInverseLog((float64(index) - m.indexOffset) / m.multiplier)
}

// Return an approximation of Math.log(x) / Math.log(base(2)).
func (m *CubicallyInterpolatedMapping) approximateLog(x float64) float64 {
	bits := math.Float64bits(x)
	e := getExponent(bits)
	s := getSignificandPlusOne(bits) - 1
	return ((A*s+B)*s+C)*s + e
}

// The exact inverse of approximateLog.
func (m *CubicallyInterpolatedMapping) approximateInverseLog(x float64) float64 {
	exponent := math.Floor(x)
	// Derived from Cardano's formula
	d0 := B*B - 3*A*C
	d1 := 2*B*B*B - 9*A*B*C - 27*A*A*(x-exponent)
	p := math.Cbrt((d1 - math.Sqrt(d1*d1-4*d0*d0*d0)) / 2)
	significandPlusOne := -(B+p+d0/p)/(3*A) + 1
	return buildFloat64(int(exponent), significandPlusOne)
}

func (m *CubicallyInterpolatedMapping) MinIndexableValue() float64 {
	return m.minIndexableValue
}

func (m *CubicallyInterpolatedMapping) MaxIndexableValue() float64 {
	return m.maxIndexableValue
}

func (m *CubicallyInterpolatedMapping) RelativeAccuracy() float64 {
	return 1 - 2/(1+math.Exp(7.0/10*math.Log2(m.gamma)))
}

func (m *CubicallyInterpolatedMapping) ToProto() *sketchpb.IndexMapping {
	return &sketchpb.IndexMapping{
		Gamma:         m.gamma,
		IndexOffset:   m.indexOffset,
		Interpolation: sketchpb.IndexMapping_CUBIC,
	}
}

func (m *CubicallyInterpolatedMapping) Encode(b *[]byte) {
	enc.EncodeFlag(b, enc.FlagIndexMappingBaseCubic)
	enc.EncodeFloat64LE(b, m.gamma)
	enc.EncodeFloat64LE(b, m.indexOffset)
}

func (m *CubicallyInterpolatedMapping) string() string {
	var buffer bytes.Buffer
	buffer.WriteString(fmt.Sprintf("gamma: %v, indexOffset: %v\n", m.gamma, m.indexOffset))
	return buffer.String()
}

var _ IndexMapping = (*CubicallyInterpolatedMapping)(nil)
