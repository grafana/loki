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

// LogarithmicMapping is an IndexMapping that is memory-optimal, that is to say
// that given a targeted relative accuracy, it requires the least number of
// indices to cover a given range of values. This is done by logarithmically
// mapping floating-point values to integers.
type LogarithmicMapping struct {
	gamma             float64 // base
	indexOffset       float64
	multiplier        float64 // precomputed for performance
	minIndexableValue float64
	maxIndexableValue float64
}

func NewLogarithmicMapping(relativeAccuracy float64) (*LogarithmicMapping, error) {
	if relativeAccuracy <= 0 || relativeAccuracy >= 1 {
		return nil, errors.New("The relative accuracy must be between 0 and 1.")
	}
	gamma := (1 + relativeAccuracy) / (1 - relativeAccuracy) // > 1
	m, _ := NewLogarithmicMappingWithGamma(gamma, 0)
	return m, nil
}

func NewLogarithmicMappingWithGamma(gamma, indexOffset float64) (*LogarithmicMapping, error) {
	if gamma <= 1 {
		return nil, errors.New("Gamma must be greater than 1.")
	}
	multiplier := 1 / math.Log(gamma)
	m := &LogarithmicMapping{
		gamma:       gamma,
		indexOffset: indexOffset,
		multiplier:  multiplier,
		minIndexableValue: math.Max(
			math.Exp((math.MinInt32-indexOffset)/multiplier+1), // so that index >= MinInt32
			minNormalFloat64*gamma,
		),
		maxIndexableValue: math.Min(
			math.Exp((math.MaxInt32-indexOffset)/multiplier-1), // so that index <= MaxInt32
			math.Exp(expOverflow)/(2*gamma)*(gamma+1),          // so that math.Exp does not overflow
		),
	}
	return m, nil
}

func (m *LogarithmicMapping) Equals(other IndexMapping) bool {
	o, ok := other.(*LogarithmicMapping)
	if !ok {
		return false
	}
	tol := 1e-12
	return withinTolerance(m.gamma, o.gamma, tol) && withinTolerance(m.indexOffset, o.indexOffset, tol)
}

func (m *LogarithmicMapping) Index(value float64) int {
	index := math.Log(value)*m.multiplier + m.indexOffset
	if index >= 0 {
		return int(index)
	} else {
		return int(index) - 1 // faster than Math.Floor
	}
}

func (m *LogarithmicMapping) Value(index int) float64 {
	return m.LowerBound(index) * (1 + m.RelativeAccuracy())
}

func (m *LogarithmicMapping) LowerBound(index int) float64 {
	return math.Exp((float64(index) - m.indexOffset) / m.multiplier)
}

func (m *LogarithmicMapping) MinIndexableValue() float64 {
	return m.minIndexableValue
}

func (m *LogarithmicMapping) MaxIndexableValue() float64 {
	return m.maxIndexableValue
}

func (m *LogarithmicMapping) RelativeAccuracy() float64 {
	return 1 - 2/(1+m.gamma)
}

// Generates a protobuf representation of this LogarithicMapping.
func (m *LogarithmicMapping) ToProto() *sketchpb.IndexMapping {
	return &sketchpb.IndexMapping{
		Gamma:         m.gamma,
		IndexOffset:   m.indexOffset,
		Interpolation: sketchpb.IndexMapping_NONE,
	}
}

func (m *LogarithmicMapping) Encode(b *[]byte) {
	enc.EncodeFlag(b, enc.FlagIndexMappingBaseLogarithmic)
	enc.EncodeFloat64LE(b, m.gamma)
	enc.EncodeFloat64LE(b, m.indexOffset)
}

func (m *LogarithmicMapping) string() string {
	var buffer bytes.Buffer
	buffer.WriteString(fmt.Sprintf("gamma: %v, indexOffset: %v\n", m.gamma, m.indexOffset))
	return buffer.String()
}

var _ IndexMapping = (*LogarithmicMapping)(nil)
