package log

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"inet.af/netaddr"
)

var (
	errInvalidPattern = errors.New("invalid pattern")
)

const (
	IPV4_CHARSET = "0123456789."
	IPV6_CHARSET = "0123456789abcdefABCDEF.:"
)

// Should be one of the netaddr.IP, netaddr.IPRange, netadd.IPPrefix.
type IPMatcher interface{}

type IPFilter struct {
	matcher IPMatcher
}

// IPFilter search for IP addresses of given `pattern` in the given `line`.
// It returns true if pattern is matched with at least one IP in the `line`

// pattern - can be of the following form for both IPv4 and IPv6.
// 1. SINGLE-IP - "192.168.0.1"
// 2. IP RANGE  - "192.168.0.1-192.168.0.23"
// 3. CIDR      - "192.168.0.0/16"
func NewIPFilter(pattern string) (*IPFilter, error) {
	matcher, err := getMatcher(pattern)
	if err != nil {
		return nil, err
	}
	return &IPFilter{matcher: matcher}, nil
}

func (ipf *IPFilter) Filter(line string) bool {
	if len(line) == 0 {
		return false
	}

	n := len(line)

	for i := 0; i < n; i++ {
		if i+3 < n && ipv4Hint([4]byte{line[i], line[i+1], line[i+2], line[i+3]}) {
			start := i
			iplen := strings.LastIndexAny(line[start:], IPV4_CHARSET)
			ip, err := netaddr.ParseIP(line[start : start+iplen+1])
			if err == nil {
				if contains(ipf.matcher, ip) {
					return true
				}
			}
			i += iplen
			continue
		}

		if i+4 < n && ipv6Hint([5]byte{line[i], line[i+1], line[i+2], line[i+3], line[i+4]}) {
			start := i
			iplen := strings.LastIndex(line[start:], IPV6_CHARSET)
			ip, err := netaddr.ParseIP(line[start : start+iplen+1])
			if err == nil {
				if contains(ipf.matcher, ip) {
					return true
				}
			}
			i += iplen
			continue
		}
	}
	return false
}

func (ipf *IPFilter) Process(line []byte, _ *LabelsBuilder) ([]byte, bool) {
	return line, ipf.Filter(string(line))
}

func (ipf *IPFilter) RequiredLabelNames() []string {
	return []string{}
}

func contains(matcher IPMatcher, ip netaddr.IP) bool {
	switch m := matcher.(type) {
	case netaddr.IP:
		return m.Compare(ip) == 0
	case netaddr.IPRange:
		return m.Contains(ip)
	case netaddr.IPPrefix:
		return m.Contains(ip)
	}
	return false
}

func getMatcher(pattern string) (IPMatcher, error) {
	var (
		matcher IPMatcher
		err     error
	)

	matcher, err = netaddr.ParseIP(pattern) // is it simple single IP?
	if err == nil {
		return matcher, nil
	}
	matcher, err = netaddr.ParseIPPrefix(pattern) // is it cidr format? (192.168.0.1/16)
	if err == nil {
		return matcher, nil
	}

	matcher, err = netaddr.ParseIPRange(pattern) // is it IP range format? (192.168.0.1 - 192.168.4.5
	if err == nil {
		return matcher, nil
	}

	return nil, fmt.Errorf("%w: %q", errInvalidPattern, pattern)
}

func ipv4Hint(prefix [4]byte) bool {
	return unicode.IsDigit(rune(prefix[0])) && (prefix[1] == '.' || prefix[2] == '.' || prefix[3] == '.')
}

func ipv6Hint(prefix [5]byte) bool {
	hint1 := prefix[0] == ':' && prefix[1] == ':' && isHexDigit(prefix[2])
	hint2 := isHexDigit(prefix[0]) && prefix[1] == ':'
	hint3 := isHexDigit(prefix[0]) && isHexDigit(prefix[1]) && prefix[2] == ':'
	hint4 := isHexDigit(prefix[0]) && isHexDigit(prefix[1]) && isHexDigit(prefix[2]) && prefix[3] == ':'
	hint5 := isHexDigit(prefix[0]) && isHexDigit(prefix[1]) && isHexDigit(prefix[2]) && isHexDigit(prefix[3]) && prefix[4] == ':'

	return hint1 || hint2 || hint3 || hint4 || hint5
}

func isHexDigit(r byte) bool {
	return unicode.IsDigit(rune(r)) || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}
