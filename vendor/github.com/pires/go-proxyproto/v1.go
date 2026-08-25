package proxyproto

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

const (
	crlf      = "\r\n"
	separator = " "

	// v1ProtoTCP4 and v1ProtoTCP6 are the PROXY protocol v1 transport-protocol
	// tokens for TCP over IPv4 and IPv6 respectively (net has no constants for
	// these textual identifiers).
	v1ProtoTCP4 = "TCP4"
	v1ProtoTCP6 = "TCP6"
)

// V1AcceptIPv4InTCP6 permits plain IPv4 literals in the address fields of a v1
// TCP6 line, promoting them to v4-mapped IPv6 (::ffff:x.x.x.x). Some proxies
// (notably the nginx OSS stream module) emit such lines when the client and
// backend address families differ. The spec forbids them — "the advertised
// protocol family dictates what format to use" — so this compatibility mode is
// disabled by default.
//
// Note the promotion is lossy: round-trip serialization renders the address as
// "::ffff:x.x.x.x" rather than the original "x.x.x.x".
//
// Like DefaultReadHeaderTimeout, this is a package-level variable to keep it
// easy to override. Set it at program init; it must not be modified
// concurrently with parsing.
var V1AcceptIPv4InTCP6 = false

func initVersion1() *Header {
	header := new(Header)
	header.Version = 1
	// Command doesn't exist in v1
	header.Command = PROXY
	return header
}

