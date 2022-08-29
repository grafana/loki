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
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strings"

	dpb "google.golang.org/genproto/googleapis/type/decimal"
)

// ProtoDecimalToFloat converts the provided google.type.Decimal to a big.Float.
func ProtoDecimalToFloat(d *dpb.Decimal) (*big.Float, error) {
	value := strings.ToLower(d.GetValue())

	// Determine the required precision.
	v := value
	if strings.ContainsRune(v, 'e') {
		v = v[0:strings.IndexRune(v, 'e')]
	}
	v = nan.ReplaceAllLiteralString(v, "")
	prec := uint(math.Pow(2, float64(len(v)+1)))

	// Parse and return a big.Float.
	f, _, err := big.ParseFloat(value, 10, prec, big.AwayFromZero)
	return f, err
}

// ProtoDecimalToFloat64 converts the provided google.type.Decimal to a float64.
func ProtoDecimalToFloat64(d *dpb.Decimal) (float64, big.Accuracy, error) {
	f, err := ProtoDecimalToFloat(d)
	if err != nil {
		return 0.0, big.Exact, err
	}
	f64, accuracy := f.Float64()
	return f64, accuracy, nil
}

// Float64ToProtoDecimal converts the provided float64 to a google.type.Decimal.
func Float64ToProtoDecimal(f float64) *dpb.Decimal {
	return &dpb.Decimal{
		Value: fmt.Sprintf("%f", f),
	}
}

// FloatToProtoDecimal converts the provided big.Float to a google.type.Decimal.
func FloatToProtoDecimal(f *big.Float) *dpb.Decimal {
	return &dpb.Decimal{
		Value: f.String(),
	}
}

var nan = regexp.MustCompile(`[^\d]`)
