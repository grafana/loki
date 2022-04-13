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

// TraceID is an alias for pdata.TraceID struct.
type TraceID = pdata.TraceID

// InvalidTraceID is an alias for pdata.InvalidTraceID function.
var InvalidTraceID = pdata.InvalidTraceID

// NewTraceID is an alias for a function to create new TraceID.
var NewTraceID = pdata.NewTraceID
