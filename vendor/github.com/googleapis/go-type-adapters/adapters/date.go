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

	dpb "google.golang.org/genproto/googleapis/type/date"
)

// ProtoDateToLocalTime returns a new Time based on the google.type.Date, in
// the system's time zone.
//
// Hours, minues, seconds, and nanoseconds are set to 0.
func ProtoDateToLocalTime(d *dpb.Date) time.Time {
	return ProtoDateToTime(d, time.Local)
}

// ProtoDateToUTCTime returns a new Time based on the google.type.Date, in UTC.
//
// Hours, minutes, seconds, and nanoseconds are set to 0.
func ProtoDateToUTCTime(d *dpb.Date) time.Time {
	return ProtoDateToTime(d, time.UTC)
}

// ProtoDateToTime returns a new Time based on the google.type.Date and provided
// *time.Location.
//
// Hours, minutes, seconds, and nanoseconds are set to 0.
func ProtoDateToTime(d *dpb.Date, l *time.Location) time.Time {
	return time.Date(int(d.GetYear()), time.Month(d.GetMonth()), int(d.GetDay()), 0, 0, 0, 0, l)
}

// TimeToProtoDate returns a new google.type.Date based on the provided time.Time.
// The location is ignored, as is anything more precise than the day.
func TimeToProtoDate(t time.Time) *dpb.Date {
	return &dpb.Date{
		Year:  int32(t.Year()),
		Month: int32(t.Month()),
		Day:   int32(t.Day()),
	}
}
