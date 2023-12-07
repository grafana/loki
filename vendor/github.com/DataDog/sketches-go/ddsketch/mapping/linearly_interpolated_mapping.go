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

// LinearlyInterpolatedMapping is a fast IndexMapping that approximates the
// memory-optimal LogarithmicMapping by extracting the floor value of the
// logarithm to the base 2 from the binary representations of floating-point
// values and linearly interpolating the logarithm in-between.
type LinearlyInterpolatedMapping struct {
	gamma             float64 // base
	indexOffset       float64
	multiplier        float64 // precomputed for performance
	minIndexableValue float64
	maxIndexableValue float64
}

func NewLinearlyInterpolatedMapping(relativeAccuracy float64) (*LinearlyInterpolatedMapping, error) {
	if relativeAccuracy <= 0 || relativeAccuracy >= 1 {
		return nil, errors.New("The relative accuracy must be between 0 and 1.")
	}
	gamma := math.Pow((1+relativeAccuracy)/(1-relativeAccuracy), math.Ln2) // > 1
	indexOffset := 1 / math.Log2(gamma)                                    // for backward compatibility
	m, _ := NewLinearlyInterpolatedMappingWithGamma(gamma, indexOffset)
	return m, nil
}

func NewLinearlyInterpolatedMappingWithGamma(gamma, indexOffset float64) (*LinearlyInterpolatedMapping, error) {
	if gamma <= 1 {
		return nil, errors.New("Gamma must be greater than 1.")
	}
	multiplier := 1 / math.Log2(gamma)
	adjustedGamma := math.Pow(gamma, 1/math.Ln2)
	m := LinearlyInterpolatedMapping{
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

func (m *LinearlyInterpolatedMapping) Equals(other IndexMapping) bool {
	o, ok := other.(*LinearlyInterpolatedMapping)
	if !ok {
		return false
	}
	tol := 1e-12
	return withinTolerance(m.gamma, o.gamma, tol) && withinTolerance(m.indexOffset, o.indexOffset, tol)
}

func (m *LinearlyInterpolatedMapping) Index(value float64) int {
	index := m.approximateLog(value)*m.multiplier + m.indexOffset
	if index >= 0 {
		return int(index)
	} else {
		return int(index) - 1
	}
}

func (m *LinearlyInterpolatedMapping) Value(index int) float64 {
	return m.LowerBound(index) * (1 + m.RelativeAccuracy())
}

func (m *LinearlyInterpolatedMapping) LowerBound(index int) float64 {
	return m.approximateInverseLog((float64(index) - m.indexOffset) / m.multiplier)
}

// Return an approximation of Math.log(x) / Math.log(2)
func (m *LinearlyInterpolatedMapping) approximateLog(x float64) float64 {
	bits := math.Float64bits(x)
	return getExponent(bits) + getSignificandPlusOne(bits) - 1
}

// The exact inverse of approximateLog.
func (m *LinearlyInterpolatedMapping) approximateInverseLog(x float64) float64 {
	exponent := math.Floor(x)
	significandPlusOne := x - exponent + 1
	return buildFloat64(int(exponent), significandPlusOne)
}

func (m *LinearlyInterpolatedMapping) MinIndexableValue() float64 {
	return m.minIndexableValue
}

func (m *LinearlyInterpolatedMapping) MaxIndexableValue() float64 {
	return m.maxIndexableValue
}

func (m *LinearlyInterpolatedMapping) RelativeAccuracy() float64 {
	return 1 - 2/(1+math.Exp(math.Log2(m.gamma)))
}

// Generates a protobuf representation of this LinearlyInterpolatedMapping.
func (m *LinearlyInterpolatedMapping) ToProto() *sketchpb.IndexMapping {
	return &sketchpb.IndexMapping{
		Gamma:         m.gamma,
		IndexOffset:   m.indexOffset,
		Interpolation: sketchpb.IndexMapping_LINEAR,
	}
}

func (m *LinearlyInterpolatedMapping) Encode(b *[]byte) {
	enc.EncodeFlag(b, enc.FlagIndexMappingBaseLinear)
	enc.EncodeFloat64LE(b, m.gamma)
	enc.EncodeFloat64LE(b, m.indexOffset)
}

func (m *LinearlyInterpolatedMapping) string() string {
	var buffer bytes.Buffer
	buffer.WriteString(fmt.Sprintf("gamma: %v, indexOffset: %v\n", m.gamma, m.indexOffset))
	return buffer.String()
}

func withinTolerance(x, y, tolerance float64) bool {
	if x == 0 || y == 0 {
		return math.Abs(x) <= tolerance && math.Abs(y) <= tolerance
	} else {
		return math.Abs(x-y) <= tolerance*math.Max(math.Abs(x), math.Abs(y))
	}
}

var _ IndexMapping = (*LinearlyInterpolatedMapping)(nil)
