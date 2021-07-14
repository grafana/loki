package log

import (
	"errors"
	"fmt"
	"unicode"

	"github.com/prometheus/prometheus/pkg/labels"
	"inet.af/netaddr"
)

var (
	ErrIPFilterInvalidPattern   = errors.New("ip: invalid pattern")
	ErrIPFilterInvalidOperation = errors.New("ip: invalid operation")
)

type IPMatchType int

const (
	IPv4Charset = "0123456789."
	IPv6Charset = "0123456789abcdefABCDEF:."

	IPMatchLine IPMatchType = iota
	IPMatchLabel
)

// Should be one of the netaddr.IP, netaddr.IPRange, netadd.IPPrefix.
type IPMatcher interface{}

// IPFilter search for IP addresses of given `pattern` in the given `line`.
// It returns true if pattern is matched with at least one IP in the `line`

// pattern - can be of the following form for both IPv4 and IPv6.
// 1. SINGLE-IP - "192.168.0.1"
// 2. IP RANGE  - "192.168.0.1-192.168.0.23"
// 3. CIDR      - "192.168.0.0/16"
type IPFilter struct {
	pattern   string
	matcher   IPMatcher
	matchType IPMatchType

	// if used as label matcher, this holds the identifier label name.
	// e.g: (|remote_addr = ip("xxx")). Here labelName is `remote_addr`
	labelName string

	// patternError represents any invalid pattern provided to match the ip.
	patternError error

	// filter `operation` used during label filter.
	labelOp LabelFilterType

	// filter `operation` used during line filter.
	lineOp labels.MatchType
}

func newIPFilter(pattern string) *IPFilter {
	filter := &IPFilter{pattern: pattern}

	matcher, err := getMatcher(pattern)
	filter.matcher = matcher
	filter.patternError = err

	return filter
}

// NewIPLineFilter is used to construct ip filter as a `LineFilter`
func NewIPLineFilter(pattern string, op labels.MatchType) *IPFilter {
	filter := newIPFilter(pattern)
	filter.matchType = IPMatchLine
	filter.lineOp = op
	return filter
}

// NewIPLabelFilter is used to construct ip filter as label filter for the given `label`.
func NewIPLabelFilter(label string, op LabelFilterType, pattern string) *IPFilter {
	filter := newIPFilter(pattern)
	filter.matchType = IPMatchLabel
	filter.labelName = label
	filter.labelOp = op
	return filter
}

// filter does the heavy lifting finding ip `pattern` in the givin `line`.
// This is the function if you want to understand how the core logic how ip filter works!
func (ipf *IPFilter) filter(line []byte) bool {
	if len(line) == 0 {
		return false
	}

	n := len(line)

	filterFn := func(line []byte, start int, charset string) (bool, int) {
		iplen := bytesSpan(line[start:], []byte(charset))
		if iplen < 0 {
			return false, 0
		}
		ip, err := netaddr.ParseIP(string(line[start : start+iplen]))
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
			ok, iplen := filterFn(line, i, IPv4Charset)
			if ok {
				return true
			}
			i += iplen
			continue
		}

		if i+4 < n && ipv6Hint([5]byte{line[i], line[i+1], line[i+2], line[i+3], line[i+4]}) {
			ok, iplen := filterFn(line, i, IPv6Charset)
			if ok {
				return true
			}
			i += iplen
			continue
		}
	}
	return false
}

// Filter implement `Filterer` interface. Used by `LineFilter`
func (ipf *IPFilter) Filter(line []byte) bool {
	ok := ipf.filter(line)

	fmt.Println("linefilter here?", "op", ipf.lineOp, "equal?", ipf.lineOp == labels.MatchNotEqual)

	if ipf.lineOp == labels.MatchNotEqual {
		return !ok
	}
	return ok
}

// ToStage implements `Filterer` interface.
func (ipf *IPFilter) ToStage() Stage {
	return ipf
}

// `Process` implements `Stage` interface
func (ipf *IPFilter) Process(line []byte, lbs *LabelsBuilder) ([]byte, bool) {

	// make sure the pattern provided was valid, even before trying to match.
	if ipf.patternError != nil {
		lbs.SetErr(fmt.Errorf("%s: %s", errLabelFilter, ipf.patternError.Error()).Error())
		return line, false
	}

	switch ipf.matchType {
	case IPMatchLine:
		if ipf.lineOp == labels.MatchNotEqual {
			return line, !ipf.filter(line)
		}
		return line, ipf.filter(line)
	case IPMatchLabel:
		v, ok := lbs.Get(ipf.labelName)
		if !ok {
			// we have not found the corresponding label.
			return line, false
		}
		switch ipf.labelOp {
		case LabelFilterEqual:
			return line, ipf.filter([]byte(v))
		case LabelFilterNotEqual:
			return line, !ipf.filter([]byte(v))
		default:
			lbs.SetErr(ErrIPFilterInvalidOperation.Error())
			return line, false
		}
	}
	return line, false
}

// `RequiredLabelNames` implements `Stage` interface
func (ipf *IPFilter) RequiredLabelNames() []string {
	return []string{ipf.labelName}
}

// `String` implements fmt.Stringer inteface, by which also implements `LabelFilterer` inteface.
func (ipf *IPFilter) String() string {
	eq := "=" // LabelMatchNotEqual emits `==` string. which we don't want.
	if ipf.matchType == IPMatchLabel {
		if ipf.labelOp == LabelFilterNotEqual {
			eq = "!="
		}
		return fmt.Sprintf("%s%sip(%q)", ipf.labelName, eq, ipf.pattern)
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

	return nil, fmt.Errorf("%w: %q", ErrIPFilterInvalidPattern, pattern)
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

// bytesSpan is same as C's `strcspan()` function.
// It returns the number of chars in the initial segment of `s`
// which consist only of chars from `accept`.
func bytesSpan(s, accept []byte) int {
	m := make(map[byte]bool)

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
