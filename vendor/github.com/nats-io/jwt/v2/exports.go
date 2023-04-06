/*
 * Copyright 2018-2019 The NATS Authors
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package jwt

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ResponseType is used to store an export response type
type ResponseType string

const (
	// ResponseTypeSingleton is used for a service that sends a single response only
	ResponseTypeSingleton = "Singleton"

	// ResponseTypeStream is used for a service that will send multiple responses
	ResponseTypeStream = "Stream"

	// ResponseTypeChunked is used for a service that sends a single response in chunks (so not quite a stream)
	ResponseTypeChunked = "Chunked"
)

// ServiceLatency is used when observing and exported service for
// latency measurements.
// Sampling 1-100, represents sampling rate, defaults to 100.
// Results is the subject where the latency metrics are published.
// A metric will be defined by the nats-server's ServiceLatency. Time durations
// are in nanoseconds.
// see https://github.com/nats-io/nats-server/blob/main/server/accounts.go#L524
// e.g.
// {
//  "app": "dlc22",
//  "start": "2019-09-16T21:46:23.636869585-07:00",
//  "svc": 219732,
//  "nats": {
//    "req": 320415,
//    "resp": 228268,
//    "sys": 0
//  },
//  "total": 768415
// }
//
type ServiceLatency struct {
	Sampling SamplingRate `json:"sampling"`
	Results  Subject      `json:"results"`
}

type SamplingRate int

const Headers = SamplingRate(0)

// MarshalJSON marshals the field as "headers" or percentages
func (r *SamplingRate) MarshalJSON() ([]byte, error) {
	sr := *r
	if sr == 0 {
		return []byte(`"headers"`), nil
	}
	if sr >= 1 && sr <= 100 {
		return []byte(fmt.Sprintf("%d", sr)), nil
	}
	return nil, fmt.Errorf("unknown sampling rate")
}

// UnmarshalJSON unmashals numbers as percentages or "headers"
func (t *SamplingRate) UnmarshalJSON(b []byte) error {
	if len(b) == 0 {
		return fmt.Errorf("empty sampling rate")
	}
	if strings.ToLower(string(b)) == `"headers"` {
		*t = Headers
		return nil
	}
	var j int
	err := json.Unmarshal(b, &j)
	if err != nil {
		return err
	}
	*t = SamplingRate(j)
	return nil
}

func (sl *ServiceLatency) Validate(vr *ValidationResults) {
	if sl.Sampling != 0 {
		if sl.Sampling < 1 || sl.Sampling > 100 {
			vr.AddError("sampling percentage needs to be between 1-100")
		}
	}
	sl.Results.Validate(vr)
	if sl.Results.HasWildCards() {
		vr.AddError("results subject can not contain wildcards")
	}
}

// Export represents a single export
type Export struct {
	Name                 string          `json:"name,omitempty"`
	Subject              Subject         `json:"subject,omitempty"`
	Type                 ExportType      `json:"type,omitempty"`
	TokenReq             bool            `json:"token_req,omitempty"`
	Revocations          RevocationList  `json:"revocations,omitempty"`
	ResponseType         ResponseType    `json:"response_type,omitempty"`
	ResponseThreshold    time.Duration   `json:"response_threshold,omitempty"`
	Latency              *ServiceLatency `json:"service_latency,omitempty"`
	AccountTokenPosition uint            `json:"account_token_position,omitempty"`
	Advertise            bool            `json:"advertise,omitempty"`
	Info
}

// IsService returns true if an export is for a service
func (e *Export) IsService() bool {
	return e.Type == Service
}

// IsStream returns true if an export is for a stream
func (e *Export) IsStream() bool {
	return e.Type == Stream
}

// IsSingleResponse returns true if an export has a single response
// or no response type is set, also checks that the type is service
func (e *Export) IsSingleResponse() bool {
	return e.Type == Service && (e.ResponseType == ResponseTypeSingleton || e.ResponseType == "")
}

// IsChunkedResponse returns true if an export has a chunked response
func (e *Export) IsChunkedResponse() bool {
	return e.Type == Service && e.ResponseType == ResponseTypeChunked
}

// IsStreamResponse returns true if an export has a chunked response
func (e *Export) IsStreamResponse() bool {
	return e.Type == Service && e.ResponseType == ResponseTypeStream
}

// Validate appends validation issues to the passed in results list
func (e *Export) Validate(vr *ValidationResults) {
	if e == nil {
		vr.AddError("null export is not allowed")
		return
	}
	if !e.IsService() && !e.IsStream() {
		vr.AddError("invalid export type: %q", e.Type)
	}
	if e.IsService() && !e.IsSingleResponse() && !e.IsChunkedResponse() && !e.IsStreamResponse() {
		vr.AddError("invalid response type for service: %q", e.ResponseType)
	}
	if e.IsStream() && e.ResponseType != "" {
		vr.AddError("invalid response type for stream: %q", e.ResponseType)
	}
	if e.Latency != nil {
		if !e.IsService() {
			vr.AddError("latency tracking only permitted for services")
		}
		e.Latency.Validate(vr)
	}
	if e.ResponseThreshold.Nanoseconds() < 0 {
		vr.AddError("negative response threshold is invalid")
	}
	if e.ResponseThreshold.Nanoseconds() > 0 && !e.IsService() {
		vr.AddError("response threshold only valid for services")
	}
	e.Subject.Validate(vr)
	if e.AccountTokenPosition > 0 {
		if !e.Subject.HasWildCards() {
			vr.AddError("Account Token Position can only be used with wildcard subjects: %s", e.Subject)
		} else {
			subj := string(e.Subject)
			token := strings.Split(subj, ".")
			tkCnt := uint(len(token))
			if e.AccountTokenPosition > tkCnt {
				vr.AddError("Account Token Position %d exceeds length of subject '%s'",
					e.AccountTokenPosition, e.Subject)
			} else if tk := token[e.AccountTokenPosition-1]; tk != "*" {
				vr.AddError("Account Token Position %d matches '%s' but must match a * in: %s",
					e.AccountTokenPosition, tk, e.Subject)
			}
		}
	}
	e.Info.Validate(vr)
}

// Revoke enters a revocation by publickey using time.Now().
func (e *Export) Revoke(pubKey string) {
	e.RevokeAt(pubKey, time.Now())
}

// RevokeAt enters a revocation by publickey and timestamp into this export
// If there is already a revocation for this public key that is newer, it is kept.
func (e *Export) RevokeAt(pubKey string, timestamp time.Time) {
	if e.Revocations == nil {
		e.Revocations = RevocationList{}
	}

	e.Revocations.Revoke(pubKey, timestamp)
}

// ClearRevocation removes any revocation for the public key
func (e *Export) ClearRevocation(pubKey string) {
	e.Revocations.ClearRevocation(pubKey)
}

// isRevoked checks if the public key is in the revoked list with a timestamp later than the one passed in.
// Generally this method is called with the subject and issue time of the jwt to be tested.
// DO NOT pass time.Now(), it will not produce a stable/expected response.
func (e *Export) isRevoked(pubKey string, claimIssuedAt time.Time) bool {
	return e.Revocations.IsRevoked(pubKey, claimIssuedAt)
}

// IsClaimRevoked checks if the activation revoked the claim passed in.
// Invalid claims (nil, no Subject or IssuedAt) will return true.
func (e *Export) IsClaimRevoked(claim *ActivationClaims) bool {
	if claim == nil || claim.IssuedAt == 0 || claim.Subject == "" {
		return true
	}
	return e.isRevoked(claim.Subject, time.Unix(claim.IssuedAt, 0))
}

// Exports is a slice of exports
type Exports []*Export

// Add appends exports to the list
func (e *Exports) Add(i ...*Export) {
	*e = append(*e, i...)
}

func isContainedIn(kind ExportType, subjects []Subject, vr *ValidationResults) {
	m := make(map[string]string)
	for i, ns := range subjects {
		for j, s := range subjects {
			if i == j {
				continue
			}
			if ns.IsContainedIn(s) {
				str := string(s)
				_, ok := m[str]
				if !ok {
					m[str] = string(ns)
				}
			}
		}
	}

	if len(m) != 0 {
		for k, v := range m {
			var vi ValidationIssue
			vi.Blocking = true
			vi.Description = fmt.Sprintf("%s export subject %q already exports %q", kind, k, v)
			vr.Add(&vi)
		}
	}
}

// Validate calls validate on all of the exports
func (e *Exports) Validate(vr *ValidationResults) error {
	var serviceSubjects []Subject
	var streamSubjects []Subject

	for _, v := range *e {
		if v == nil {
			vr.AddError("null export is not allowed")
			continue
		}
		if v.IsService() {
			serviceSubjects = append(serviceSubjects, v.Subject)
		} else {
			streamSubjects = append(streamSubjects, v.Subject)
		}
		v.Validate(vr)
	}

	isContainedIn(Service, serviceSubjects, vr)
	isContainedIn(Stream, streamSubjects, vr)

	return nil
}

// HasExportContainingSubject checks if the export list has an export with the provided subject
func (e *Exports) HasExportContainingSubject(subject Subject) bool {
	for _, s := range *e {
		if subject.IsContainedIn(s.Subject) {
			return true
		}
	}
	return false
}

func (e Exports) Len() int {
	return len(e)
}

func (e Exports) Swap(i, j int) {
	e[i], e[j] = e[j], e[i]
}

func (e Exports) Less(i, j int) bool {
	return e[i].Subject < e[j].Subject
}
