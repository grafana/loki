package log

import (
	"errors"
	"fmt"
	"unicode"

	"inet.af/netaddr"
)

var (
	errInvalidPattern = errors.New("invalid pattern")
)

type IPMatchType int

const (
	IPV4_CHARSET = "0123456789."
	IPV6_CHARSET = "0123456789abcdefABCDEF:."

	IPMatchLine IPMatchType = iota
	IPMatchLabel
)

// Should be one of the netaddr.IP, netaddr.IPRange, netadd.IPPrefix.
type IPMatcher interface{}

type IPFilter struct {
	pattern   string
	matcher   IPMatcher
	matchType IPMatchType

	// if used as label matcher, this holds the identifier label name.
	// e.g: (|remote_addr = ip("xxx")). Here labelName is `remote_addr`
	labelName string

	// patternError represents any invalid pattern provided to match the ip.
	patternError error
}

// IPFilter search for IP addresses of given `pattern` in the given `line`.
// It returns true if pattern is matched with at least one IP in the `line`

// pattern - can be of the following form for both IPv4 and IPv6.
// 1. SINGLE-IP - "192.168.0.1"
// 2. IP RANGE  - "192.168.0.1-192.168.0.23"
// 3. CIDR      - "192.168.0.0/16"
func NewIPFilter(pattern string) *IPFilter {
	filter := &IPFilter{pattern: pattern}

	matcher, err := getMatcher(pattern)
	filter.matcher = matcher
	filter.patternError = err

	return filter
}

func NewIPLabelFilter(pattern string, label string) *IPFilter {
	filter := NewIPFilter(pattern)
	filter.labelName = label
	filter.matchType = IPMatchLabel
	return filter
}

func (ipf *IPFilter) Filter(line string) bool {
	if len(line) == 0 {
		return false
	}

	n := len(line)

	filterFn := func(line string, start int, charset string) (bool, int) {
		iplen := stringSpan(line[start:], charset)
		if iplen < 0 {
			return false, 0
		}
		ip, err := netaddr.ParseIP(line[start : start+iplen])
		if err == nil {
			if contains(ipf.matcher, ip) {
				return true, 0
			}
		}
		return false, iplen
	}

	// This loop try to extract IPv4 or IPv6 address from the arbitrary string.
	// It uses IPv4 and IPv6 prefix hints to find the IP addresses faster without using regexp.
	for i := 0; i < n; i++ {
		if i+3 < n && ipv4Hint([4]byte{line[i], line[i+1], line[i+2], line[i+3]}) {
			ok, iplen := filterFn(line, i, IPV4_CHARSET)
			if ok {
				return true
			}
			i += iplen
			continue
		}

		if i+4 < n && ipv6Hint([5]byte{line[i], line[i+1], line[i+2], line[i+3], line[i+4]}) {
			ok, iplen := filterFn(line, i, IPV6_CHARSET)
			if ok {
				return true
			}
			i += iplen
			continue
		}
	}
	return false
}

// `Process` implements `Stage` interface
func (ipf *IPFilter) Process(line []byte, lbs *LabelsBuilder) ([]byte, bool) {

	// make sure the pattern provided was valid, even before trying to match.
	if ipf.patternError != nil {
		lbs.SetErr(fmt.Errorf("%s: %s", errLabelFilter, ipf.patternError.Error()).Error())
		return line, false
	}

	input := string(line)

	if ipf.matchType == IPMatchLabel {
		v, ok := lbs.Get(ipf.labelName)
		if !ok {
			// we have not found the corresponding label.
			return line, false
		}
		input = v
	}

	return line, ipf.Filter(input)
}

// `RequiredLabelNames` implements `Stage` interface
func (ipf *IPFilter) RequiredLabelNames() []string {
	return []string{}
}

// `String` implements fmt.Stringer inteface, by which also implements `LabelFilterer` inteface.
func (ipf *IPFilter) String() string {
	if ipf.matchType == IPMatchLabel {
		return fmt.Sprintf("%s=ip(%q)", ipf.labelName, ipf.pattern)
	}
	return fmt.Sprintf("ip(%q)", ipf.pattern)
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

// stringSpan is same as C's `strcspan()` function.
// It returns the number of chars in the initial segment of `s`
// which consist only of chars from `accept`.
func stringSpan(s, accept string) int {
	m := make(map[rune]bool)

	for _, r := range accept {
		m[r] = true
	}

	for i, r := range s {
		if !m[r] {
			return i
		}
	}

	return len(s)
}