func parseVersion1(reader *bufio.Reader) (*Header, error) {
	//The header cannot be more than 107 bytes long. Per spec:
	//
	//   (...)
	//   - worst case (optional fields set to 0xff) :
	//     "PROXY UNKNOWN ffff:f...f:ffff ffff:f...f:ffff 65535 65535\r\n"
	//     => 5 + 1 + 7 + 1 + 39 + 1 + 39 + 1 + 5 + 1 + 5 + 2 = 107 chars
	//
	//   So a 108-byte buffer is always enough to store all the line and a
	//   trailing zero for string processing.
	//
	// It must also be CRLF terminated, as above. The header does not otherwise
	// contain a CR or LF byte.
	//
	// ISSUE #69
	// We can't use Peek here as it will block trying to fill the buffer, which
	// will never happen if the header is TCP4 or TCP6 (max. 56 and 104 bytes
	// respectively) and the server is expected to speak first.
	//
	// Similarly, we can't use ReadString or ReadBytes as these will keep reading
	// until the delimiter is found; an abusive client could easily disrupt a
	// server by sending a large amount of data that do not contain a LF byte.
	// Another means of attack would be to start connections and simply not send
	// data after the initial PROXY signature bytes, accumulating a large
	// number of blocked goroutines on the server. ReadSlice will also block for
	// a delimiter when the internal buffer does not fill up.
	//
	// A plain Read is also problematic since we risk reading past the end of the
	// header without being able to easily put the excess bytes back into the reader's
	// buffer (with the current implementation's design).
	//
	// So we use a ReadByte loop, which solves the overflow problem and avoids
	// reading beyond the end of the header. However, we need one more trick to harden
	// against partial header attacks (slow loris) - per spec:
	//
	//    (..) The sender must always ensure that the header is sent at once, so that
	//    the transport layer maintains atomicity along the path to the receiver. The
	//    receiver may be tolerant to partial headers or may simply drop the connection
	//    when receiving a partial header. Recommendation is to be tolerant, but
	//    implementation constraints may not always easily permit this.
	//
	// We are subject to such implementation constraints. So we return an error if
	// the header cannot be fully extracted with a single read of the underlying
	// reader.
	buf := make([]byte, 0, 107)
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrCantReadVersion1Header, err)
		}
		buf = append(buf, b)
		if b == '\n' {
			// End of header found
			break
		}
		if len(buf) == 107 {
			// No delimiter in first 107 bytes
			return nil, ErrVersion1HeaderTooLong
		}
		if reader.Buffered() == 0 {
			// Header was not buffered in a single read. Since we can't
			// differentiate between genuine slow writers and DoS agents,
			// we abort. On healthy networks, this should never happen.
			return nil, ErrCantReadVersion1Header
		}
	}

	// Check for CR before LF.
	if len(buf) < 2 || buf[len(buf)-2] != '\r' {
		return nil, ErrLineMustEndWithCrlf
	}

	// Check full signature. Read dispatches to this parser after peeking only
	// the first 5 bytes ("PROXY"), so the first token must still be checked to
	// be exactly "PROXY": per spec the line starts with "PROXY" followed by
	// exactly one space, and anything else (e.g. "PROXYjunk TCP4 ...") is not a
	// v1 header.
	tokens := strings.Split(string(buf[:len(buf)-2]), separator)
	if tokens[0] != "PROXY" {
		return nil, ErrCantReadVersion1Header
	}

	// Expect at least 2 tokens: "PROXY" and the transport protocol.
	if len(tokens) < 2 {
		return nil, ErrCantReadAddressFamilyAndProtocol
	}

	// Read address family and protocol
	var transportProtocol AddressFamilyAndProtocol
	switch tokens[1] {
	case v1ProtoTCP4:
		transportProtocol = TCPv4
	case v1ProtoTCP6:
		transportProtocol = TCPv6
	case "UNKNOWN":
		transportProtocol = UNSPEC // doesn't exist in v1 but fits UNKNOWN
	default:
		return nil, ErrCantReadAddressFamilyAndProtocol
	}

	// Expect exactly 6 tokens when UNKNOWN is not present. The spec's TCP4/TCP6
	// line is "PROXY <proto> <src> <dst> <sport> <dport>"; trailing tokens are
	// not permitted. (UNKNOWN is handled leniently below, per spec: the receiver
	// must ignore anything up to the CRLF.)
	if transportProtocol != UNSPEC && len(tokens) != 6 {
		return nil, ErrCantReadAddressFamilyAndProtocol
	}

	// When a signature is found, allocate a v1 header with Command set to PROXY.
	// Command doesn't exist in v1 but set it for other parts of this library
	// to rely on it for determining connection details.
	header := initVersion1()

	// Transport protocol has been processed already.
	header.TransportProtocol = transportProtocol

	// When UNKNOWN, set the command to LOCAL and return early
	if header.TransportProtocol == UNSPEC {
		header.Command = LOCAL
		return header, nil
	}

	// Otherwise, continue to read addresses and ports
	sourceIP, err := parseV1IPAddress(header.TransportProtocol, tokens[2])
	if err != nil {
		return nil, err
	}
	destIP, err := parseV1IPAddress(header.TransportProtocol, tokens[3])
	if err != nil {
		return nil, err
	}
	sourcePort, err := parseV1PortNumber(tokens[4])
	if err != nil {
		return nil, err
	}
	destPort, err := parseV1PortNumber(tokens[5])
	if err != nil {
		return nil, err
	}
	header.SourceAddr = &net.TCPAddr{
		IP:   sourceIP,
		Port: sourcePort,
	}
	header.DestinationAddr = &net.TCPAddr{
		IP:   destIP,
		Port: destPort,
	}

	return header, nil
}

