// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2021 Datadog, Inc.

package encoding

import (
	"io"
)

// An encoded DDSketch comprises multiple contiguous blocks (sequences of
// bytes). Each block is prefixed with a flag that indicates what the block
// contains and how the data is encoded in the block.
//
// A flag is a single byte, which itself contains two parts:
// - the flag type (the 2 least significant bits),
// - the subflag (the 6 most significant bits).
//
// There are four flag types, for:
// - sketch features,
// - index mapping,
// - positive value store,
// - negative value store.
//
// The meaning of the subflag depends on the flag type:
// - for the sketch feature flag type, it indicates what feature is encoded,
// - for the index mapping flag type, it indicates what mapping is encoded and
// how,
// - for the store flag types, it indicates how bins are encoded.

const (
	numBitsForType byte = 2
	flagTypeMask   byte = (1 << numBitsForType) - 1
	subFlagMask    byte = ^flagTypeMask
)

type Flag struct{ byte }
type FlagType struct{ byte } // mask: 0b00000011
type SubFlag struct{ byte }  // mask: 0b11111100

var (
	// FLAG TYPES

	flagTypeSketchFeatures = FlagType{0b00}
	FlagTypeIndexMapping   = FlagType{0b10}
	FlagTypePositiveStore  = FlagType{0b01}
	FlagTypeNegativeStore  = FlagType{0b11}

	// SKETCH FEATURES

	// Encodes the count of the zero bin.
	// Encoding format:
	// - [byte] flag
	// - [varfloat64] count of the zero bin
	FlagZeroCountVarFloat = NewFlag(flagTypeSketchFeatures, newSubFlag(1))

	// Encode the total count.
	// Encoding format:
	// - [byte] flag
	// - [varfloat64] total count
	FlagCount = NewFlag(flagTypeSketchFeatures, newSubFlag(0x28))

	// Encode the summary statistics.
	// Encoding format:
	// - [byte] flag
	// - [float64LE] summary stat
	FlagSum = NewFlag(flagTypeSketchFeatures, newSubFlag(0x21))
	FlagMin = NewFlag(flagTypeSketchFeatures, newSubFlag(0x22))
	FlagMax = NewFlag(flagTypeSketchFeatures, newSubFlag(0x23))

	// INDEX MAPPING

	// Encodes log-like index mappings, specifying the base (gamma) and the index offset
	// The subflag specifies the interpolation method.
	// Encoding format:
	// - [byte] flag
	// - [float64LE] gamma
	// - [float64LE] index offset
	FlagIndexMappingBaseLogarithmic = NewFlag(FlagTypeIndexMapping, newSubFlag(0))
	FlagIndexMappingBaseLinear      = NewFlag(FlagTypeIndexMapping, newSubFlag(1))
	FlagIndexMappingBaseQuadratic   = NewFlag(FlagTypeIndexMapping, newSubFlag(2))
	FlagIndexMappingBaseCubic       = NewFlag(FlagTypeIndexMapping, newSubFlag(3))
	FlagIndexMappingBaseQuartic     = NewFlag(FlagTypeIndexMapping, newSubFlag(4))

	// BINS

	// Encodes N bins, each one with its index and its count.
	// Indexes are delta-encoded.
	// Encoding format:
	// - [byte] flag
	// - [uvarint64] number of bins N
	// - [varint64] index of first bin
	// - [varfloat64] count of first bin
	// - [varint64] difference between the index of the second bin and the index
	// of the first bin
	// - [varfloat64] count of second bin
	// - ...
	// - [varint64] difference between the index of the N-th bin and the index
	// of the (N-1)-th bin
	// - [varfloat64] count of N-th bin
	BinEncodingIndexDeltasAndCounts = newSubFlag(1)

	// Encodes N bins whose counts are each equal to 1.
	// Indexes are delta-encoded.
	// Encoding format:
	// - [byte] flag
	// - [uvarint64] number of bins N
	// - [varint64] index of first bin
	// - [varint64] difference between the index of the second bin and the index
	// of the first bin
	// - ...
	// - [varint64] difference between the index of the N-th bin and the index
	// of the (N-1)-th bin
	BinEncodingIndexDeltas = newSubFlag(2)

	// Encodes N contiguous bins, specifiying the count of each one
	// Encoding format:
	// - [byte] flag
	// - [uvarint64] number of bins N
	// - [varint64] index of first bin
	// - [varint64] difference between two successive indexes
	// - [varfloat64] count of first bin
	// - [varfloat64] count of second bin
	// - ...
	// - [varfloat64] count of N-th bin
	BinEncodingContiguousCounts = newSubFlag(3)
)

func NewFlag(t FlagType, s SubFlag) Flag {
	return Flag{t.byte | s.byte}
}

func (f Flag) Type() FlagType {
	return FlagType{f.byte & flagTypeMask}
}

func (f Flag) SubFlag() SubFlag {
	return SubFlag{f.byte & subFlagMask}
}

func newSubFlag(b byte) SubFlag {
	return SubFlag{b << numBitsForType}
}

// EncodeFlag encodes a flag and appends its content to the provided []byte.
func EncodeFlag(b *[]byte, f Flag) {
	*b = append(*b, f.byte)
}

// DecodeFlag decodes a flag and updates the provided []byte so that it starts
// immediately after the encoded flag.
func DecodeFlag(b *[]byte) (Flag, error) {
	if len(*b) == 0 {
		return Flag{}, io.EOF
	}
	flag := Flag{(*b)[0]}
	*b = (*b)[1:]
	return flag, nil
}
