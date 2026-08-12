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
	"fmt"
	"regexp"

	"google.golang.org/grpc/internal/xds/matcher"

	v3mutationpb "github.com/envoyproxy/go-control-plane/envoy/config/common/mutation_rules/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
)

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
