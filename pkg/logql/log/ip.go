package log

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// This actually implements the Stage interface to process the line
type LineIPFiler struct {
	pat string
}

func NewLineIpFilter(pattern string) (Stage, error) {
	return &LineIPFiler{pat: pattern}, nil
}

func (f *LineIPFiler) Process(line []byte, _ *LabelsBuilder) ([]byte, bool) {
	match, _ := ip(f.pat, string(line))
	if match {
		return line, true
	}
	return line, false
}

func (f *LineIPFiler) RequiredLabelNames() []string {
	return []string{}
}

const (
	IPV4_CHARSET = "0123456789."
)

// ip search for IP addresses of given pattern `pat` in the given `line`.
// It returns true if pattern is matched with at least one IP in the `line`

// pat - can be of the following form
// 1. SINGLE-IP - "192.168.0.1"
// 2. IP RANGE  - "192.168.0.1-192.168.0.23"
// 3. CIDR      - "192.168.0.0/16"

// TODO(kavi): It works only for ipv4. Add support for ipv6
func ip(pat, line string) (bool, error) {
	if len(line) == 0 {
		return false, nil
	}

	// parse `pat` into net4 spec.
	spec, err := parseNet4(pat)
	if err != nil {
		return false, err
	}

	// look for any ip in given `line` that matches the spec.
	// Stop and return `true` if at least one match is found.
	for i := 0; i < len(line)-4; i++ {
		// use IPV4 hint. faster than regexp.
		// if first byte is digit, and if second or third or fourth byte is '.' then good chance
		// it can be ip.
		// example: "1.x" or "12.x" or "123.x"
		if unicode.IsDigit(rune(line[i])) && (line[i+11] == '.' || line[i+2] == '.' || line[i+3] == '.') {
			start := i
			end := strings.LastIndexAny(line[start:], IPV4_CHARSET)
			ip, err := parseIP4(line[start : start+end+1])
			if err != nil {
				// TODO(kavi): We can do a better job of incrementing the offset when its not a valid IP.
				continue
			}
			ipv := uint32(buildIP4(ip))
			if ipv >= spec.min && ipv <= spec.max {
				// got the match
				return true, nil
			}
		}
	}

	return false, nil
}

// parseIP4 takes the input string `s` try to parse it into
// array of 4 uints.
func parseIP4(s string) ([4]uint, error) {
	var ip [4]uint

	_, err := fmt.Sscanf(s, "%d.%d.%d.%d", &ip[0], &ip[1], &ip[2], &ip[3])
	if err != nil {
		return ip, err
	}

	return ip, nil
}

var (
	errInValidIP   = errors.New("not a valid IP pattern")
	errInValidMask = errors.New("not a valid IP mask")
	errIPRange     = errors.New("not valid IP range")
)

// net4 is IPV4 version of pattern either in CIDR or Range format.
// for single IP, both min and max = ip itself.
type net4 struct {
	min, max uint32
}

func (n *net4) String() string {
	return fmt.Sprintf("%s-%s", formatIP4(n.min), formatIP4(n.max))
}

func formatIP4(ip uint32) string {
	bitmask := uint32((1 << 8) - 1)
	var ip1 [4]uint32

	ip1[3] = bitmask & ip
	ip1[2] = bitmask & (ip >> 8)
	ip1[1] = bitmask & (ip >> 16)
	ip1[0] = bitmask & (ip >> 24)

	return fmt.Sprintf("%d.%d.%d.%d", ip1[0], ip1[1], ip1[2], ip1[3])
}

// net6 is IPV6 version of pattern
type net6 struct {
	min, max [16]uint8
}

func parseNet4(pat string) (*net4, error) {
	var (
		spec     net4
		ip1, ip2 [4]uint
		mask     int
	)

	// try parsing in CIDR format
	// TODO(kavi): Use net.CIDR instead??
	if strings.ContainsRune(pat, '/') {
		_, err := fmt.Sscanf(pat, "%d.%d.%d.%d/%d", &ip1[0], &ip1[1], &ip1[2], &ip1[3], &mask)
		if err != nil {
			return nil, errors.New("invalid CIDR format")
		}
		if mask < 0 || mask > 32 {
			return nil, errInValidMask
		}

		if !validIP4(ip1) {
			return nil, errors.New("invalid IP in CIDR format")
		}

		ip := buildIP4(ip1)

		spec.min = uint32(ip) & (^((1 << (32 - mask)) - 1) & 0xFFFFFFFF)
		spec.max = spec.min | (((1 << (32 - mask)) - 1) & 0xFFFFFFFF)
		return &spec, nil
	}

	// try parsing in IP Range
	if strings.ContainsRune(pat, '-') {
		// TODO(kavi): should we allow spaces between '-' on both sides?
		_, err := fmt.Sscanf(pat, "%d.%d.%d.%d-%d.%d.%d.%d", &ip1[0], &ip1[1], &ip1[2], &ip1[3], &ip2[0], &ip2[1], &ip2[2], &ip2[3])
		if err != nil {
			return nil, errors.New("invalid IP range format")
		}

		spec.min = uint32(buildIP4(ip1))
		spec.max = uint32(buildIP4(ip2))

		if spec.min > spec.max {
			return nil, errors.New("invalid IP range 'end' should be greater")
		}

		return &spec, nil

	}

	// try parsing as single IP
	n, err := fmt.Sscanf(pat, "%d.%d.%d.%d", &ip1[0], &ip1[1], &ip1[2], &ip1[3])
	if n == 4 && err == nil {
		spec.min = uint32(buildIP4(ip1))
		spec.max = spec.min
		return &spec, nil
	}

	// given pattern is invalid.
	return nil, errInValidIP
}

// validIP4 checks whether given 4 element array is valid IP octets.
func validIP4(ip [4]uint) bool {
	return ip[0] < 256 && ip[1] < 256 && ip[2] < 256 && ip[3] < 256
}

// buildIP4 builds
func buildIP4(ip [4]uint) uint {
	return ip[0]<<24 | ip[1]<<16 | ip[2]<<8 | ip[3]
}
