/*
 *
 * Copyright 2021 gRPC authors.
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

// Package rbac implements the Envoy RBAC HTTP filter.
package rbac

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/rbac"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	rpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
)

func init() {
	httpfilter.Register(builder{})
}

type builder struct {
}

type config struct {
	httpfilter.FilterConfig
	chainEngine *rbac.ChainEngine
}

func (builder) TypeURLs() []string {
	return []string{
		"type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBAC",
		"type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBACPerRoute",
	}
}

// Parsing is the same for the base config and the override config.
func parseConfig(rbacCfg *rpb.RBAC) (httpfilter.FilterConfig, error) {
	// All the validation logic described in A41.
	for _, policy := range rbacCfg.GetRules().GetPolicies() {
		// "Policy.condition and Policy.checked_condition must cause a
		// validation failure if present." - A41
		if policy.Condition != nil {
			return nil, errors.New("rbac: Policy.condition is present")
		}
		if policy.CheckedCondition != nil {
			return nil, errors.New("rbac: policy.CheckedCondition is present")
		}

		// "It is also a validation failure if Permission or Principal has a
		// header matcher for a grpc- prefixed header name or :scheme." - A41.
		//
		// "Envoy aliases :authority and Host in its header map implementation,
		// so they should be treated equivalent for the RBAC matchers; there must
		// be no behavior change depending on which of the two header names is
		// used in the RBAC policy." - A41. Any header matcher with value "host"
		// is rewritten to ":authority", as that is what grpc-go shifts both
		// headers to in the transport layer.
		//
		// Both rules apply to header matchers nested inside and/or/not rules, so
		// the whole permission and principal trees are walked.
		for _, principal := range policy.GetPrincipals() {
			if err := normalizePrincipalHeaders(principal); err != nil {
				return nil, err
			}
		}
		for _, permission := range policy.GetPermissions() {
			if err := normalizePermissionHeaders(permission); err != nil {
				return nil, err
			}
		}
	}

	// Two cases where this HTTP Filter is a no op:
	// "If absent, no enforcing RBAC policy will be applied" - RBAC
	// Documentation for Rules field.
	// "At this time, if the RBAC.action is Action.LOG then the policy will be
	// completely ignored, as if RBAC was not configured." - A41
	if rbacCfg.Rules == nil || rbacCfg.GetRules().GetAction() == v3rbacpb.RBAC_LOG {
		return config{}, nil
	}

	// TODO(gregorycooke) - change the call chain to here so we have the filter
	// name to input here instead of an empty string. It will come from here:
	// https://github.com/grpc/grpc-go/blob/eff0942e95d93112921414aee758e619ec86f26f/xds/internal/xdsclient/xdsresource/unmarshal_lds.go#L199
	ce, err := rbac.NewChainEngine([]*v3rbacpb.RBAC{rbacCfg.GetRules()}, "")
	if err != nil {
		// "At this time, if the RBAC.action is Action.LOG then the policy will be
		// completely ignored, as if RBAC was not configured." - A41
		if rbacCfg.GetRules().GetAction() != v3rbacpb.RBAC_LOG {
			return nil, fmt.Errorf("rbac: error constructing matching engine: %v", err)
		}
	}

	return config{chainEngine: ce}, nil
}

// normalizePermissionHeaders applies the A41 header-name rules to every header
// matcher reachable from permission, including those nested inside and/or/not
// rules.
func normalizePermissionHeaders(permission *v3rbacpb.Permission) error {
	switch p := permission.GetRule().(type) {
	case *v3rbacpb.Permission_Header:
		return normalizeHeaderMatcher(p.Header)
	case *v3rbacpb.Permission_AndRules:
		for _, rule := range p.AndRules.GetRules() {
			if err := normalizePermissionHeaders(rule); err != nil {
				return err
			}
		}
	case *v3rbacpb.Permission_OrRules:
		for _, rule := range p.OrRules.GetRules() {
			if err := normalizePermissionHeaders(rule); err != nil {
				return err
			}
		}
	case *v3rbacpb.Permission_NotRule:
		return normalizePermissionHeaders(p.NotRule)
	}
	return nil
}

// normalizePrincipalHeaders applies the A41 header-name rules to every header
// matcher reachable from principal, including those nested inside and/or/not
// ids.
func normalizePrincipalHeaders(principal *v3rbacpb.Principal) error {
	switch p := principal.GetIdentifier().(type) {
	case *v3rbacpb.Principal_Header:
		return normalizeHeaderMatcher(p.Header)
	case *v3rbacpb.Principal_AndIds:
		for _, id := range p.AndIds.GetIds() {
			if err := normalizePrincipalHeaders(id); err != nil {
				return err
			}
		}
	case *v3rbacpb.Principal_OrIds:
		for _, id := range p.OrIds.GetIds() {
			if err := normalizePrincipalHeaders(id); err != nil {
				return err
			}
		}
	case *v3rbacpb.Principal_NotId:
		return normalizePrincipalHeaders(p.NotId)
	}
	return nil
}

// normalizeHeaderMatcher lowercases the name of a header matcher, rejects the
// names that A41 forbids (:scheme or a grpc- prefixed name) and rewrites a
// "host" matcher to ":authority".
func normalizeHeaderMatcher(header *v3routepb.HeaderMatcher) error {
	// The keys of the metadata the matchers run against are always lowercase,
	// so a name that contains an uppercase character matches no header at all
	// and the rule using it never fires. Lowercase the name, as Envoy and
	// grpc-java do, both to make it match and to keep the checks below from
	// being evaded by the case of the name.
	name := header.GetName()
	lowerName := strings.ToLower(name)
	if lowerName != name {
		header.Name = lowerName
	}
	if lowerName == ":scheme" {
		return fmt.Errorf("rbac: header matcher for %q is %q", name, ":scheme")
	}
	if strings.HasPrefix(lowerName, "grpc-") {
		return fmt.Errorf("rbac: header matcher for %q starts with %q", name, "grpc-")
	}
	if lowerName == "host" {
		header.Name = ":authority"
	}
	return nil
}

func (builder) ParseFilterConfig(cfg proto.Message) (httpfilter.FilterConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("rbac: nil configuration message provided")
	}
	m, ok := cfg.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("rbac: error parsing config %v: unknown type %T", cfg, cfg)
	}
	msg := new(rpb.RBAC)
	if err := m.UnmarshalTo(msg); err != nil {
		return nil, fmt.Errorf("rbac: error parsing config %v: %v", cfg, err)
	}
	return parseConfig(msg)
}

func (builder) ParseFilterConfigOverride(override proto.Message) (httpfilter.FilterConfig, error) {
	if override == nil {
		return nil, fmt.Errorf("rbac: nil configuration message provided")
	}
	m, ok := override.(*anypb.Any)
	if !ok {
		return nil, fmt.Errorf("rbac: error parsing override config %v: unknown type %T", override, override)
	}
	msg := new(rpb.RBACPerRoute)
	if err := m.UnmarshalTo(msg); err != nil {
		return nil, fmt.Errorf("rbac: error parsing override config %v: %v", override, err)
	}
	return parseConfig(msg.Rbac)
}

func (builder) IsTerminal() bool {
	return false
}

func (builder) BuildServerFilter() httpfilter.ServerFilter {
	return serverFilter{}
}

var _ httpfilter.ServerFilterBuilder = builder{}

type serverFilter struct{}

func (serverFilter) Close() {}

func (serverFilter) BuildServerInterceptor(cfg httpfilter.FilterConfig, override httpfilter.FilterConfig) (resolver.ServerInterceptor, error) {
	if cfg == nil {
		return nil, fmt.Errorf("rbac: nil config provided")
	}

	c, ok := cfg.(config)
	if !ok {
		return nil, fmt.Errorf("rbac: incorrect config type provided (%T): %v", cfg, cfg)
	}

	if override != nil {
		// override completely replaces the listener configuration; but we
		// still validate the listener config type.
		c, ok = override.(config)
		if !ok {
			return nil, fmt.Errorf("rbac: incorrect override config type provided (%T): %v", override, override)
		}
	}

	// RBAC HTTP Filter is a no op from one of these two cases:
	// "If absent, no enforcing RBAC policy will be applied" - RBAC
	// Documentation for Rules field.
	// "At this time, if the RBAC.action is Action.LOG then the policy will be
	// completely ignored, as if RBAC was not configured." - A41
	if c.chainEngine == nil {
		return nil, nil
	}
	return &interceptor{chainEngine: c.chainEngine}, nil
}

type interceptor struct {
	chainEngine *rbac.ChainEngine
}

func (i *interceptor) AllowRPC(ctx context.Context) error {
	return i.chainEngine.IsAuthorized(ctx)
}

func (i *interceptor) Close() {}
