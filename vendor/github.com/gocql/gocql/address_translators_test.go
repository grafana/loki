package gocql

import (
	"net"
	"testing"
)

func TestIdentityAddressTranslator_NilAddrAndZeroPort(t *testing.T) {
	var tr AddressTranslator = IdentityTranslator()
	hostIP := net.ParseIP("")
	if hostIP != nil {
		t.Errorf("expected host ip to be (nil) but was (%+v) instead", hostIP)
	}

	addr, port := tr.Translate(hostIP, 0)
	if addr != nil {
		t.Errorf("expected translated host to be (nil) but was (%+v) instead", addr)
	}
	assertEqual(t, "translated port", 0, port)
}

func TestIdentityAddressTranslator_HostProvided(t *testing.T) {
	var tr AddressTranslator = IdentityTranslator()
	hostIP := net.ParseIP("10.1.2.3")
	if hostIP == nil {
		t.Error("expected host ip not to be (nil)")
	}

	addr, port := tr.Translate(hostIP, 9042)
	if !hostIP.Equal(addr) {
		t.Errorf("expected translated addr to be (%+v) but was (%+v) instead", hostIP, addr)
	}
	assertEqual(t, "translated port", 9042, port)
}
