package gocql

import (
	"net"
	"testing"
)

func TestHostInfo_Lookup(t *testing.T) {
	hostLookupPreferV4 = true
	defer func() { hostLookupPreferV4 = false }()

	tests := [...]struct {
		addr string
		ip   net.IP
	}{
		{"127.0.0.1", net.IPv4(127, 0, 0, 1)},
		{"localhost", net.IPv4(127, 0, 0, 1)}, // TODO: this may be host dependant
	}

	for i, test := range tests {
		hosts, err := hostInfo(test.addr, 1)
		if err != nil {
			t.Errorf("%d: %v", i, err)
			continue
		}

		host := hosts[0]
		if !host.ConnectAddress().Equal(test.ip) {
			t.Errorf("expected ip %v got %v for addr %q", test.ip, host.ConnectAddress(), test.addr)
		}
	}
}

func TestParseProtocol(t *testing.T) {
	tests := [...]struct {
		err   error
		proto int
	}{
		{
			err: &protocolError{
				frame: errorFrame{
					code:    0x10,
					message: "Invalid or unsupported protocol version (5); the lowest supported version is 3 and the greatest is 4",
				},
			},
			proto: 4,
		},
		{
			err: &protocolError{
				frame: errorFrame{
					frameHeader: frameHeader{
						version: 0x83,
					},
					code:    0x10,
					message: "Invalid or unsupported protocol version: 5",
				},
			},
			proto: 3,
		},
	}

	for i, test := range tests {
		if proto := parseProtocolFromError(test.err); proto != test.proto {
			t.Errorf("%d: exepcted proto %d got %d", i, test.proto, proto)
		}
	}
}
