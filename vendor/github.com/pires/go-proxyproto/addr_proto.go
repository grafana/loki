package proxyproto

// AddressFamilyAndProtocol represents address family and transport protocol.
type AddressFamilyAndProtocol byte

// AddressFamilyAndProtocol enum values.
const (
	UNSPEC       AddressFamilyAndProtocol = '\x00'
	TCPv4        AddressFamilyAndProtocol = '\x11'
	UDPv4        AddressFamilyAndProtocol = '\x12'
	TCPv6        AddressFamilyAndProtocol = '\x21'
	UDPv6        AddressFamilyAndProtocol = '\x22'
	UnixStream   AddressFamilyAndProtocol = '\x31'
	UnixDatagram AddressFamilyAndProtocol = '\x32'
)

// supportedTransportProtocol is the set of address-family/transport-protocol
// bytes defined by the PROXY protocol v2 spec (section 2.2). Any other value is
// rejected during v2 parsing. This matters for bytes that share a known family
// but an undefined transport (e.g. 0x13: IPv4 family, transport 3): they pass
// the IsIPv4/IsIPv6 family checks yet are neither stream nor datagram, so
// newIPAddr would return a nil net.Addr and later panic callers of
// RemoteAddr().String() / LocalAddr().String().
var supportedTransportProtocol = map[AddressFamilyAndProtocol]bool{
	UNSPEC:       true,
	TCPv4:        true,
	UDPv4:        true,
	TCPv6:        true,
	UDPv6:        true,
	UnixStream:   true,
	UnixDatagram: true,
}

// IsIPv4 returns true if the address family is IPv4 (AF_INET4), false otherwise.
func (ap AddressFamilyAndProtocol) IsIPv4() bool {
	return ap&0xF0 == 0x10
}

// IsIPv6 returns true if the address family is IPv6 (AF_INET6), false otherwise.
func (ap AddressFamilyAndProtocol) IsIPv6() bool {
	return ap&0xF0 == 0x20
}

// IsUnix returns true if the address family is UNIX (AF_UNIX), false otherwise.
func (ap AddressFamilyAndProtocol) IsUnix() bool {
	return ap&0xF0 == 0x30
}

// IsStream returns true if the transport protocol is TCP or STREAM (SOCK_STREAM), false otherwise.
func (ap AddressFamilyAndProtocol) IsStream() bool {
	return ap&0x0F == 0x01
}

// IsDatagram returns true if the transport protocol is UDP or DGRAM (SOCK_DGRAM), false otherwise.
func (ap AddressFamilyAndProtocol) IsDatagram() bool {
	return ap&0x0F == 0x02
}

// IsUnspec returns true if the transport protocol or address family is unspecified, false otherwise.
func (ap AddressFamilyAndProtocol) IsUnspec() bool {
	return (ap&0xF0 == 0x00) || (ap&0x0F == 0x00)
}

func (ap AddressFamilyAndProtocol) toByte() byte {
	if ap.IsIPv4() && ap.IsStream() {
		return byte(TCPv4)
	} else if ap.IsIPv4() && ap.IsDatagram() {
		return byte(UDPv4)
	} else if ap.IsIPv6() && ap.IsStream() {
		return byte(TCPv6)
	} else if ap.IsIPv6() && ap.IsDatagram() {
		return byte(UDPv6)
	} else if ap.IsUnix() && ap.IsStream() {
		return byte(UnixStream)
	} else if ap.IsUnix() && ap.IsDatagram() {
		return byte(UnixDatagram)
	}

	return byte(UNSPEC)
}
