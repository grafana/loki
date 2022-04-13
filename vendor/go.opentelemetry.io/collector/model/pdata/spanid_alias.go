// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pdata // import "go.opentelemetry.io/collector/model/pdata"

import "go.opentelemetry.io/collector/model/internal/pdata"

// SpanID is an alias for pdata.SpanID struct.
type SpanID = pdata.SpanID

// InvalidSpanID is an alias for pdata.InvalidSpanID function.
var InvalidSpanID = pdata.InvalidSpanID

// NewSpanID is an alias for a function to create new SpanID.
var NewSpanID = pdata.NewSpanID
