package proxyproto

import (
	"fmt"
	"net"
	"strings"
)

// PolicyFunc can be used to decide whether to trust the PROXY info from
// upstream. If set, the connecting address is passed in as an argument.
//
// See below for the different policies.
//
// In case an error is returned the connection is denied: an error wrapping
// ErrInvalidUpstream denies just that connection while Listener.Accept keeps
// listening; any other error is returned by Accept itself, which typically
// stops the caller's accept loop.
//
// Deprecated: use ConnPolicyFunc instead.
type PolicyFunc func(upstream net.Addr) (Policy, error)

// ConnPolicyFunc can be used to decide whether to trust the PROXY info
// based on connection policy options. If set, the connecting addresses
// (remote and local) are passed in as argument.
//
// See below for the different policies.
//
// In case an error is returned the connection is denied: an error wrapping
// ErrInvalidUpstream denies just that connection while Listener.Accept keeps
// listening; any other error is returned by Accept itself, which typically
// stops the caller's accept loop.
type ConnPolicyFunc func(connPolicyOptions ConnPolicyOptions) (Policy, error)

// ConnPolicyOptions contains the remote and local addresses of a connection.
type ConnPolicyOptions struct {
	Upstream   net.Addr
	Downstream net.Addr
}

// Policy defines how a connection with a PROXY header address is treated.
type Policy int

const (
	// USE address from PROXY header.
	USE Policy = iota
	// IGNORE address from PROXY header, but accept connection.
	IGNORE
	// REJECT connection when PROXY header is sent
	// Note: if a PROXY header is present the first read returns
	// ErrSuperfluousProxyHeader, and every subsequent read returns the same
	// error, so the connection should be closed.
	REJECT
	// REQUIRE connection to send PROXY header, reject if not present
	// Note: if no PROXY header is present the first read returns
	// ErrNoProxyProtocol, and every subsequent read returns the same error,
	// so the connection should be closed.
	REQUIRE
	// SKIP accepts a connection without requiring the PROXY header.
	// Note: an example usage can be found in the SkipProxyHeaderForCIDR
	// function.
	//
	// On a Listener, SKIP short-circuits Accept and returns the raw, unwrapped
	// connection. On a Conn (via WithPolicy), a PROXY header that is present is
	// still consumed from the stream but discarded: ProxyHeader returns nil and
	// no validation runs.
	SKIP
)

// DefaultPolicy is the policy applied when none is configured explicitly: by
// NewConn when no WithPolicy option is given, and by Listener.Accept when
// neither Policy nor ConnPolicy is set.
//
// It defaults to REQUIRE, per the spec's mandate that a receiver "MUST not try
// to guess whether the protocol header is present or not": a connection that
// does not open with a PROXY header fails its first Read/Write with
// ErrNoProxyProtocol. Note REQUIRE alone still honors headers from any peer;
// restricting who may send one needs a policy such as TrustProxyHeaderFrom or
// TrustProxyHeaderFromRanges. (The deprecated *WhiteListPolicy helpers are
// NOT equivalent: they return USE for allowed peers, which replaces this
// default and makes the header optional again — that is why they were
// deprecated in favor of the explicit PolicyFromRanges.)
//
// Deployments that relied on the historical optional-header behavior can
// restore it process-wide with:
//
//	proxyproto.DefaultPolicy = proxyproto.USE
//
// Like DefaultReadHeaderTimeout, this is a package-level variable to keep it
// easy to override. Set it at program init; it must not be modified
// concurrently with accepting connections.
var DefaultPolicy = REQUIRE

// ConnSkipProxyHeaderForCIDR returns a ConnPolicyFunc which can be used to accept
// a connection from a skipHeaderCIDR without requiring a PROXY header, e.g.
// Kubernetes pods local traffic. The def is a policy to use when an upstream
// address doesn't match the skipHeaderCIDR.
func ConnSkipProxyHeaderForCIDR(skipHeaderCIDR *net.IPNet, def Policy) ConnPolicyFunc {
	return func(connOpts ConnPolicyOptions) (Policy, error) {
		ip, err := ipFromAddr(connOpts.Upstream)
		if err != nil {
			// Deny only this connection: wrapping ErrInvalidUpstream keeps
			// Listener.Accept listening instead of surfacing the error and
			// stopping the caller's accept loop.
			return def, fmt.Errorf("%w: %w", ErrInvalidUpstream, err)
		}

		if skipHeaderCIDR != nil && skipHeaderCIDR.Contains(ip) {
			return SKIP, nil
		}

		return def, nil
	}
}

