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

import (
	"time"
)

// Timestamp is a time specified as UNIX Epoch time in nanoseconds since
// 1970-01-01 00:00:00 +0000 UTC.
type Timestamp uint64

// NewTimestampFromTime constructs a new Timestamp from the provided time.Time.
func NewTimestampFromTime(t time.Time) Timestamp {
	return Timestamp(uint64(t.UnixNano()))
}

// AsTime converts this to a time.Time.
func (ts Timestamp) AsTime() time.Time {
	return time.Unix(0, int64(ts)).UTC()
}

// String returns the string representation of this in UTC.
func (ts Timestamp) String() string {
	return ts.AsTime().String()
}
