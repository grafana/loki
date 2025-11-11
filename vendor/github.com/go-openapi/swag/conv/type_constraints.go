// Copyright 2015 go-swagger maintainers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package conv

type (
	// these type constraints are redefined after golang.org/x/exp/constraints,
	// because importing that package causes an undesired go upgrade.

	// Signed integer types, cf. [golang.org/x/exp/constraints.Signed]
	Signed interface {
		~int | ~int8 | ~int16 | ~int32 | ~int64
	}

	// Unsigned integer types, cf. [golang.org/x/exp/constraints.Unsigned]
	Unsigned interface {
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
	}

	// Float numerical types, cf. [golang.org/x/exp/constraints.Float]
	Float interface {
		~float32 | ~float64
	}

	// Numerical types
	Numerical interface {
		Signed | Unsigned | Float
	}
)
