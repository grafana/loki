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
	"time"

	mpb "google.golang.org/genproto/googleapis/type/month"
)

// ToMonth converts a google.type.Month to a golang Month.
func ToMonth(m mpb.Month) time.Month {
	return time.Month(m.Number())
}

// ToProtoMonth converts a golang Month to a google.type.Month.
func ToProtoMonth(m time.Month) mpb.Month {
	return mpb.Month(m)
}
