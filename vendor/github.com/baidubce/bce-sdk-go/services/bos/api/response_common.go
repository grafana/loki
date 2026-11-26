/*
 * Copyright 2026 Baidu, Inc.
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

// response_common.go - definitions of the per-request response metadata of BOS service

package api

// ResponseCommon carries the per-request information returned by the BOS server.
// It is populated on both success and failure, as long as an HTTP response has been
// received. All the fields are excluded from the JSON (de)serialization, so embedding
// this struct never changes the wire format of any request or response payload.
type ResponseCommon struct {
	RequestId  string `json:"-"`
	DebugId    string `json:"-"`
	StatusCode int    `json:"-"`
}

// setResponseCommon copies the metadata of the given response into m. It is a no-op
// when m is nil or when no http response has been received, so that the value already
// held by the caller is never overwritten with empty values.
func (m *ResponseCommon) setResponseCommon(resp *BosResponse) {
	if m == nil || !responseMetadataReady(resp) {
		return
	}
	m.RequestId = resp.RequestId()
	m.DebugId = resp.DebugId()
	m.StatusCode = resp.StatusCode()
}

// responseMetadataReady reports whether the given response has completed a round trip.
// BceResponse.statusCode stays 0 until ParseResponse() runs, which happens only after
// SetHttpResponse(). Note that resp.IsFail() must NOT be used here: it dereferences the
// inner http response which is still nil before SetHttpResponse().
func responseMetadataReady(resp *BosResponse) bool {
	return resp != nil && resp.StatusCode() != 0
}

// fillResponseCommon writes the metadata of the response into the struct supplied by
// the caller, if any. The request scoped sink takes precedence over the context scoped
// one, which only serves the api functions that are unable to accept options.
func fillResponseCommon(req *BosRequest, ctx *BosContext, resp *BosResponse) {
	if !responseMetadataReady(resp) {
		return
	}
	if req != nil && req.ResponseCommon != nil {
		req.ResponseCommon.setResponseCommon(resp)
		return
	}
	if ctx != nil {
		ctx.ResponseCommon.setResponseCommon(resp)
	}
}

// metadataSetter is satisfied by every result struct embedding ResponseCommon by
// value, through the method promotion on the addressable field.
type metadataSetter interface {
	setResponseCommon(resp *BosResponse)
}

// retrieveResponseFields populates the ResponseCommon embedded in the given result. Results
// which do not embed it are silently ignored.
func retrieveResponseFields(result interface{}, resp *BosResponse) {
	if setter, ok := result.(metadataSetter); ok {
		setter.setResponseCommon(resp)
	}
}

// setResponseCommon satisfies metadataSetter for CopyObjectResult, which cannot embed
// ResponseCommon because its own RequestId field would shadow the promoted one. The
// callers invoke it right after the allocation, so the RequestId decoded from the body
// and the one read from the response header still take precedence, as they always did.
func (r *CopyObjectResult) setResponseCommon(resp *BosResponse) {
	if r == nil || !responseMetadataReady(resp) {
		return
	}
	r.RequestId = resp.RequestId()
	r.DebugId = resp.DebugId()
	r.StatusCode = resp.StatusCode()
}

// setResponseCommon satisfies metadataSetter for FetchObjectResult, whose RequestId is
// part of the response body. Same shadowing reason and same ordering guarantee as above.
func (r *FetchObjectResult) setResponseCommon(resp *BosResponse) {
	if r == nil || !responseMetadataReady(resp) {
		return
	}
	r.RequestId = resp.RequestId()
	r.DebugId = resp.DebugId()
	r.StatusCode = resp.StatusCode()
}
