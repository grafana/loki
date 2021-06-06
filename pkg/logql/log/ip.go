package log

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// This actually implements the Stage interface to process the line
type LineIPFiler struct {
	pattern *net4
	parser  *IP4parser
}

func NewLineIpFilter(pattern string) (Stage, error) {
	// parse `pat` into net4 spec.
	spec, err := parseNet4(pattern)
	if err != nil {
		return nil, err
	}

	return &LineIPFiler{
		pattern: spec,
		parser:  newIP4Parser(),
	}, nil
}

func (f *LineIPFiler) Process(line []byte, _ *LabelsBuilder) ([]byte, bool) {
	return line, filterIP(f.pattern, f.parser, string(line))
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
func filterIP(pat *net4, parser *IP4parser, line string) bool {
	if len(line) == 0 {
		return false
	}

	// look for any ip in given `line` that matches the spec.
	// Stop and return `true` if at least one match is found.
	for i := 0; i < len(line)-4; i++ {
		// use IPV4 hint. faster than regexp.
		// if first byte is digit, and if second or third or fourth byte is '.' then good chance
		// it can be ip.
		// example: "1.x" or "12.x" or "123.x"
		if unicode.IsDigit(rune(line[i])) && (line[i+1] == '.' || line[i+2] == '.' || line[i+3] == '.') {
			start := i
			end := strings.LastIndexAny(line[start:], IPV4_CHARSET)
			ip, err := parser.parseIP4(line[start : start+end+1])
			if err != nil {
				// TODO(kavi): We can do a better job of incrementing the offset when its not a valid IP.
				continue
			}
			ipv := uint32(buildIP4(ip))
			if ipv >= pat.min && ipv <= pat.max {
				// got the match
				return true
			}
		}
	}

	return false
}

type IP4parser struct {
	buf *bytes.Buffer
}

func newIP4Parser() *IP4parser {
	return &IP4parser{
		buf: bytes.NewBuffer(make([]byte, 0, 4)),
	}
}

// parseIP4 takes the input string `s` try to parse it into
// array of 4 uints.
func (p *IP4parser) parseIP4(s string) ([4]uint, error) {
	var (
		ip [4]uint
		i  int
	)
	defer p.buf.Reset()

	for _, r := range s {
		if i > 3 {
			return ip, errInValidIP
		}
		if r == '.' { // 127.0.0.1\
			if p.buf.Len() == 0 { // .x or x..y invalid
				return ip, errInValidIP
			}
			v, err := strconv.ParseInt(p.buf.String(), 10, 32)
			if err != nil {
				return ip, errInValidIP
			}
			ip[i] = uint(v)
			p.buf.Reset()
			i++
			continue
		}
		_, _ = p.buf.WriteRune(r)
	}

	if i != 3 {
		return ip, errInValidIP
	}

	// don't forget the last octet.
	v, err := strconv.ParseInt(p.buf.String(), 10, 32)
	if err != nil {
		return ip, errInValidIP
	}
	ip[i] = uint(v)

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
