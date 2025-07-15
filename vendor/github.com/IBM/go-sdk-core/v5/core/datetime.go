package core

/**
 * (C) Copyright IBM Corp. 2020.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

import (
	"time"

	"github.com/go-openapi/strfmt"
)

// Customize the strfmt DateTime parsing and formatting for our use.
func init() {
	// Force date-time serialization to use the UTC representation.
	strfmt.NormalizeTimeForMarshal = NormalizeDateTimeUTC

	// These formatting layouts (supported by time.Time.Format()) are added to the set of layouts used
	// by the strfmt.DateTime unmarshalling function(s).

	// RFC 3339 but with 2-digit tz-offset.
	// yyyy-MM-ddThh:mm:ss.SSS<tz-offset>, where tz-offset is 'Z', +HH or -HH
	rfc3339TZ2Layout := "2006-01-02T15:04:05.000Z07"

	// RFC 3339 but with only seconds precision.
	// yyyy-MM-ddThh:mm:ss<tz-offset>, where tz-offset is 'Z', +HH:MM or -HH:MM
	secsPrecisionLayout := "2006-01-02T15:04:05Z07:00"
	// Seconds precision with no colon in tz-offset
	secsPrecisionNoColonLayout := "2006-01-02T15:04:05Z0700"
	// Seconds precision with 2-digit tz-offset
	secsPrecisionTZ2Layout := "2006-01-02T15:04:05Z07"

	// RFC 3339 but with only minutes precision.
	// yyyy-MM-ddThh:mm<tz-offset>, where tz-offset is 'Z' or +HH:MM or -HH:MM
	minPrecisionLayout := "2006-01-02T15:04Z07:00"
	// Minutes precision with no colon in tz-offset
	minPrecisionNoColonLayout := "2006-01-02T15:04Z0700"
	// Minutes precision with 2-digit tz-offset
	minPrecisionTZ2Layout := "2006-01-02T15:04Z07"

	// "Dialog" format.
	// yyyy-MM-dd hh:mm:ss (no tz-offset)
	dialogLayout := "2006-01-02 15:04:05"

	// Register our parsing layouts with the strfmt package.
	strfmt.DateTimeFormats =
		append(strfmt.DateTimeFormats,
			rfc3339TZ2Layout,
			secsPrecisionLayout,
			secsPrecisionNoColonLayout,
			secsPrecisionTZ2Layout,
			minPrecisionLayout,
			minPrecisionNoColonLayout,
			minPrecisionTZ2Layout,
			dialogLayout)
}

// NormalizeDateTimeUTC normalizes t to reflect UTC timezone for marshaling
func NormalizeDateTimeUTC(t time.Time) time.Time {
	return t.UTC()
}

// ParseDate parses the specified RFC3339 full-date string (YYYY-MM-DD) and returns a strfmt.Date instance.
// If the string is empty the return value will be the unix epoch (1970-01-01).
func ParseDate(dateString string) (fmtDate strfmt.Date, err error) {
	if dateString == "" {
		return strfmt.Date(time.Unix(0, 0).UTC()), nil
	}

	formattedTime, err := time.Parse(strfmt.RFC3339FullDate, dateString)
	if err == nil {
		fmtDate = strfmt.Date(formattedTime)
	} else {
		err = SDKErrorf(err, "", "date-parse-error", getComponentInfo())
	}
	return
}

// ParseDateTime parses the specified date-time string and returns a strfmt.DateTime instance.
// If the string is empty the return value will be the unix epoch (1970-01-01T00:00:00.000Z).
func ParseDateTime(dateString string) (strfmt.DateTime, error) {
	dt, err := strfmt.ParseDateTime(dateString)
	if err != nil {
		err = SDKErrorf(err, "", "datetime-parse-error", getComponentInfo())
	}
	return dt, err
}
