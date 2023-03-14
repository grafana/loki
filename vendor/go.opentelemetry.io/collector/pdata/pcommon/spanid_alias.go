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

package pcommon // import "go.opentelemetry.io/collector/pdata/pcommon"

import "go.opentelemetry.io/collector/pdata/internal"

// SpanID is span identifier.
type SpanID = internal.SpanID

// InvalidSpanID returns an empty (all zero bytes) SpanID.
var InvalidSpanID = internal.InvalidSpanID

// NewSpanID returns a new SpanID from the given byte array.
var NewSpanID = internal.NewSpanID
