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
	"fmt"
	"regexp"
	"strconv"
	"time"

	dtpb "google.golang.org/genproto/googleapis/type/datetime"
	durpb "google.golang.org/protobuf/types/known/durationpb"
)

// ProtoDateTimeToTime returns a new Time based on the google.type.DateTime.
//
// It errors if it gets invalid time zone information.
func ProtoDateTimeToTime(d *dtpb.DateTime) (time.Time, error) {
	var err error

	// Determine the location.
	loc := time.UTC
	if tz := d.GetTimeZone(); tz != nil {
		loc, err = time.LoadLocation(tz.GetId())
		if err != nil {
			return time.Time{}, err
		}
	}
	if offset := d.GetUtcOffset(); offset != nil {
		hours := int(offset.GetSeconds()) / 3600
		loc = time.FixedZone(fmt.Sprintf("UTC%+d", hours), hours)
	}

	// Return the Time.
	return time.Date(
		int(d.GetYear()),
		time.Month(d.GetMonth()),
		int(d.GetDay()),
		int(d.GetHours()),
		int(d.GetMinutes()),
		int(d.GetSeconds()),
		int(d.GetNanos()),
		loc,
	), nil
}

// TimeToProtoDateTime returns a new google.type.DateTime based on the
// provided time.Time.
//
// It errors if it gets invalid time zone information.
func TimeToProtoDateTime(t time.Time) (*dtpb.DateTime, error) {
	dt := &dtpb.DateTime{
		Year:    int32(t.Year()),
		Month:   int32(t.Month()),
		Day:     int32(t.Day()),
		Hours:   int32(t.Hour()),
		Minutes: int32(t.Minute()),
		Seconds: int32(t.Second()),
		Nanos:   int32(t.Nanosecond()),
	}

	// If the location is a UTC offset, encode it as such in the proto.
	loc := t.Location().String()
	if match := offsetRegexp.FindStringSubmatch(loc); len(match) > 0 {
		offsetInt, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, err
		}
		dt.TimeOffset = &dtpb.DateTime_UtcOffset{
			UtcOffset: &durpb.Duration{Seconds: int64(offsetInt) * 3600},
		}
	} else if loc != "" {
		dt.TimeOffset = &dtpb.DateTime_TimeZone{
			TimeZone: &dtpb.TimeZone{Id: loc},
		}
	}

	return dt, nil
}

var offsetRegexp = regexp.MustCompile(`^UTC([+-][\d]{1,2})$`)
