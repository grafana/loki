package proxyproto

import (
	"bufio"
	"bytes"
)

// ParseUDPDatagram parses a PROXY protocol header at the start of a UDP
// datagram and returns the header together with the proxied payload that
// follows it. The returned payload aliases the datagram slice.
//
// Per spec (section 2), when the PROXY protocol is carried over UDP the header
// and the proxied payload MUST be sent in the same datagram, and the receiver
// MUST parse the header independently for each received datagram. Call this
// for every datagram received; there is no connection state to carry over.
// Conn and Listener are stream-oriented and cannot provide those semantics.
//
// A datagram that does not begin with a complete, valid header fails with the
// same errors Read returns — ErrNoProxyProtocol when the signature is absent.
// The spec forbids guessing whether a header is present, so on a receiver
// configured for the PROXY protocol such datagrams must be dropped, not
// treated as raw payload.
func ParseUDPDatagram(datagram []byte) (*Header, []byte, error) {
	byteReader := bytes.NewReader(datagram)
	// Size the buffer to the whole datagram so the header is fully buffered up
	// front: v1 parsing aborts if the header is not available in a single read,
	// and a datagram, unlike a stream, can never deliver more bytes later.
	// bufio enforces a minimum buffer size internally, covering len == 0.
	reader := bufio.NewReaderSize(byteReader, len(datagram))
	header, err := Read(reader)
	if err != nil {
		return nil, nil, err
	}
	consumed := len(datagram) - reader.Buffered() - byteReader.Len()
	return header, datagram[consumed:], nil
}

// FormatUDPDatagram renders the header followed by the proxied payload,
// producing the exact bytes to send as one UDP datagram. Per spec (section 2)
// the header and the payload MUST share a single datagram, which a single
// write of the returned slice guarantees; formatting the header and payload
// separately risks a sender splitting them across datagrams.
func (header *Header) FormatUDPDatagram(payload []byte) ([]byte, error) {
	buf, err := header.Format()
	if err != nil {
		return nil, err
	}
	return append(buf, payload...), nil
}
