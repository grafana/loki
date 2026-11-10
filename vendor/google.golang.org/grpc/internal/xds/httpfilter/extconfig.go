/*
 *
 * Copyright 2026 gRPC authors.
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

package httpfilter

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	imetadata "google.golang.org/grpc/internal/metadata"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/metadata"

	v3mutationpb "github.com/envoyproxy/go-control-plane/envoy/config/common/mutation_rules/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
)

// maxHeaderSize is the maximum length, in bytes, of a header key or value in a
// mutation received from an external processing server.
const maxHeaderSize = 16384

// HeaderMutationRules specifies the rules for what modifications an external
// processing server may make to headers sent on the data plane RPC.
type HeaderMutationRules struct {
	// AllowExpr specifies a regular expression that matches the headers that can
	// be mutated.
	AllowExpr *regexp.Regexp
	// DisallowExpr specifies a regular expression that matches the headers that
	// cannot be mutated. This overrides the above allowExpr if a header matches
	// both.
	DisallowExpr *regexp.Regexp
	// DisallowAll specifies that no header mutations are allowed. This overrides
	// all other settings.
	DisallowAll bool
	// DisallowIsError specifies whether to return an error if a header mutation
	// is disallowed. If true, the data plane RPC will be failed with a grpc
	// status code of Unknown.
	DisallowIsError bool
}

// ConvertStringMatchers converts a slice of protobuf StringMatcher messages to
// a slice of matcher.StringMatcher.
func ConvertStringMatchers(patterns []*v3matcherpb.StringMatcher) ([]matcher.StringMatcher, error) {
	matchers := make([]matcher.StringMatcher, 0, len(patterns))
	for _, p := range patterns {
		sm, err := matcher.StringMatcherFromProto(p)
		if err != nil {
			return nil, err
		}
		matchers = append(matchers, sm)
	}
	return matchers, nil
}

// HeaderMutationRulesFromProto converts a protobuf HeaderMutationRules proto
// message to a HeaderMutationRules struct.
func HeaderMutationRulesFromProto(mr *v3mutationpb.HeaderMutationRules) (HeaderMutationRules, error) {
	var rules HeaderMutationRules
	if mr == nil {
		return rules, nil
	}
	if allowExpr := mr.GetAllowExpression(); allowExpr != nil {
		re, err := matcher.CompileSafeRegex(allowExpr.GetRegex())
		if err != nil {
			return rules, fmt.Errorf("httpfilter: %v", err)
		}
		rules.AllowExpr = re
	}
	if disallowExpr := mr.GetDisallowExpression(); disallowExpr != nil {
		re, err := matcher.CompileSafeRegex(disallowExpr.GetRegex())
		if err != nil {
			return rules, fmt.Errorf("httpfilter: %v", err)
		}
		rules.DisallowExpr = re
	}
	rules.DisallowAll = mr.GetDisallowAll().GetValue()
	rules.DisallowIsError = mr.GetDisallowIsError().GetValue()
	return rules, nil
}

// ApplyAdditions takes a set of header mutations (for additions and
// modifications) received from an external server and applies them to the
// provided metadata, subject to the rules defined in hmr.
//
// If the DisallowAll field is true, no mutations are performed, and the input
// metadata is returned unmodified.
//
// It iterates through each header mutation, performs validation on the header
// key and value, and checks if the mutation is permitted by the AllowExpr and
// DisallowExpr regular expressions.
//
// A mutation for any of the following headers fails validation and an error is
// returned:
// - Pseudo-headers (keys starting with ':').
// - The 'host' header.
// - Headers with keys in the reserved 'grpc-' space.
// - Headers with non-lowercase keys.
// - Headers with keys or values exceeding 16384 bytes.
// - Headers with an invalid gRPC header name or header value.
//
// If a mutation is disallowed by the mutation rules and DisallowIsError is
// true, an error is returned. Otherwise, the disallowed mutation is silently
// ignored.
//
// The input metadata must not be nil.
func (hmr *HeaderMutationRules) ApplyAdditions(hvos []*v3corepb.HeaderValueOption, input metadata.MD) error {
	if hmr == nil {
		hmr = &HeaderMutationRules{}
	}
	if input == nil {
		return fmt.Errorf("input metadata is nil")
	}
	if hmr.DisallowAll {
		return nil
	}

	for _, hvo := range hvos {
		header := hvo.GetHeader()
		key := header.GetKey()
		if err := validateHeaderKey(key); err != nil {
			return fmt.Errorf("invalid header mutation: %v", err)
		}

		value := header.GetValue()
		if strings.HasSuffix(key, "-bin") {
			value = string(header.GetRawValue())
		}
		if len(value) > maxHeaderSize {
			return fmt.Errorf("invalid header mutation: value for header key %q exceeds the maximum length of %d bytes", key, maxHeaderSize)
		}
		// ValidatePair rejects values carrying bytes outside %x20-%x7E. It
		// skips the value check for "-bin" keys, whose values the transport
		// base64 encodes.
		if err := imetadata.ValidatePair(key, value); err != nil {
			return fmt.Errorf("invalid header mutation: %v", err)
		}

		if !hmr.allow(key) {
			if hmr.DisallowIsError {
				return fmt.Errorf("header mutation disallowed by headerMutationRules for header key %q", key)
			}
			continue
		}

		// Perform the mutation on output metadata using the append_action
		// field from the header value option.
		switch hvo.GetAppendAction() {
		case v3corepb.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD:
			input.Append(key, value)
		case v3corepb.HeaderValueOption_ADD_IF_ABSENT:
			if input.Get(key) == nil {
				input.Set(key, value)
			}
		case v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD:
			input.Set(key, value)
		case v3corepb.HeaderValueOption_OVERWRITE_IF_EXISTS:
			if input.Get(key) != nil {
				input.Set(key, value)
			}
		}
	}
	return nil
}

// ApplyRemovals takes a set of headers (for removal) received from an external
// processing server and applies them to the provided metadata, subject to the
// rules defined in hmr.
//
// This method is very similar to ApplyAdditions, except that headers are
// removed here instead of added or mutated as is the case in the latter. See
// ApplyAdditions for more details.
//
// The input metadata must not be nil.
func (hmr *HeaderMutationRules) ApplyRemovals(headersToRemove []string, input metadata.MD) error {
	if hmr == nil {
		hmr = &HeaderMutationRules{}
	}
	if input == nil {
		return fmt.Errorf("input metadata is nil")
	}
	if hmr.DisallowAll {
		return nil
	}

	for _, header := range headersToRemove {
		if err := validateHeaderKey(header); err != nil {
			return fmt.Errorf("invalid header mutation: %v", err)
		}
		if !hmr.allow(header) {
			if hmr.DisallowIsError {
				return fmt.Errorf("header mutation disallowed by headerMutationRules for header %q", header)
			}
			continue
		}
		input.Delete(header)
	}
	return nil
}

// validateHeaderKey returns a non-nil error if key may not be mutated by an
// external processing server, either because the key is reserved or because it
// is not a valid gRPC header name.
func validateHeaderKey(key string) error {
	switch {
	case len(key) == 0:
		return fmt.Errorf("header key is empty")
	case key[0] == ':':
		return fmt.Errorf("header key %q is a pseudo-header", key)
	case key == "host":
		return fmt.Errorf("header key %q is reserved", key)
	case strings.HasPrefix(key, "grpc-"):
		return fmt.Errorf("header key %q is in the reserved 'grpc-' space", key)
	case key != strings.ToLower(key):
		return fmt.Errorf("header key %q is not lowercase", key)
	case len(key) > maxHeaderSize:
		return fmt.Errorf("header key exceeds the maximum length of %d bytes", maxHeaderSize)
	}
	return imetadata.ValidateKey(key)
}

func (hmr *HeaderMutationRules) allow(key string) bool {
	if hmr.DisallowExpr != nil && hmr.DisallowExpr.MatchString(key) {
		return false
	}
	if hmr.AllowExpr != nil && hmr.AllowExpr.MatchString(key) {
		return true
	}
	if hmr.AllowExpr != nil {
		return false
	}
	return true
}

// ConstructHeaderMap constructs a HeaderMap from the given metadata and raw
// appended metadata slice, using the following rules:
//   - if the header is matched by the disallowed_headers config field, it will
//     not be added to the map, otherwise,
//   - if the allowed_headers config field is unset or matches the header, the
//     header will be added to the map, otherwise,
//   - the header will be excluded from the map.
func ConstructHeaderMap(md metadata.MD, added [][]string, allowedHeaders, disallowedHeaders []matcher.StringMatcher) *v3corepb.HeaderMap {
	headerMap := &v3corepb.HeaderMap{}
	// Process the base metadata map.
	for key, values := range md {
		if isDisallowedHeader(key, disallowedHeaders) {
			continue
		}
		if isAllowedHeader(key, allowedHeaders) {
			for _, value := range values {
				headerMap.Headers = append(headerMap.Headers, constructHeader(key, value))
			}
		}
	}
	// Process the raw appended metadata slice.
	for _, kvs := range added {
		for i := 0; i < len(kvs); i += 2 {
			key := strings.ToLower(kvs[i])
			if isDisallowedHeader(key, disallowedHeaders) {
				continue
			}
			if isAllowedHeader(key, allowedHeaders) {
				headerMap.Headers = append(headerMap.Headers, constructHeader(key, kvs[i+1]))
			}
		}
	}
	if len(headerMap.Headers) == 0 {
		return nil
	}
	return headerMap
}

func constructHeader(key, value string) *v3corepb.HeaderValue {
	rawValue := []byte(value)
	if strings.HasSuffix(key, "-bin") {
		encoded := make([]byte, base64.StdEncoding.EncodedLen(len(rawValue)))
		base64.StdEncoding.Encode(encoded, rawValue)
		rawValue = encoded
	}
	return &v3corepb.HeaderValue{
		Key:      key,
		RawValue: rawValue,
	}
}

// isDisallowedHeader returns true if the given header key matches any of the
// provided disallowed header matchers.
func isDisallowedHeader(key string, matchers []matcher.StringMatcher) bool {
	for _, m := range matchers {
		if m.Match(key) {
			return true
		}
	}
	return false
}

// isAllowedHeader returns true if the allowed header matchers list is empty, or
// if the given header key matches any of the provided allowed header matchers.
func isAllowedHeader(key string, matchers []matcher.StringMatcher) bool {
	if len(matchers) == 0 {
		return true
	}
	for _, m := range matchers {
		if m.Match(key) {
			return true
		}
	}
	return false
}
