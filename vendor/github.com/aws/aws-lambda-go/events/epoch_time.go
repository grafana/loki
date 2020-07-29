// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.

package events

import (
	"encoding/json"
	"time"
)

// SecondsEpochTime serializes a time.Time in JSON as a UNIX epoch time in seconds
type SecondsEpochTime struct {
	time.Time
}

// MilliSecondsEpochTime serializes a time.Time in JSON as a UNIX epoch time in milliseconds.
type MilliSecondsEpochTime struct {
	time.Time
}

const secondsToNanoSecondsFactor = 1000000000
const milliSecondsToNanoSecondsFactor = 1000000

func (e SecondsEpochTime) MarshalJSON() ([]byte, error) {
	// UnixNano() returns the epoch in nanoseconds
	unixTime := float64(e.UnixNano()) / float64(secondsToNanoSecondsFactor)
	return json.Marshal(unixTime)
}

func (e *SecondsEpochTime) UnmarshalJSON(b []byte) error {
	var epoch float64
	err := json.Unmarshal(b, &epoch)
	if err != nil {
		return err
	}

	epochSec := int64(epoch)
	epochNano := int64((epoch - float64(epochSec)) * float64(secondsToNanoSecondsFactor))

	// time.Unix(sec, nsec) expects the epoch integral seconds in the first parameter
	// and remaining nanoseconds in the second parameter
	*e = SecondsEpochTime{time.Unix(epochSec, epochNano)}
	return nil
}

func (e MilliSecondsEpochTime) MarshalJSON() ([]byte, error) {
	// UnixNano() returns the epoch in nanoseconds
	unixTimeMs := e.UnixNano() / milliSecondsToNanoSecondsFactor
	return json.Marshal(unixTimeMs)
}

func (e *MilliSecondsEpochTime) UnmarshalJSON(b []byte) error {
	var epoch int64
	err := json.Unmarshal(b, &epoch)
	if err != nil {
		return err
	}
	*e = MilliSecondsEpochTime{time.Unix(epoch/1000, (epoch%1000)*1000000)}
	return nil
}
