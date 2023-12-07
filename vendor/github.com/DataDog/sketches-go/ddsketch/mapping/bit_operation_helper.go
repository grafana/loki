// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2021 Datadog, Inc.

package mapping

import (
	"math"
)

const (
	exponentBias     = 1023
	exponentMask     = uint64(0x7FF0000000000000)
	exponentShift    = 52
	significandMask  = uint64(0x000fffffffffffff)
	significandWidth = 53
	oneMask          = uint64(0x3ff0000000000000)
)

func getExponent(float64Bits uint64) float64 {
	return float64(int((float64Bits&exponentMask)>>exponentShift) - exponentBias)
}

func getSignificandPlusOne(float64Bits uint64) float64 {
	return math.Float64frombits((float64Bits & significandMask) | oneMask)
}

// exponent should be >= -1022 and <= 1023
// significandPlusOne should be >= 1 and < 2
func buildFloat64(exponent int, significandPlusOne float64) float64 {
	return math.Float64frombits(
		(uint64((exponent+exponentBias)<<exponentShift) & exponentMask) | (math.Float64bits(significandPlusOne) & significandMask),
	)
}
