package s3

import (
	"context"
	// We're shuffling addresses; not security-sensitive.
	"math/rand/v2" // nosemgrep: math-random-used
	"net"
	"time"
)

// shufflingDialer spreads connections across all addresses returned from DNS.
// Without this, every connection tends to land on the same IP.
//
// S3 publishes in DNS a rotating subset of its fleet with a short TTL, however
// Go's resolver sorts those records in RFC6724 order and net.Dialer takes the
// first address that connects.
//
// Resolving here means we lose the happy-eyeballs racing that net.Dialer does
// when DualStack is set. We also cannot replicate the way net.Dialer shadows
// nettrace, so Connect events will fire for DNS lookups.
type shufflingDialer struct {
	dialer *net.Dialer

	// lookupIPAddr and shuffle are hooks for tests. shuffle must permute addrs
	// in place.
	lookupIPAddr func(ctx context.Context, host string) ([]net.IPAddr, error)
	shuffle      func(addrs []net.IPAddr)
}

const (
	// maxDialAttempts bounds how many of the resolved addresses a single dial
	// will try. The total dialTimeout is divided across attempts, so if DNS
	// returns a really large number this could shrink each attempt's share
	// until a healthy-but-slow address gets timed out. orderAddrs interleaves
	// the address families so this cap cannot spend every attempt on one of
	// them.
	maxDialAttempts = 10

	// values coped from exthttp.DefaultTransport
	dialTimeout   = 30 * time.Second
	dialKeepAlive = 30 * time.Second
)

func newShufflingDialer() *shufflingDialer {
	return &shufflingDialer{
		dialer: &net.Dialer{
			Timeout:   dialTimeout,
			KeepAlive: dialKeepAlive,
		},
		lookupIPAddr: net.DefaultResolver.LookupIPAddr,
		shuffle: func(addrs []net.IPAddr) {
			rand.Shuffle(len(addrs), func(i, j int) { addrs[i], addrs[j] = addrs[j], addrs[i] })
		},
	}
}

func (d *shufflingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return d.dialer.DialContext(ctx, network, address)
	}

	// A literal IP has nothing to resolve or spread across.
	if net.ParseIP(host) != nil {
		return d.dialer.DialContext(ctx, network, address)
	}

	addrs, err := d.lookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	addrs = d.orderAddrs(network, addrs)
	if len(addrs) == 0 {
		return d.dialer.DialContext(ctx, network, address)
	}

	// Bound the total time the same way net.Dialer would have, then split what
	// is left evenly across the attempts we have not made yet, so one
	// black-holed address cannot consume the whole budget.
	deadline := time.Now().Add(dialTimeout)
	if d.dialer.Timeout > 0 {
		deadline = time.Now().Add(d.dialer.Timeout)
	}
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	attempts := min(len(addrs), maxDialAttempts)

	var firstErr error
	for i := range attempts {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}

		attemptCtx, cancel := context.WithTimeout(ctx, remaining/time.Duration(attempts-i))
		conn, err := d.dialer.DialContext(attemptCtx, network, net.JoinHostPort(addrs[i].String(), port))
		cancel()

		if err == nil {
			return conn, nil
		}
		if firstErr == nil {
			firstErr = err
		}
		// The caller gave up, or the transport cancelled the dial.
		if ctx.Err() != nil {
			break
		}
	}

	return nil, firstErr
}

// orderAddrs drops addresses the network does not permit, then shuffles within
// each address family and interleaves the two, leading with the family the
// resolver returned first. LookupIPAddr applies RFC 6724 sorting, so the
// preferred family leads - we only randomise which address within it gets used.
//
// The families are interleaved because shufflingDialer does not try v4 and v6 in parallel.
func (d *shufflingDialer) orderAddrs(network string, addrs []net.IPAddr) []net.IPAddr {
	var v4, v6 []net.IPAddr
	v6First := false

	for _, addr := range addrs {
		if addr.IP.To4() != nil {
			if network == "tcp6" {
				continue
			}
			v4 = append(v4, addr)
			continue
		}
		if network == "tcp4" {
			continue
		}
		if len(v4) == 0 && len(v6) == 0 {
			v6First = true
		}
		v6 = append(v6, addr)
	}

	d.shuffle(v4)
	d.shuffle(v6)

	if v6First {
		return interleave(v6, v4)
	}
	return interleave(v4, v6)
}

// interleave alternates between the two slices, starting with first, and takes
// whatever is left once the shorter one runs out.
func interleave(first, second []net.IPAddr) []net.IPAddr {
	out := make([]net.IPAddr, 0, len(first)+len(second))
	for i := range max(len(first), len(second)) {
		if i < len(first) {
			out = append(out, first[i])
		}
		if i < len(second) {
			out = append(out, second[i])
		}
	}
	return out
}
