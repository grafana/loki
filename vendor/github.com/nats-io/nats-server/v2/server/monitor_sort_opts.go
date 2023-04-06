// Copyright 2013-2018 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"time"
)

// ConnInfos represents a connection info list. We use pointers since it will be sorted.
type ConnInfos []*ConnInfo

// For sorting
// Len returns length for sorting.
func (cl ConnInfos) Len() int { return len(cl) }

// Swap will sawap the elements.
func (cl ConnInfos) Swap(i, j int) { cl[i], cl[j] = cl[j], cl[i] }

// SortOpt is a helper type to sort clients
type SortOpt string

// Possible sort options
const (
	ByCid      SortOpt = "cid"        // By connection ID
	ByStart    SortOpt = "start"      // By connection start time, same as CID
	BySubs     SortOpt = "subs"       // By number of subscriptions
	ByPending  SortOpt = "pending"    // By amount of data in bytes waiting to be sent to client
	ByOutMsgs  SortOpt = "msgs_to"    // By number of messages sent
	ByInMsgs   SortOpt = "msgs_from"  // By number of messages received
	ByOutBytes SortOpt = "bytes_to"   // By amount of bytes sent
	ByInBytes  SortOpt = "bytes_from" // By amount of bytes received
	ByLast     SortOpt = "last"       // By the last activity
	ByIdle     SortOpt = "idle"       // By the amount of inactivity
	ByUptime   SortOpt = "uptime"     // By the amount of time connections exist
	ByStop     SortOpt = "stop"       // By the stop time for a closed connection
	ByReason   SortOpt = "reason"     // By the reason for a closed connection

)

// Individual sort options provide the Less for sort.Interface. Len and Swap are on cList.
// CID
type byCid struct{ ConnInfos }

func (l byCid) Less(i, j int) bool { return l.ConnInfos[i].Cid < l.ConnInfos[j].Cid }

// Number of Subscriptions
type bySubs struct{ ConnInfos }

func (l bySubs) Less(i, j int) bool { return l.ConnInfos[i].NumSubs < l.ConnInfos[j].NumSubs }

// Pending Bytes
type byPending struct{ ConnInfos }

func (l byPending) Less(i, j int) bool { return l.ConnInfos[i].Pending < l.ConnInfos[j].Pending }

// Outbound Msgs
type byOutMsgs struct{ ConnInfos }

func (l byOutMsgs) Less(i, j int) bool { return l.ConnInfos[i].OutMsgs < l.ConnInfos[j].OutMsgs }

// Inbound Msgs
type byInMsgs struct{ ConnInfos }

func (l byInMsgs) Less(i, j int) bool { return l.ConnInfos[i].InMsgs < l.ConnInfos[j].InMsgs }

// Outbound Bytes
type byOutBytes struct{ ConnInfos }

func (l byOutBytes) Less(i, j int) bool { return l.ConnInfos[i].OutBytes < l.ConnInfos[j].OutBytes }

// Inbound Bytes
type byInBytes struct{ ConnInfos }

func (l byInBytes) Less(i, j int) bool { return l.ConnInfos[i].InBytes < l.ConnInfos[j].InBytes }

// Last Activity
type byLast struct{ ConnInfos }

func (l byLast) Less(i, j int) bool {
	return l.ConnInfos[i].LastActivity.UnixNano() < l.ConnInfos[j].LastActivity.UnixNano()
}

// Idle time
type byIdle struct{ ConnInfos }

func (l byIdle) Less(i, j int) bool {
	ii := l.ConnInfos[i].LastActivity.Sub(l.ConnInfos[i].Start)
	ij := l.ConnInfos[j].LastActivity.Sub(l.ConnInfos[j].Start)
	return ii < ij
}

// Uptime
type byUptime struct {
	ConnInfos
	now time.Time
}

func (l byUptime) Less(i, j int) bool {
	ci := l.ConnInfos[i]
	cj := l.ConnInfos[j]
	var upi, upj time.Duration
	if ci.Stop == nil || ci.Stop.IsZero() {
		upi = l.now.Sub(ci.Start)
	} else {
		upi = ci.Stop.Sub(ci.Start)
	}
	if cj.Stop == nil || cj.Stop.IsZero() {
		upj = l.now.Sub(cj.Start)
	} else {
		upj = cj.Stop.Sub(cj.Start)
	}
	return upi < upj
}

// Stop
type byStop struct{ ConnInfos }

func (l byStop) Less(i, j int) bool {
	ciStop := l.ConnInfos[i].Stop
	cjStop := l.ConnInfos[j].Stop
	return ciStop.Before(*cjStop)
}

// Reason
type byReason struct{ ConnInfos }

func (l byReason) Less(i, j int) bool {
	return l.ConnInfos[i].Reason < l.ConnInfos[j].Reason
}

// IsValid determines if a sort option is valid
func (s SortOpt) IsValid() bool {
	switch s {
	case "", ByCid, ByStart, BySubs, ByPending, ByOutMsgs, ByInMsgs, ByOutBytes, ByInBytes, ByLast, ByIdle, ByUptime, ByStop, ByReason:
		return true
	default:
		return false
	}
}