func (header *Header) formatVersion1() ([]byte, error) {
	// As of version 1, only "TCP4" ( \x54 \x43 \x50 \x34 ) for TCP over IPv4,
	// and "TCP6" ( \x54 \x43 \x50 \x36 ) for TCP over IPv6 are allowed.
	var proto string
	switch header.TransportProtocol {
	case TCPv4:
		proto = v1ProtoTCP4
	case TCPv6:
		proto = v1ProtoTCP6
	default:
		// Unknown connection (short form)
		return []byte("PROXY UNKNOWN" + crlf), nil
	}

	sourceAddr, sourceOK := header.SourceAddr.(*net.TCPAddr)
	destAddr, destOK := header.DestinationAddr.(*net.TCPAddr)
	if !sourceOK || !destOK {
		return nil, ErrInvalidAddress
	}

	// A hand-built header can carry any int; the spec requires ports in the
	// decimal range 0..65535, so validate before serializing (mirrors the v2
	// port check in formatVersion2).
	if sourceAddr.Port < 0 || sourceAddr.Port > math.MaxUint16 || destAddr.Port < 0 || destAddr.Port > math.MaxUint16 {
		return nil, ErrInvalidPortNumber
	}

	// netip.Addr (not net.IP) is used here so String() honors the address family
	// declared by TransportProtocol. AddrFromSlice reports ok=false when the slice
	// is nil (e.g. To4() on an IPv6-only address), which the guard below rejects.
	var sourceIP, destIP netip.Addr
	switch header.TransportProtocol {
	case TCPv4:
		sourceIP, sourceOK = netip.AddrFromSlice(sourceAddr.IP.To4())
		destIP, destOK = netip.AddrFromSlice(destAddr.IP.To4())
	case TCPv6:
		// Use netip.Addr instead of net.IP to guarantee an Is6() address; i.e. a
		// v4-mapped IP in a TCP6 header serializes as ::ffff:1.2.3.4 instead of
		// net.IP.String()'s collapsed 1.2.3.4.
		sourceIP, sourceOK = netip.AddrFromSlice(sourceAddr.IP.To16())
		destIP, destOK = netip.AddrFromSlice(destAddr.IP.To16())
	default:
		// Unreachable today: the proto switch at the top of this function already
		// returns for anything other than TCPv4/TCPv6. Kept so a future protocol
		// can't fall through with zero-value IPs while sourceOK/destOK still hold
		// from the type assertion above.
		return nil, ErrInvalidAddress
	}
	if !sourceOK || !destOK {
		return nil, ErrInvalidAddress
	}

	buf := bytes.NewBuffer(make([]byte, 0, 108))
	buf.Write(SIGV1)
	buf.WriteString(separator)
	buf.WriteString(proto)
	buf.WriteString(separator)
	buf.WriteString(sourceIP.String())
	buf.WriteString(separator)
	buf.WriteString(destIP.String())
	buf.WriteString(separator)
	buf.WriteString(strconv.Itoa(sourceAddr.Port))
	buf.WriteString(separator)
	buf.WriteString(strconv.Itoa(destAddr.Port))
	buf.WriteString(crlf)

	return buf.Bytes(), nil
}

func parseV1PortNumber(portStr string) (int, error) {
	// Per spec, a v1 port is 1..5 decimal digits in the range 0..65535, with no
	// leading zero and no sign. ParseUint with bitSize 16 enforces all of that
	// (it rejects empty strings, signs, non-digits, and values above 65535)
	// except the leading zero ("080"), which the spec forbids and which creates
	// ambiguity for anything downstream that re-parses the address.
	if len(portStr) > 1 && portStr[0] == '0' {
		return 0, ErrInvalidPortNumber
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrInvalidPortNumber, err)
	}
	return int(port), nil
}

func parseV1IPAddress(protocol AddressFamilyAndProtocol, addrStr string) (net.IP, error) {
	addr, err := netip.ParseAddr(addrStr)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidAddress, err)
	}
	// netip accepts zoned literals ("fe80::1%eth0"). The spec's address grammar
	// does not (hex digits and colons only), and net.IP cannot carry a zone, so
	// accepting one would silently forward an address the sender never wrote.
	// Per spec, "any sequence which does not exactly match the protocol must be
	// discarded".
	if addr.Zone() != "" {
		return nil, ErrInvalidAddress
	}

	switch protocol {
	case TCPv4:
		if addr.Is4() {
			return net.IP(addr.AsSlice()), nil
		}
	case TCPv6:
		if addr.Is6() || addr.Is4In6() {
			return net.IP(addr.AsSlice()), nil
		}
		// Plain IPv4 in a TCP6 line is spec-invalid but emitted by some proxies;
		// see V1AcceptIPv4InTCP6 for the compatibility trade-off.
		if V1AcceptIPv4InTCP6 && addr.Is4() {
			mapped := netip.AddrFrom16(addr.As16())
			return net.IP(mapped.AsSlice()), nil
		}
	}

	return nil, ErrInvalidAddress
}
