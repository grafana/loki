package proxyproto

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
)

// MaxV2HeaderSize is the maximum accepted value of a v2 header's 16-bit length
// field, i.e. the number of bytes following the fixed 16-byte prefix (address
// block plus TLVs).
//
// The spec allows up to 65535, but parseVersion2 allocates this many bytes
// before reading, so a lower limit mitigates memory-allocation DoS from
// untrusted peers while allowing real-world legitimate headers.
// PP2_SUBTYPE_SSL_CLIENT_CERT (a DER-encoded certificate) is typically between
// 1 and 2KiB, so the 4KiB default leaves room for other TLVs; deployments
// expecting larger headers may raise it.
//
// Like DefaultReadHeaderTimeout, this is a package-level variable to keep it
// easy to override. Set it at program init; it must not be modified
// concurrently with parsing.
var MaxV2HeaderSize uint16 = 4096

var (
	lengthUnspec      = uint16(0)
	lengthV4          = uint16(12)
	lengthV6          = uint16(36)
	lengthUnix        = uint16(216)
	lengthUnspecBytes = func() []byte {
		a := make([]byte, 2)
		binary.BigEndian.PutUint16(a, lengthUnspec)
		return a
	}()
	lengthV4Bytes = func() []byte {
		a := make([]byte, 2)
		binary.BigEndian.PutUint16(a, lengthV4)
		return a
	}()
	lengthV6Bytes = func() []byte {
		a := make([]byte, 2)
		binary.BigEndian.PutUint16(a, lengthV6)
		return a
	}()
	lengthUnixBytes = func() []byte {
		a := make([]byte, 2)
		binary.BigEndian.PutUint16(a, lengthUnix)
		return a
	}()
	errUint16Overflow = errors.New("proxyproto: uint16 overflow")
)

type _ports struct {
	SrcPort uint16
	DstPort uint16
}

type _addr4 struct {
	Src     [4]byte
	Dst     [4]byte
	SrcPort uint16
	DstPort uint16
}

type _addr6 struct {
	Src [16]byte
	Dst [16]byte
	_ports
}

type _addrUnix struct {
	Src [108]byte
	Dst [108]byte
}