// SkipProxyHeaderForCIDR returns a PolicyFunc which can be used to accept a
// connection from a skipHeaderCIDR without requiring a PROXY header, e.g.
// Kubernetes pods local traffic. The def is a policy to use when an upstream
// address doesn't match the skipHeaderCIDR.
//
// Deprecated: use ConnSkipProxyHeaderForCIDR instead.
func SkipProxyHeaderForCIDR(skipHeaderCIDR *net.IPNet, def Policy) PolicyFunc {
	connPolicy := ConnSkipProxyHeaderForCIDR(skipHeaderCIDR, def)
	return func(upstream net.Addr) (Policy, error) {
		return connPolicy(ConnPolicyOptions{Upstream: upstream})
	}
}

// WithPolicy adds given policy to a connection when passed as option to NewConn().
func WithPolicy(p Policy) func(*Conn) {
	return func(c *Conn) {
		c.ProxyHeaderPolicy = p
	}
}

// ConnLaxWhiteListPolicy returns a ConnPolicyFunc which decides whether the
// upstream ip is allowed to send a proxy header based on a list of allowed
// IP addresses and IP ranges. In case upstream IP is not in list the proxy
// header will be ignored. If one of the provided IP addresses or IP ranges
// is invalid it will return an error instead of a ConnPolicyFunc.
//
// Deprecated: the name hides that the header stays optional (USE) for allowed
// peers, overriding the REQUIRE default. Use the equivalent, explicit
// PolicyFromRanges(allowed, USE, IGNORE), or TrustProxyHeaderFromRanges for
// the spec-strict header-mandatory posture.
func ConnLaxWhiteListPolicy(allowed []string) (ConnPolicyFunc, error) {
	return PolicyFromRanges(allowed, USE, IGNORE)
}

// LaxWhiteListPolicy returns a PolicyFunc which decides whether the
// upstream ip is allowed to send a proxy header based on a list of allowed
// IP addresses and IP ranges. In case upstream IP is not in list the proxy
// header will be ignored. If one of the provided IP addresses or IP ranges
// is invalid it will return an error instead of a PolicyFunc.
//
// Deprecated: use PolicyFromRanges(allowed, USE, IGNORE) instead; see
// ConnLaxWhiteListPolicy for why.
func LaxWhiteListPolicy(allowed []string) (PolicyFunc, error) {
	connPolicy, err := ConnLaxWhiteListPolicy(allowed)
	if err != nil {
		return nil, err
	}

	return func(upstream net.Addr) (Policy, error) {
		return connPolicy(ConnPolicyOptions{Upstream: upstream})
	}, nil
}

// ConnMustLaxWhiteListPolicy returns a ConnLaxWhiteListPolicy but will panic
// if one of the provided IP addresses or IP ranges is invalid.
//
// Deprecated: use MustPolicyFromRanges(allowed, USE, IGNORE) instead; see
// ConnLaxWhiteListPolicy for why.
func ConnMustLaxWhiteListPolicy(allowed []string) ConnPolicyFunc {
	pfunc, err := ConnLaxWhiteListPolicy(allowed)
	if err != nil {
		panic(err)
	}

	return pfunc
}

// MustLaxWhiteListPolicy returns a LaxWhiteListPolicy but will panic if one
// of the provided IP addresses or IP ranges is invalid.
//
// Deprecated: use MustPolicyFromRanges(allowed, USE, IGNORE) instead; see
// ConnLaxWhiteListPolicy for why.
func MustLaxWhiteListPolicy(allowed []string) PolicyFunc {
	connPolicy := ConnMustLaxWhiteListPolicy(allowed)
	return func(upstream net.Addr) (Policy, error) {
		return connPolicy(ConnPolicyOptions{Upstream: upstream})
	}
}

// ConnStrictWhiteListPolicy returns a ConnPolicyFunc which decides whether the
// upstream ip is allowed to send a proxy header based on a list of allowed
// IP addresses and IP ranges. In case upstream IP is not in list reading on
// the connection will be refused: the first read returns
// ErrSuperfluousProxyHeader and every subsequent read returns the same error,
// so the connection should be closed. If one of the provided IP addresses or IP
// ranges is invalid it will return an error instead of a ConnPolicyFunc.
//
// Deprecated: "strict" only refers to refusing headers from unlisted peers;
// the header stays optional (USE) for allowed peers, overriding the REQUIRE
// default. Use the equivalent, explicit PolicyFromRanges(allowed, USE,
// REJECT), or TrustProxyHeaderFromRanges for the spec-strict header-mandatory
// posture.
func ConnStrictWhiteListPolicy(allowed []string) (ConnPolicyFunc, error) {
	return PolicyFromRanges(allowed, USE, REJECT)
}

