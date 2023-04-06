/*
 * Copyright 2018-2020 The NATS Authors
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
	"errors"
	"strings"

	"github.com/nats-io/nkeys"
)

// GenericClaims can be used to read a JWT as a map for any non-generic fields
type GenericClaims struct {
	ClaimsData
	Data map[string]interface{} `json:"nats,omitempty"`
}

// NewGenericClaims creates a map-based Claims
func NewGenericClaims(subject string) *GenericClaims {
	if subject == "" {
		return nil
	}
	c := GenericClaims{}
	c.Subject = subject
	c.Data = make(map[string]interface{})
	return &c
}

// DecodeGeneric takes a JWT string and decodes it into a ClaimsData and map
func DecodeGeneric(token string) (*GenericClaims, error) {
	// must have 3 chunks
	chunks := strings.Split(token, ".")
	if len(chunks) != 3 {
		return nil, errors.New("expected 3 chunks")
	}

	// header
	header, err := parseHeaders(chunks[0])
	if err != nil {
		return nil, err
	}
	// claim
	data, err := decodeString(chunks[1])
	if err != nil {
		return nil, err
	}

	gc := struct {
		GenericClaims
		GenericFields
	}{}
	if err := json.Unmarshal(data, &gc); err != nil {
		return nil, err
	}

	// sig
	sig, err := decodeString(chunks[2])
	if err != nil {
		return nil, err
	}

	if header.Algorithm == AlgorithmNkeyOld {
		if !gc.verify(chunks[1], sig) {
			return nil, errors.New("claim failed V1 signature verification")
		}
		if tp := gc.GenericFields.Type; tp != "" {
			// the conversion needs to be from a string because
			// on custom types the type is not going to be one of
			// the constants
			gc.GenericClaims.Data["type"] = string(tp)
		}
		if tp := gc.GenericFields.Tags; len(tp) != 0 {
			gc.GenericClaims.Data["tags"] = tp
		}

	} else {
		if !gc.verify(token[:len(chunks[0])+len(chunks[1])+1], sig) {
			return nil, errors.New("claim failed V2 signature verification")
		}
	}
	return &gc.GenericClaims, nil
}

// Claims returns the standard part of the generic claim
func (gc *GenericClaims) Claims() *ClaimsData {
	return &gc.ClaimsData
}

// Payload returns the custom part of the claims data
func (gc *GenericClaims) Payload() interface{} {
	return &gc.Data
}

// Encode takes a generic claims and creates a JWT string
func (gc *GenericClaims) Encode(pair nkeys.KeyPair) (string, error) {
	return gc.ClaimsData.encode(pair, gc)
}

// Validate checks the generic part of the claims data
func (gc *GenericClaims) Validate(vr *ValidationResults) {
	gc.ClaimsData.Validate(vr)
}

func (gc *GenericClaims) String() string {
	return gc.ClaimsData.String(gc)
}

// ExpectedPrefixes returns the types allowed to encode a generic JWT, which is nil for all
func (gc *GenericClaims) ExpectedPrefixes() []nkeys.PrefixByte {
	return nil
}

func (gc *GenericClaims) ClaimType() ClaimType {
	v, ok := gc.Data["type"]
	if !ok {
		v, ok = gc.Data["nats"]
		if ok {
			m, ok := v.(map[string]interface{})
			if ok {
				v = m["type"]
			}
		}
	}

	switch ct := v.(type) {
	case string:
		if IsGenericClaimType(ct) {
			return GenericClaim
		}
		return ClaimType(ct)
	case ClaimType:
		return ct
	default:
		return ""
	}
}

func (gc *GenericClaims) updateVersion() {
	if gc.Data != nil {
		// store as float as that is what decoding with json does too
		gc.Data["version"] = float64(libVersion)
	}
}