func parseVersion2(reader *bufio.Reader) (header *Header, err error) {
	// Skip first 12 bytes (signature)
	for range 12 {
		if _, err = reader.ReadByte(); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrCantReadProtocolVersionAndCommand, err)
		}
	}

	header = new(Header)
	header.Version = 2

	// Read the 13th byte, protocol version and command
	b13, err := reader.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCantReadProtocolVersionAndCommand, err)
	}
	header.Command = ProtocolVersionAndCommand(b13)
	if _, ok := supportedCommand[header.Command]; !ok {
		return nil, ErrUnsupportedProtocolVersionAndCommand
	}

	// Read the 14th byte, address family and protocol
	b14, err := reader.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCantReadAddressFamilyAndProtocol, err)
	}
	header.TransportProtocol = AddressFamilyAndProtocol(b14)
	// Per spec (section 2.2) only the listed address family / transport protocol
	// combinations are defined; "other values are unspecified and must not be
	// emitted in version 2 of this protocol and must be rejected as invalid by
	// receivers". That mandate has no command carve-out, so it applies to LOCAL
	// too: the family byte is ignored when interpreting a LOCAL address block,
	// but an undefined byte still makes the frame invalid. Rejecting bytes that
	// share a known family with an undefined transport — e.g. 0x13 (IPv4
	// family, transport 3) — also matters for PROXY: such a byte passes the
	// IsIPv4/IsIPv6 checks below, has an address struct read for it, and then
	// yields a nil net.Addr from newIPAddr. That nil is stored as
	// SourceAddr/DestinationAddr and panics any caller that does
	// RemoteAddr().String() (e.g. logging), a remotely triggerable crash.
	if !supportedTransportProtocol[header.TransportProtocol] {
		return nil, ErrUnsupportedAddressFamilyAndProtocol
	}
	// UNSPEC carries no address block to trust. For LOCAL it is the family the
	// spec expects senders to use; for PROXY the spec leaves it to the receiver
	// to accept or reject, and this library rejects it.
	if header.TransportProtocol == UNSPEC && header.Command != LOCAL {
		return nil, ErrUnsupportedAddressFamilyAndProtocol
	}

	// Make sure there are bytes available as specified in length
	var length uint16
	if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCantReadLength, err)
	}

	if !header.validateLength(length) {
		// A LOCAL connection is one the proxy opened itself (e.g. health
		// checks). Per spec (section 2.2) the receiver must use the real
		// connection endpoints (Conn.RemoteAddr/LocalAddr short-circuit on
		// IsLocal), must skip exactly `length` bytes, and "must not assume zero
		// is presented for LOCAL connections". So a LOCAL frame whose length
		// does not fit its declared family's address-block layout — e.g.
		// LOCAL + TCPv4 + length 0 — is still valid: skip the block, which the
		// spec says to discard, and normalize the header to UNSPEC so it stays
		// serializable via Format/WriteTo. A LOCAL frame whose length does fit
		// the family layout is instead decoded exactly like a PROXY frame
		// below, preserving the (informational) addresses and any trailing
		// TLVs, so the header round-trips byte for byte.
		if header.Command == LOCAL {
			if length > MaxV2HeaderSize {
				return nil, ErrInvalidLength
			}
			if _, err := reader.Discard(int(length)); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidLength, err)
			}
			header.TransportProtocol = UNSPEC
			return header, nil
		}
		return nil, ErrInvalidLength
	}

	// Return early if the length is zero, which means that
	// there's no address information and TLVs present for UNSPEC.
	if length == 0 {
		return header, nil
	}

	if length > MaxV2HeaderSize {
		return nil, ErrInvalidLength
	}

	// Length-limited reader for payload section
	payloadReader := io.LimitReader(reader, int64(length)).(*io.LimitedReader)

	// Read addresses and ports for protocols other than UNSPEC.
	// Ignore address information for UNSPEC, and skip straight to read TLVs,
	// since the length is greater than zero.
	if header.TransportProtocol != UNSPEC {
		if header.TransportProtocol.IsIPv4() {
			var addr _addr4
			if err := binary.Read(payloadReader, binary.BigEndian, &addr); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidAddress, err)
			}
			header.SourceAddr = newIPAddr(header.TransportProtocol, addr.Src[:], addr.SrcPort)
			header.DestinationAddr = newIPAddr(header.TransportProtocol, addr.Dst[:], addr.DstPort)
		} else if header.TransportProtocol.IsIPv6() {
			var addr _addr6
			if err := binary.Read(payloadReader, binary.BigEndian, &addr); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidAddress, err)
			}
			header.SourceAddr = newIPAddr(header.TransportProtocol, addr.Src[:], addr.SrcPort)
			header.DestinationAddr = newIPAddr(header.TransportProtocol, addr.Dst[:], addr.DstPort)
		} else if header.TransportProtocol.IsUnix() {
			var addr _addrUnix
			if err := binary.Read(payloadReader, binary.BigEndian, &addr); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidAddress, err)
			}

			network := networkUnix
			if header.TransportProtocol.IsDatagram() {
				network = networkUnixgram
			}

			header.SourceAddr = &net.UnixAddr{
				Net:  network,
				Name: parseUnixName(addr.Src[:]),
			}
			header.DestinationAddr = &net.UnixAddr{
				Net:  network,
				Name: parseUnixName(addr.Dst[:]),
			}
		}
	}

	// Copy bytes for optional Type-Length-Value vector
	header.rawTLVs = make([]byte, payloadReader.N) // Allocate minimum size slice
	if _, err = io.ReadFull(payloadReader, header.rawTLVs); err != nil && err != io.EOF {
		return nil, err
	}

	if payloadReader.N != 0 {
		return nil, ErrInvalidLength
	}

	return header, nil
}

