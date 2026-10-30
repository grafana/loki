// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package internal

import spb "cloud.google.com/go/bigtable/apiv2/bigtablepb"

// reqMsgType / respMsgType label the oneof payload variants of
// SessionRequest / SessionResponse. They're used as indices into the fixed
// per-session atomic-counter arrays so the breakdown stays lock-free.
//
// The "other" bucket catches frames whose oneof is unset or carries a
// payload type we haven't enumerated here (forward-compat with new server
// frames). Add new values BEFORE numReq/Resp so the totals stay aligned
// with the array size.
type reqMsgType int

const (
	reqMsgOpenSession reqMsgType = iota
	reqMsgVirtualRPC
	reqMsgCloseSession
	reqMsgOther
	numReqMsgTypes
)

func (t reqMsgType) String() string {
	switch t {
	case reqMsgOpenSession:
		return "OpenSessionRequest"
	case reqMsgVirtualRPC:
		return "VirtualRpc"
	case reqMsgCloseSession:
		return "CloseSession"
	default:
		return "other"
	}
}

func classifyReq(req *spb.SessionRequest) reqMsgType {
	if req == nil {
		return reqMsgOther
	}
	switch req.GetPayload().(type) {
	case *spb.SessionRequest_OpenSession:
		return reqMsgOpenSession
	case *spb.SessionRequest_VirtualRpc:
		return reqMsgVirtualRPC
	case *spb.SessionRequest_CloseSession:
		return reqMsgCloseSession
	default:
		return reqMsgOther
	}
}

type respMsgType int

const (
	respMsgOpenSession respMsgType = iota
	respMsgVirtualRPC
	respMsgError
	respMsgSessionParameters
	respMsgHeartbeat
	respMsgGoAway
	respMsgSessionRefreshConfig
	respMsgOther
	numRespMsgTypes
)

func (t respMsgType) String() string {
	switch t {
	case respMsgOpenSession:
		return "OpenSessionResponse"
	case respMsgVirtualRPC:
		return "VirtualRpc"
	case respMsgError:
		return "Error"
	case respMsgSessionParameters:
		return "SessionParameters"
	case respMsgHeartbeat:
		return "Heartbeat"
	case respMsgGoAway:
		return "GoAway"
	case respMsgSessionRefreshConfig:
		return "SessionRefreshConfig"
	default:
		return "other"
	}
}

func classifyResp(resp *spb.SessionResponse) respMsgType {
	if resp == nil {
		return respMsgOther
	}
	switch resp.GetPayload().(type) {
	case *spb.SessionResponse_OpenSession:
		return respMsgOpenSession
	case *spb.SessionResponse_VirtualRpc:
		return respMsgVirtualRPC
	case *spb.SessionResponse_Error:
		return respMsgError
	case *spb.SessionResponse_SessionParameters:
		return respMsgSessionParameters
	case *spb.SessionResponse_Heartbeat:
		return respMsgHeartbeat
	case *spb.SessionResponse_GoAway:
		return respMsgGoAway
	case *spb.SessionResponse_SessionRefreshConfig:
		return respMsgSessionRefreshConfig
	default:
		return respMsgOther
	}
}