// StrictWhiteListPolicy returns a PolicyFunc which decides whether the
// upstream ip is allowed to send a proxy header based on a list of allowed
// IP addresses and IP ranges. In case upstream IP is not in list reading on
// the connection will be refused: the first read returns
// ErrSuperfluousProxyHeader and every subsequent read returns the same error,
// so the connection should be closed. If one of the provided IP addresses or IP
// ranges is invalid it will return an error instead of a PolicyFunc.
//
// Deprecated: use PolicyFromRanges(allowed, USE, REJECT) instead; see
// ConnStrictWhiteListPolicy for why.
func StrictWhiteListPolicy(allowed []string) (PolicyFunc, error) {
	connPolicy, err := ConnStrictWhiteListPolicy(allowed)
	if err != nil {
		return nil, err
	}

	return func(upstream net.Addr) (Policy, error) {
		return connPolicy(ConnPolicyOptions{Upstream: upstream})
	}, nil
}

// ConnMustStrictWhiteListPolicy returns a ConnStrictWhiteListPolicy but will panic
// if one of the provided IP addresses or IP ranges is invalid.
//
// Deprecated: use MustPolicyFromRanges(allowed, USE, REJECT) instead; see
// ConnStrictWhiteListPolicy for why.
func ConnMustStrictWhiteListPolicy(allowed []string) ConnPolicyFunc {
	pfunc, err := ConnStrictWhiteListPolicy(allowed)
	if err != nil {
		panic(err)
	}

	return pfunc
}

// MustStrictWhiteListPolicy returns a StrictWhiteListPolicy but will panic
// if one of the provided IP addresses or IP ranges is invalid.
//
// Deprecated: use MustPolicyFromRanges(allowed, USE, REJECT) instead; see
// ConnStrictWhiteListPolicy for why.
func MustStrictWhiteListPolicy(allowed []string) PolicyFunc {
	connPolicy := ConnMustStrictWhiteListPolicy(allowed)
	return func(upstream net.Addr) (Policy, error) {
		return connPolicy(ConnPolicyOptions{Upstream: upstream})
	}
}

func connRangesPolicy(allowed []func(net.IP) bool, matched, unmatched Policy) ConnPolicyFunc {
	return func(connOpts ConnPolicyOptions) (Policy, error) {
		upstreamIP, err := ipFromAddr(connOpts.Upstream)
		if err != nil {
			// Something is wrong with the source IP: deny only this connection.
			// Wrapping ErrInvalidUpstream keeps Listener.Accept listening instead
			// of surfacing the error and stopping the caller's accept loop.
			return REJECT, fmt.Errorf("%w: %w", ErrInvalidUpstream, err)
		}

		for _, allowFrom := range allowed {
			if allowFrom(upstreamIP) {
				return matched, nil
			}
		}

		return unmatched, nil
	}
}

// PolicyFromRanges returns a ConnPolicyFunc that applies the matched policy to
// connections whose upstream address belongs to ranges, and the unmatched
// policy to every other connection. Each entry in ranges may be an individual
// IP address ("10.0.0.10") or a CIDR range ("10.0.0.0/24"); an invalid entry
// returns an error instead of a ConnPolicyFunc. Connections whose upstream
// address cannot be classified are dropped by Listener.Accept via an
// ErrInvalidUpstream-wrapping error.
//
// It is the explicit replacement for the deprecated *WhiteListPolicy helpers:
//
//	PolicyFromRanges(ranges, USE, IGNORE) // ConnLaxWhiteListPolicy
//	PolicyFromRanges(ranges, USE, REJECT) // ConnStrictWhiteListPolicy
//
// Both of those combinations leave the header optional for matched peers. For
// the spec-strict posture — trusted sources MUST send a header, everything
// else is dropped — use TrustProxyHeaderFromRanges instead.
func PolicyFromRanges(ranges []string, matched, unmatched Policy) (ConnPolicyFunc, error) {
	allowFrom, err := parse(ranges)
	if err != nil {
		return nil, err
	}

	return connRangesPolicy(allowFrom, matched, unmatched), nil
}

// MustPolicyFromRanges returns PolicyFromRanges and panics if any entry in
// ranges is invalid. Intended for static configuration known at program init.
func MustPolicyFromRanges(ranges []string, matched, unmatched Policy) ConnPolicyFunc {
	pfunc, err := PolicyFromRanges(ranges, matched, unmatched)
	if err != nil {
		panic(err)
	}

	return pfunc
}

