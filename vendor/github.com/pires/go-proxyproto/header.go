// Package proxyproto implements Proxy Protocol (v1 and v2) parser and writer, as per specification:
// https://www.haproxy.org/download/2.3/doc/proxy-protocol.txt
package proxyproto

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"time"
)

var (
	// SIGV1 is the signature for PROXY protocol v1.
	SIGV1 = []byte{'\x50', '\x52', '\x4F', '\x58', '\x59'}
	// SIGV2 is the signature for PROXY protocol v2.
	SIGV2 = []byte{'\x0D', '\x0A', '\x0D', '\x0A', '\x00', '\x0D', '\x0A', '\x51', '\x55', '\x49', '\x54', '\x0A'}

	// ErrCantReadVersion1Header indicates a v1 header could not be read.
	ErrCantReadVersion1Header = errors.New("proxyproto: can't read version 1 header")
	// ErrVersion1HeaderTooLong indicates a v1 header is too long.
	ErrVersion1HeaderTooLong = errors.New("proxyproto: version 1 header must be 107 bytes or less")
	// ErrLineMustEndWithCrlf indicates a v1 header is invalid, must end with \r\n.
	ErrLineMustEndWithCrlf = errors.New("proxyproto: version 1 header is invalid, must end with \\r\\n")
	// ErrCantReadProtocolVersionAndCommand indicates a protocol version and command could not be read.
	ErrCantReadProtocolVersionAndCommand = errors.New("proxyproto: can't read proxy protocol version and command")
	// ErrCantReadAddressFamilyAndProtocol indicates an address family and protocol could not be read.
	ErrCantReadAddressFamilyAndProtocol = errors.New("proxyproto: can't read address family or protocol")
	// ErrCantReadLength indicates a length could not be read.
	ErrCantReadLength = errors.New("proxyproto: can't read length")
	// ErrCantResolveSourceUnixAddress indicates a source Unix address could not be resolved.
	ErrCantResolveSourceUnixAddress = errors.New("proxyproto: can't resolve source Unix address")
	// ErrCantResolveDestinationUnixAddress indicates a destination Unix address could not be resolved.
	ErrCantResolveDestinationUnixAddress = errors.New("proxyproto: can't resolve destination Unix address")
	// ErrNoProxyProtocol indicates a proxy protocol signature is not present.
	ErrNoProxyProtocol = errors.New("proxyproto: proxy protocol signature not present")
	// ErrUnknownProxyProtocolVersion indicates an unknown proxy protocol version.
	ErrUnknownProxyProtocolVersion = errors.New("proxyproto: unknown proxy protocol version")
	// ErrUnsupportedProtocolVersionAndCommand indicates an unsupported protocol version and command.
	ErrUnsupportedProtocolVersionAndCommand = errors.New("proxyproto: unsupported proxy protocol version and command")
	// ErrUnsupportedAddressFamilyAndProtocol indicates an unsupported address family and protocol.
	ErrUnsupportedAddressFamilyAndProtocol = errors.New("proxyproto: unsupported address family and protocol")
	// ErrInvalidLength indicates an invalid length.
	ErrInvalidLength = errors.New("proxyproto: invalid length")
	// ErrInvalidAddress indicates an invalid address.
	ErrInvalidAddress = errors.New("proxyproto: invalid address")
	// ErrInvalidPortNumber indicates an invalid port number.
	ErrInvalidPortNumber = errors.New("proxyproto: invalid port number")
	// ErrSuperfluousProxyHeader indicates an upstream connection sent a PROXY header but isn't allowed to send one.
	ErrSuperfluousProxyHeader = errors.New("proxyproto: upstream connection sent PROXY header but isn't allowed to send one")
)

// Header is the placeholder for proxy protocol header.
type Header struct {
	Version           byte
	Command           ProtocolVersionAndCommand
	TransportProtocol AddressFamilyAndProtocol
	SourceAddr        net.Addr
	DestinationAddr   net.Addr
	rawTLVs           []byte
}

