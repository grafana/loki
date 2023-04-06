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
)

const (
	// Version is semantic version.
	Version = "2.3.0"

	// TokenTypeJwt is the JWT token type supported JWT tokens
	// encoded and decoded by this library
	// from RFC7519 5.1 "typ":
	// it is RECOMMENDED that "JWT" always be spelled using uppercase characters for compatibility
	TokenTypeJwt = "JWT"

	// AlgorithmNkey is the algorithm supported by JWT tokens
	// encoded and decoded by this library
	AlgorithmNkeyOld = "ed25519"
	AlgorithmNkey    = AlgorithmNkeyOld + "-nkey"
)

// Header is a JWT Jose Header
type Header struct {
	Type      string `json:"typ"`
	Algorithm string `json:"alg"`
}

// Parses a header JWT token
func parseHeaders(s string) (*Header, error) {
	h, err := decodeString(s)
	if err != nil {
		return nil, err
	}
	header := Header{}
	if err := json.Unmarshal(h, &header); err != nil {
		return nil, err
	}

	if err := header.Valid(); err != nil {
		return nil, err
	}
	return &header, nil
}

// Valid validates the Header. It returns nil if the Header is
// a JWT header, and the algorithm used is the NKEY algorithm.
func (h *Header) Valid() error {
	if TokenTypeJwt != strings.ToUpper(h.Type) {
		return fmt.Errorf("not supported type %q", h.Type)
	}

	alg := strings.ToLower(h.Algorithm)
	if !strings.HasPrefix(alg, AlgorithmNkeyOld) {
		return fmt.Errorf("unexpected %q algorithm", h.Algorithm)
	}
	if AlgorithmNkeyOld != alg && AlgorithmNkey != alg {
		return fmt.Errorf("unexpected %q algorithm", h.Algorithm)
	}
	return nil
}