func parse(allowed []string) ([]func(net.IP) bool, error) {
	a := make([]func(net.IP) bool, len(allowed))
	for i, allowFrom := range allowed {
		if strings.LastIndex(allowFrom, "/") > 0 {
			_, ipRange, err := net.ParseCIDR(allowFrom)
			if err != nil {
				return nil, fmt.Errorf("proxyproto: given string %q is not a valid IP range: %v", allowFrom, err)
			}

			a[i] = ipRange.Contains
		} else {
			allowed := net.ParseIP(allowFrom)
			if allowed == nil {
				return nil, fmt.Errorf("proxyproto: given string %q is not a valid IP address", allowFrom)
			}

			a[i] = allowed.Equal
		}
	}

	return a, nil
}

func ipFromAddr(upstream net.Addr) (net.IP, error) {
	upstreamString, _, err := net.SplitHostPort(upstream.String())
	if err != nil {
		return nil, err
	}

	upstreamIP := net.ParseIP(upstreamString)
	if nil == upstreamIP {
		return nil, fmt.Errorf("proxyproto: invalid IP address")
	}

	return upstreamIP, nil
}

// TrustProxyHeaderFrom returns a ConnPolicyFunc implementing the spec's
// receiver posture end to end: connections from trusted IPs MUST carry a PROXY
// header (REQUIRE — absence fails the first Read with ErrNoProxyProtocol, so
// header presence is never guessed), and connections from any other source are
// dropped by Listener.Accept without stopping the listener.
//
// Note the REJECT policy alone cannot provide the second half: REJECT refuses
// connections that DO send a header, but a headerless connection from an
// untrusted peer would still be served raw, sharing the port between PROXY and
// non-PROXY traffic — exactly what the spec's security model forbids.
func TrustProxyHeaderFrom(trustedIPs ...net.IP) ConnPolicyFunc {
	return func(connOpts ConnPolicyOptions) (Policy, error) {
		ip, err := ipFromAddr(connOpts.Upstream)
		if err != nil {
			// Deny only this connection: wrapping ErrInvalidUpstream keeps
			// Listener.Accept listening instead of surfacing the error and
			// stopping the caller's accept loop.
			return REJECT, fmt.Errorf("%w: %w", ErrInvalidUpstream, err)
		}

		for _, trustedIP := range trustedIPs {
			if trustedIP.Equal(ip) {
				return REQUIRE, nil
			}
		}

		return REJECT, fmt.Errorf("%w: %s is not a trusted PROXY sender", ErrInvalidUpstream, ip)
	}
}

// TrustProxyHeaderFromRanges is the CIDR-capable variant of
// TrustProxyHeaderFrom, with the same spec posture: connections from the
// trusted set MUST carry a PROXY header (REQUIRE), and connections from any
// other source are dropped by Listener.Accept without stopping the listener.
// Each entry in trusted may be an individual IP address ("10.0.0.10") or a
// CIDR range ("10.0.0.0/24"); an invalid entry returns an error instead of a
// ConnPolicyFunc.
//
// Unlike the *WhiteListPolicy helpers — which make the header optional (USE)
// for allowed peers and merely ignore or refuse the header for denied ones —
// this helper never guesses whether a header is present.
func TrustProxyHeaderFromRanges(trusted []string) (ConnPolicyFunc, error) {
	allowFrom, err := parse(trusted)
	if err != nil {
		return nil, err
	}

	return func(connOpts ConnPolicyOptions) (Policy, error) {
		ip, err := ipFromAddr(connOpts.Upstream)
		if err != nil {
			// Deny only this connection: wrapping ErrInvalidUpstream keeps
			// Listener.Accept listening instead of surfacing the error and
			// stopping the caller's accept loop.
			return REJECT, fmt.Errorf("%w: %w", ErrInvalidUpstream, err)
		}

		for _, allow := range allowFrom {
			if allow(ip) {
				return REQUIRE, nil
			}
		}

		return REJECT, fmt.Errorf("%w: %s is not a trusted PROXY sender", ErrInvalidUpstream, ip)
	}, nil
}

// IgnoreProxyHeaderNotOnInterface returns a ConnPolicyFunc which can be used to
// decide whether to use or ignore PROXY headers depending on the connection
// being made on specific interfaces. This policy can be used when the server
// is bound to multiple interfaces but wants to allow on one or more interfaces.
func IgnoreProxyHeaderNotOnInterface(allowedIP net.IP) ConnPolicyFunc {
	return func(connOpts ConnPolicyOptions) (Policy, error) {
		ip, err := ipFromAddr(connOpts.Downstream)
		if err != nil {
			// The local (downstream) address cannot be classified; deny only this
			// connection. Wrapping ErrInvalidUpstream keeps Listener.Accept
			// listening instead of surfacing the error and stopping the caller's
			// accept loop.
			return REJECT, fmt.Errorf("%w: %w", ErrInvalidUpstream, err)
		}

		if allowedIP.Equal(ip) {
			return USE, nil
		}

		return IGNORE, nil
	}
}
