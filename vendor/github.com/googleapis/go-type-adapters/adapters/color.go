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
	"image/color"
	"math"

	cpb "google.golang.org/genproto/googleapis/type/color"
	wpb "google.golang.org/protobuf/types/known/wrapperspb"
)

// ProtoColorToRGBA returns an RGBA based on the provided google.type.Color.
// If alpha is not set in the proto, full opacity is assumed.
//
// Note: Converting between a float using [0, 1] to an int using [0, 256)
// causes some cognitive dissonance between accuracy and user expectations.
// For example, most people writing CSS use 0x80 (decimal 128) to mean "half",
// but it is not actually half (it is slightly over). There is actually no
// way to precisely specify the 0.5 float value in a [0, 256) range of
// integers.
//
// This function uses math.Round to address this, meaning that 0.5 will be
// rounded up to 128 rather than rounded down to 127.
//
// Because of this fuzziness and precision loss, it is NOT guaranteed that
// ProtoColorToRGBA and RGBAToProtoColor are exact inverses, and both functions
// will lose precision.
func ProtoColorToRGBA(c *cpb.Color) *color.RGBA {
	// Determine the appropriate alpha value.
	// If alpha is unset, full opacity is the proper default.
	alpha := uint8(255)
	if c.Alpha != nil {
		alpha = uint8(math.Round(float64(c.GetAlpha().GetValue() * 255)))
	}

	// Return the RGBA.
	return &color.RGBA{
		R: uint8(math.Round(float64(c.GetRed()) * 255)),
		G: uint8(math.Round(float64(c.GetGreen()) * 255)),
		B: uint8(math.Round(float64(c.GetBlue()) * 255)),
		A: alpha,
	}
}

// RGBAToProtoColor returns a google.type.Color based on the provided RGBA.
//
// Note: Converting between ints using [0, 256) and a float using [0, 1]
// causes some cognitive dissonance between accuracy and user expectations.
// For example, most people using CSS use 0x80 (decimal 128) to mean "half",
// but it is not actually half (it is slightly over). These is actually no
// way to precisely specify the 0.5 float value in a [0, 256) range of
// integers.
//
// This function addresses this by limiting decimal precision to 0.01, on
// the rationale that most precision beyond this point is probably
// unintentional.
//
// Because of this fuzziness and precision loss, it is NOT guaranteed that
// ProtoColorToRGBA and RGBAToProtoColor are exact inverses, and both functions
// will lose precision.
func RGBAToProtoColor(rgba *color.RGBA) *cpb.Color {
	return &cpb.Color{
		Red:   float32(int(rgba.R)*100/255) / 100,
		Green: float32(int(rgba.G)*100/255) / 100,
		Blue:  float32(int(rgba.B)*100/255) / 100,
		Alpha: &wpb.FloatValue{Value: float32(int(rgba.A)*100/255) / 100},
	}
}
