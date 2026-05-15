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

package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/transport"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// RouteAndProcess routes the incoming RPC to a configured route in the route
// table and also processes the RPC by running the incoming RPC through any HTTP
// Filters configured.
func RouteAndProcess(ctx context.Context) error {
	conn := transport.GetConnection(ctx)
	cw, ok := conn.(*connWrapper)
	if !ok {
		return errors.New("missing virtual hosts in incoming context")
	}

	rc := cw.urc.Load()
	// Error out at routing l7 level with a status code UNAVAILABLE, represents
	// an nack before usable route configuration or resource not found for RDS
	// or error combining LDS + RDS (Shouldn't happen).
	if rc.err != nil {
		if logger.V(2) {
			logger.Infof("RPC on connection with xDS Configuration error: %v", rc.err)
		}
		return status.Error(codes.Unavailable, fmt.Sprintf("error from xDS configuration for matched route configuration: %v", rc.err))
	}

	mn, ok := grpc.Method(ctx)
	if !ok {
		return errors.New("missing method name in incoming context")
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return errors.New("missing metadata in incoming context")
	}
	// A41 added logic to the core grpc implementation to guarantee that once
	// the RPC gets to this point, there will be a single, unambiguous authority
	// present in the header map.
	authority := md.Get(":authority")
	// authority[0] is safe because of the guarantee mentioned above.
	vh := findBestMatchingVirtualHostServer(authority[0], rc.vhs)
	if vh == nil {
		return rc.statusErrWithNodeID(codes.Unavailable, "the incoming RPC did not match a configured Virtual Host")
	}

	var rwi *routeWithInterceptors
	rpcInfo := iresolver.RPCInfo{
		Context: ctx,
		Method:  mn,
	}
	for _, r := range vh.routes {
		if r.matcher.Match(rpcInfo) {
			// "NonForwardingAction is expected for all Routes used on
			// server-side; a route with an inappropriate action causes RPCs
			// matching that route to fail with UNAVAILABLE." - A36
			if r.actionType != xdsresource.RouteActionNonForwardingAction {
				return rc.statusErrWithNodeID(codes.Unavailable, "the incoming RPC matched to a route that was not of action type non forwarding")
			}
			rwi = &r
			break
		}
	}
	if rwi == nil {
		return rc.statusErrWithNodeID(codes.Unavailable, "the incoming RPC did not match a configured Route")
	}
	for _, interceptor := range rwi.interceptors {
		if err := interceptor.AllowRPC(ctx); err != nil {
			return rc.statusErrWithNodeID(codes.PermissionDenied, "Incoming RPC is not allowed: %v", err)
		}
	}
	return nil
}

// findBestMatchingVirtualHostServer returns the virtual host whose domains field best
// matches host
//
//	The domains field support 4 different matching pattern types:
//
//	- Exact match
//	- Suffix match (e.g. “*ABC”)
//	- Prefix match (e.g. “ABC*)
//	- Universal match (e.g. “*”)
//
//	The best match is defined as:
//	- A match is better if it’s matching pattern type is better.
//	  * Exact match > suffix match > prefix match > universal match.
//
//	- If two matches are of the same pattern type, the longer match is
//	  better.
//	  * This is to compare the length of the matching pattern, e.g. “*ABCDE” >
//	    “*ABC”
func findBestMatchingVirtualHostServer(authority string, vHosts []virtualHostWithInterceptors) *virtualHostWithInterceptors {
	var (
		matchVh   *virtualHostWithInterceptors
		matchType = domainMatchTypeInvalid
		matchLen  int
	)
	for _, vh := range vHosts {
		for _, domain := range vh.domains {
			typ, matched := match(domain, authority)
			if typ == domainMatchTypeInvalid {
				// The rds response is invalid.
				return nil
			}
			if matchType.betterThan(typ) || matchType == typ && matchLen >= len(domain) || !matched {
				// The previous match has better type, or the previous match has
				// better length, or this domain isn't a match.
				continue
			}
			matchVh = &vh
			matchType = typ
			matchLen = len(domain)
		}
	}
	return matchVh
}

type domainMatchType int

const (
	domainMatchTypeInvalid domainMatchType = iota
	domainMatchTypeUniversal
	domainMatchTypePrefix
	domainMatchTypeSuffix
	domainMatchTypeExact
)

// Exact > Suffix > Prefix > Universal > Invalid.
func (t domainMatchType) betterThan(b domainMatchType) bool {
	return t > b
}

func matchTypeForDomain(d string) domainMatchType {
	if d == "" {
		return domainMatchTypeInvalid
	}
	if d == "*" {
		return domainMatchTypeUniversal
	}
	if strings.HasPrefix(d, "*") {
		return domainMatchTypeSuffix
	}
	if strings.HasSuffix(d, "*") {
		return domainMatchTypePrefix
	}
	if strings.Contains(d, "*") {
		return domainMatchTypeInvalid
	}
	return domainMatchTypeExact
}

func match(domain, host string) (domainMatchType, bool) {
	switch typ := matchTypeForDomain(domain); typ {
	case domainMatchTypeInvalid:
		return typ, false
	case domainMatchTypeUniversal:
		return typ, true
	case domainMatchTypePrefix:
		// abc.*
		return typ, strings.HasPrefix(host, strings.TrimSuffix(domain, "*"))
	case domainMatchTypeSuffix:
		// *.123
		return typ, strings.HasSuffix(host, strings.TrimPrefix(domain, "*"))
	case domainMatchTypeExact:
		return typ, domain == host
	default:
		return domainMatchTypeInvalid, false
	}
}
