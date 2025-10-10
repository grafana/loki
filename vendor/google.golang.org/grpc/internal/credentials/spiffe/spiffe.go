/*
 *
 * Copyright 2025 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

// Package spiffe defines APIs for working with SPIFFE Bundle Maps.
//
// All APIs in this package are experimental.
package spiffe

import (
	"encoding/json"
	"fmt"

	"github.com/spiffe/go-spiffe/v2/bundle/spiffebundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
)

type partialParsedSPIFFEBundleMap struct {
	Bundles map[string]json.RawMessage `json:"trust_domains"`
}

// BundleMapFromBytes parses bytes into a SPIFFE Bundle Map. See the
// SPIFFE Bundle Map spec for more detail -
// https://github.com/spiffe/spiffe/blob/main/standards/SPIFFE_Trust_Domain_and_Bundle.md#4-spiffe-bundle-format
// If duplicate keys are encountered in the JSON parsing, Go's default unmarshal
// behavior occurs which causes the last processed entry to be the entry in the
// parsed map.
func BundleMapFromBytes(bundleMapBytes []byte) (map[string]*spiffebundle.Bundle, error) {
	var result partialParsedSPIFFEBundleMap
	if err := json.Unmarshal(bundleMapBytes, &result); err != nil {
		return nil, err
	}
	if result.Bundles == nil {
		return nil, fmt.Errorf("spiffe: BundleMapFromBytes() no bundles parsed from spiffe bundle map bytes")
	}
	bundleMap := map[string]*spiffebundle.Bundle{}
	for td, jsonBundle := range result.Bundles {
		trustDomain, err := spiffeid.TrustDomainFromString(td)
		if err != nil {
			return nil, fmt.Errorf("spiffe: BundleMapFromBytes() invalid trust domain %q found when parsing SPIFFE Bundle Map: %v", td, err)
		}
		bundle, err := spiffebundle.Parse(trustDomain, jsonBundle)
		if err != nil {
			return nil, fmt.Errorf("spiffe: BundleMapFromBytes() failed to parse bundle for trust domain %q: %v", td, err)
		}
		bundleMap[td] = bundle
	}
	return bundleMap, nil
}
