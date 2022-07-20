// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package adapters

import (
	"math/big"

	fpb "google.golang.org/genproto/googleapis/type/fraction"
)

// ProtoFractionToRat returns a math/big Rat (rational number) based on the given
// google.type.fraction.
func ProtoFractionToRat(f *fpb.Fraction) *big.Rat {
	return big.NewRat(f.GetNumerator(), f.GetDenominator())
}

// RatToProtoFraction returns a google.type.Fraction from a math/big Rat.
func RatToProtoFraction(r *big.Rat) *fpb.Fraction {
	return &fpb.Fraction{
		Numerator:   r.Num().Int64(),
		Denominator: r.Denom().Int64(),
	}
}
