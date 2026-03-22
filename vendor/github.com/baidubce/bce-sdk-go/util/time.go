/*
 * Copyright 2017 Baidu, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 */

// time.go - define the time utility functions for BCE

package util

import "time"

const (
	RFC822Format  = "Mon, 02 Jan 2006 15:04:05 MST"
	ISO8601Format = "2006-01-02T15:04:05Z"
)

func NowUTCSeconds() int64 { return time.Now().UTC().Unix() }

func NowUTCNanoSeconds() int64 { return time.Now().UTC().UnixNano() }

func FormatRFC822Date(timestamp_second int64) string {
	tm := time.Unix(timestamp_second, 0).UTC()
	return tm.Format(RFC822Format)
}

func ParseRFC822Date(time_string string) (time.Time, error) {
	return time.Parse(RFC822Format, time_string)
}

func FormatISO8601Date(timestamp_second int64) string {
	tm := time.Unix(timestamp_second, 0).UTC()
	return tm.Format(ISO8601Format)
}

func ParseISO8601Date(time_string string) (time.Time, error) {
	return time.Parse(ISO8601Format, time_string)
}