// HeaderProxyFromAddrs creates a new PROXY header from a source and a
// destination address. If version is zero, the latest protocol version is
// used.
//
// The header is filled on a best-effort basis: if hints cannot be inferred
// from the provided addresses, the header will be left unspecified.
func HeaderProxyFromAddrs(version byte, sourceAddr, destAddr net.Addr) *Header {
	if version < 1 || version > 2 {
		version = 2
	}
	h := &Header{
		Version:           version,
		Command:           LOCAL,
		TransportProtocol: UNSPEC,
	}
	switch sourceAddr := sourceAddr.(type) {
	case *net.TCPAddr:
		if _, ok := destAddr.(*net.TCPAddr); !ok {
			break
		}
		if len(sourceAddr.IP.To4()) == net.IPv4len {
			h.TransportProtocol = TCPv4
		} else if len(sourceAddr.IP) == net.IPv6len {
			h.TransportProtocol = TCPv6
		}
	case *net.UDPAddr:
		if _, ok := destAddr.(*net.UDPAddr); !ok {
			break
		}
		if len(sourceAddr.IP.To4()) == net.IPv4len {
			h.TransportProtocol = UDPv4
		} else if len(sourceAddr.IP) == net.IPv6len {
			h.TransportProtocol = UDPv6
		}
	case *net.UnixAddr:
		if _, ok := destAddr.(*net.UnixAddr); !ok {
			break
		}
		switch sourceAddr.Net {
		case "unix":
			h.TransportProtocol = UnixStream
		case "unixgram":
			h.TransportProtocol = UnixDatagram
		}
	}
	if h.TransportProtocol != UNSPEC {
		h.Command = PROXY
		h.SourceAddr = sourceAddr
		h.DestinationAddr = destAddr
	}
	return h
}

// TCPAddrs returns TCP source/destination addresses if the header is stream-based.
func (header *Header) TCPAddrs() (sourceAddr, destAddr *net.TCPAddr, ok bool) {
	if !header.TransportProtocol.IsStream() {
		return nil, nil, false
	}
	sourceAddr, sourceOK := header.SourceAddr.(*net.TCPAddr)
	destAddr, destOK := header.DestinationAddr.(*net.TCPAddr)
	return sourceAddr, destAddr, sourceOK && destOK
}

// UDPAddrs returns UDP source/destination addresses if the header is datagram-based.
func (header *Header) UDPAddrs() (sourceAddr, destAddr *net.UDPAddr, ok bool) {
	if !header.TransportProtocol.IsDatagram() {
		return nil, nil, false
	}
	sourceAddr, sourceOK := header.SourceAddr.(*net.UDPAddr)
	destAddr, destOK := header.DestinationAddr.(*net.UDPAddr)
	return sourceAddr, destAddr, sourceOK && destOK
}

// UnixAddrs returns UNIX source/destination addresses if the header is UNIX-based.
func (header *Header) UnixAddrs() (sourceAddr, destAddr *net.UnixAddr, ok bool) {
	if !header.TransportProtocol.IsUnix() {
		return nil, nil, false
	}
	sourceAddr, sourceOK := header.SourceAddr.(*net.UnixAddr)
	destAddr, destOK := header.DestinationAddr.(*net.UnixAddr)
	return sourceAddr, destAddr, sourceOK && destOK
}

// IPs returns source/destination IPs for TCP/UDP headers.
func (header *Header) IPs() (sourceIP, destIP net.IP, ok bool) {
	if sourceAddr, destAddr, ok := header.TCPAddrs(); ok {
		return sourceAddr.IP, destAddr.IP, true
	}
	if sourceAddr, destAddr, ok := header.UDPAddrs(); ok {
		return sourceAddr.IP, destAddr.IP, true
	}
	return nil, nil, false
}

// Ports returns source/destination ports for TCP/UDP headers.
func (header *Header) Ports() (sourcePort, destPort int, ok bool) {
	if sourceAddr, destAddr, ok := header.TCPAddrs(); ok {
		return sourceAddr.Port, destAddr.Port, true
	}
	if sourceAddr, destAddr, ok := header.UDPAddrs(); ok {
		return sourceAddr.Port, destAddr.Port, true
	}
	return 0, 0, false
}

