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

import "go.opentelemetry.io/collector/pdata/pcommon"

// SpanID is an alias for pcommon.SpanID struct.
// Deprecated: [v0.49.0] Use pcommon.SpanID instead.
type SpanID = pcommon.SpanID

// InvalidSpanID is an alias for pcommon.InvalidSpanID function.
// Deprecated: [v0.49.0] Use pcommon.InvalidSpanID instead.
var InvalidSpanID = pcommon.InvalidSpanID

// NewSpanID is an alias for a function to create new SpanID.
// Deprecated: [v0.49.0] Use pcommon.NewSpanID instead.
var NewSpanID = pcommon.NewSpanID
