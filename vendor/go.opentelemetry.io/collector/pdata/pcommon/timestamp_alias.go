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

// Timestamp is a time specified as UNIX Epoch time in nanoseconds since
// 1970-01-01 00:00:00 +0000 UTC.
type Timestamp = internal.Timestamp

// NewTimestampFromTime constructs a new Timestamp from the provided time.Time.
var NewTimestampFromTime = internal.NewTimestampFromTime