func (header *Header) formatVersion2() ([]byte, error) {
	var buf bytes.Buffer
	buf.Write(SIGV2)
	buf.WriteByte(header.Command.toByte())
	buf.WriteByte(header.TransportProtocol.toByte())
	if header.TransportProtocol.IsUnspec() {
		// For UNSPEC, write no addresses and ports but only TLVs if they are present
		hdrLen, err := addTLVLen(lengthUnspecBytes, len(header.rawTLVs))
		if err != nil {
			return nil, err
		}
		buf.Write(hdrLen)
	} else {
		var addrSrc, addrDst []byte
		if header.TransportProtocol.IsIPv4() {
			hdrLen, err := addTLVLen(lengthV4Bytes, len(header.rawTLVs))
			if err != nil {
				return nil, err
			}
			buf.Write(hdrLen)
			sourceIP, destIP, _ := header.IPs()
			addrSrc = sourceIP.To4()
			addrDst = destIP.To4()
		} else if header.TransportProtocol.IsIPv6() {
			hdrLen, err := addTLVLen(lengthV6Bytes, len(header.rawTLVs))
			if err != nil {
				return nil, err
			}
			buf.Write(hdrLen)
			sourceIP, destIP, _ := header.IPs()
			addrSrc = sourceIP.To16()
			addrDst = destIP.To16()
		} else if header.TransportProtocol.IsUnix() {
			hdrLen, err := addTLVLen(lengthUnixBytes, len(header.rawTLVs))
			if err != nil {
				return nil, err
			}
			buf.Write(hdrLen)
			sourceAddr, destAddr, ok := header.UnixAddrs()
			if !ok {
				return nil, ErrInvalidAddress
			}
			addrSrc = formatUnixName(sourceAddr.Name)
			addrDst = formatUnixName(destAddr.Name)
		}

		if addrSrc == nil || addrDst == nil {
			return nil, ErrInvalidAddress
		}
		buf.Write(addrSrc)
		buf.Write(addrDst)

		if sourcePort, destPort, ok := header.Ports(); ok {
			if sourcePort < 0 || sourcePort > math.MaxUint16 || destPort < 0 || destPort > math.MaxUint16 {
				return nil, ErrInvalidPortNumber
			}
			portBytes := make([]byte, 2)

			//nolint:gosec // Bounds are checked above.
			binary.BigEndian.PutUint16(portBytes, uint16(sourcePort))
			buf.Write(portBytes)

			//nolint:gosec // Bounds are checked above.
			binary.BigEndian.PutUint16(portBytes, uint16(destPort))
			buf.Write(portBytes)
		}
	}

	if len(header.rawTLVs) > 0 {
		buf.Write(header.rawTLVs)
	}

	return buf.Bytes(), nil
}

func (header *Header) validateLength(length uint16) bool {
	if header.TransportProtocol.IsIPv4() {
		return length >= lengthV4
	} else if header.TransportProtocol.IsIPv6() {
		return length >= lengthV6
	} else if header.TransportProtocol.IsUnix() {
		return length >= lengthUnix
	} else if header.TransportProtocol.IsUnspec() {
		return length >= lengthUnspec
	}
	return false
}

// addTLVLen adds the length of the TLV to the header length or errors on uint16 overflow.
func addTLVLen(cur []byte, tlvLen int) ([]byte, error) {
	if tlvLen == 0 {
		return cur, nil
	}
	curLen := binary.BigEndian.Uint16(cur)
	newLen := int(curLen) + tlvLen
	if newLen >= 1<<16 {
		return nil, errUint16Overflow
	}
	a := make([]byte, 2)
	//nolint:gosec // newLen bounds are validated above.
	binary.BigEndian.PutUint16(a, uint16(newLen))
	return a, nil
}

func newIPAddr(transport AddressFamilyAndProtocol, ip net.IP, port uint16) net.Addr {
	if transport.IsStream() {
		return &net.TCPAddr{IP: ip, Port: int(port)}
	}
	if transport.IsDatagram() {
		return &net.UDPAddr{IP: ip, Port: int(port)}
	}
	return nil
}

func parseUnixName(b []byte) string {
	before, _, ok := bytes.Cut(b, []byte{0})
	if !ok {
		return string(b)
	}
	return string(before)
}

func formatUnixName(name string) []byte {
	n := int(lengthUnix) / 2
	if len(name) >= n {
		return []byte(name[:n])
	}
	pad := make([]byte, n-len(name))
	return append([]byte(name), pad...)
}
