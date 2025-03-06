// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2021 Datadog, Inc.

package mapping

import (
	"errors"
	"fmt"

	enc "github.com/DataDog/sketches-go/ddsketch/encoding"
	"github.com/DataDog/sketches-go/ddsketch/pb/sketchpb"
)

const (
	expOverflow      = 7.094361393031e+02      // The value at which math.Exp overflows
	minNormalFloat64 = 2.2250738585072014e-308 //2^(-1022)
)

type IndexMapping interface {
	Equals(other IndexMapping) bool
	Index(value float64) int
	Value(index int) float64
	LowerBound(index int) float64
	RelativeAccuracy() float64
	// MinIndexableValue returns the minimum positive value that can be mapped to an index.
	MinIndexableValue() float64
	// MaxIndexableValue returns the maximum positive value that can be mapped to an index.
	MaxIndexableValue() float64
	ToProto() *sketchpb.IndexMapping
	EncodeProto(builder *sketchpb.IndexMappingBuilder)
	// Encode encodes a mapping and appends its content to the provided []byte.
	Encode(b *[]byte)
}

func NewDefaultMapping(relativeAccuracy float64) (IndexMapping, error) {
	return NewLogarithmicMapping(relativeAccuracy)
}

// FromProto returns an Index mapping from the protobuf definition of it
func FromProto(m *sketchpb.IndexMapping) (IndexMapping, error) {
	if m == nil {
		return nil, errors.New("cannot create IndexMapping from nil protobuf index mapping")
	}
	switch m.Interpolation {
	case sketchpb.IndexMapping_NONE:
		return NewLogarithmicMappingWithGamma(m.Gamma, m.IndexOffset)
	case sketchpb.IndexMapping_LINEAR:
		return NewLinearlyInterpolatedMappingWithGamma(m.Gamma, m.IndexOffset)
	case sketchpb.IndexMapping_CUBIC:
		return NewCubicallyInterpolatedMappingWithGamma(m.Gamma, m.IndexOffset)
	default:
		return nil, fmt.Errorf("interpolation not supported: %d", m.Interpolation)
	}
}

// Decode decodes a mapping and updates the provided []byte so that it starts
// immediately after the encoded mapping.
func Decode(b *[]byte, flag enc.Flag) (IndexMapping, error) {
	switch flag {

	case enc.FlagIndexMappingBaseLogarithmic:
		gamma, indexOffset, err := decodeLogLikeIndexMapping(b)
		if err != nil {
			return nil, err
		}
		return NewLogarithmicMappingWithGamma(gamma, indexOffset)

	case enc.FlagIndexMappingBaseLinear:
		gamma, indexOffset, err := decodeLogLikeIndexMapping(b)
		if err != nil {
			return nil, err
		}
		return NewLinearlyInterpolatedMappingWithGamma(gamma, indexOffset)

	case enc.FlagIndexMappingBaseCubic:
		gamma, indexOffset, err := decodeLogLikeIndexMapping(b)
		if err != nil {
			return nil, err
		}
		return NewCubicallyInterpolatedMappingWithGamma(gamma, indexOffset)

	default:
		return nil, errors.New("unknown mapping")
	}
}

func decodeLogLikeIndexMapping(b *[]byte) (gamma, indexOffset float64, err error) {
	gamma, err = enc.DecodeFloat64LE(b)
	if err != nil {
		return
	}
	indexOffset, err = enc.DecodeFloat64LE(b)
	return
}