// EqualTo returns true if headers are equivalent, false otherwise.
// Deprecated: use EqualsTo instead. This method will eventually be removed.
func (header *Header) EqualTo(otherHeader *Header) bool {
	return header.EqualsTo(otherHeader)
}

// EqualsTo returns true if headers are equivalent, false otherwise.
func (header *Header) EqualsTo(otherHeader *Header) bool {
	if otherHeader == nil {
		return false
	}
	if header.Version != otherHeader.Version || header.Command != otherHeader.Command || header.TransportProtocol != otherHeader.TransportProtocol {
		return false
	}
	// TLVs only exist for version 2
	if header.Version == 2 && !bytes.Equal(header.rawTLVs, otherHeader.rawTLVs) {
		return false
	}
	// Return early for header with LOCAL command, which contains no address information
	if header.Command == LOCAL {
		return true
	}
	return header.SourceAddr.String() == otherHeader.SourceAddr.String() &&
		header.DestinationAddr.String() == otherHeader.DestinationAddr.String()
}

// WriteTo renders a proxy protocol header in a format and writes it to an io.Writer.
func (header *Header) WriteTo(w io.Writer) (int64, error) {
	buf, err := header.Format()
	if err != nil {
		return 0, err
	}

	return bytes.NewBuffer(buf).WriteTo(w)
}

// Format renders a proxy protocol header in a format to write over the wire.
func (header *Header) Format() ([]byte, error) {
	switch header.Version {
	case 1:
		return header.formatVersion1()
	case 2:
		return header.formatVersion2()
	default:
		return nil, ErrUnknownProxyProtocolVersion
	}
}

// TLVs returns the TLVs stored into this header, if they exist.  TLVs are optional for v2 of the protocol.
func (header *Header) TLVs() ([]TLV, error) {
	return SplitTLVs(header.rawTLVs)
}

// SetTLVs sets the TLVs stored in this header. This method replaces any
// previous TLV.
func (header *Header) SetTLVs(tlvs []TLV) error {
	raw, err := JoinTLVs(tlvs)
	if err != nil {
		return err
	}
	header.rawTLVs = raw
	return nil
}

// Read identifies the proxy protocol version and reads the remaining of
// the header, accordingly.
//
// If proxy protocol header signature is not present, the reader buffer remains untouched
// and is safe for reading outside of this code.
//
// If proxy protocol header signature is present but an error is raised while processing
// the remaining header, assume the reader buffer to be in a corrupt state.
// Also, this operation will block until enough bytes are available for peeking.
func Read(reader *bufio.Reader) (*Header, error) {
	// In order to improve speed for small non-PROXYed packets, take a peek at the first byte alone.
	b1, err := reader.Peek(1)
	if err != nil {
		if err == io.EOF {
			return nil, ErrNoProxyProtocol
		}
		return nil, err
	}

	if bytes.Equal(b1[:1], SIGV1[:1]) || bytes.Equal(b1[:1], SIGV2[:1]) {
		signature, err := reader.Peek(5)
		if err != nil {
			if err == io.EOF {
				return nil, ErrNoProxyProtocol
			}
			return nil, err
		}
		if bytes.Equal(signature[:5], SIGV1) {
			return parseVersion1(reader)
		}

		signature, err = reader.Peek(12)
		if err != nil {
			if err == io.EOF {
				return nil, ErrNoProxyProtocol
			}
			return nil, err
		}
		if bytes.Equal(signature[:12], SIGV2) {
			return parseVersion2(reader)
		}
	}

	return nil, ErrNoProxyProtocol
}

// ReadTimeout acts as Read but takes a timeout. If that timeout is reached, it's assumed
// there's no proxy protocol header.
func ReadTimeout(reader *bufio.Reader, timeout time.Duration) (*Header, error) {
	type header struct {
		h *Header
		e error
	}
	read := make(chan *header, 1)

	go func() {
		h := &header{}
		h.h, h.e = Read(reader)
		read <- h
	}()

	timer := time.NewTimer(timeout)
	select {
	case result := <-read:
		timer.Stop()
		return result.h, result.e
	case <-timer.C:
		return nil, ErrNoProxyProtocol
	}
}
