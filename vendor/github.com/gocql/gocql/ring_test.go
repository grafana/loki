package gocql

import (
	"net"
	"testing"
)

func TestRing_AddHostIfMissing_Missing(t *testing.T) {
	ring := &ring{}

	host := &HostInfo{connectAddress: net.IPv4(1, 1, 1, 1)}
	h1, ok := ring.addHostIfMissing(host)
	if ok {
		t.Fatal("host was reported as already existing")
	} else if !h1.Equal(host) {
		t.Fatalf("hosts not equal that are returned %v != %v", h1, host)
	} else if h1 != host {
		t.Fatalf("returned host same pointer: %p != %p", h1, host)
	}
}

func TestRing_AddHostIfMissing_Existing(t *testing.T) {
	ring := &ring{}

	host := &HostInfo{connectAddress: net.IPv4(1, 1, 1, 1)}
	ring.addHostIfMissing(host)

	h2 := &HostInfo{connectAddress: net.IPv4(1, 1, 1, 1)}

	h1, ok := ring.addHostIfMissing(h2)
	if !ok {
		t.Fatal("host was not reported as already existing")
	} else if !h1.Equal(host) {
		t.Fatalf("hosts not equal that are returned %v != %v", h1, host)
	} else if h1 != host {
		t.Fatalf("returned host same pointer: %p != %p", h1, host)
	}
}
