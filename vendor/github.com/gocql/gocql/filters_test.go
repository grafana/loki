package gocql

import (
	"net"
	"testing"
)

func TestFilter_WhiteList(t *testing.T) {
	f := WhiteListHostFilter("127.0.0.1", "127.0.0.2")
	tests := [...]struct {
		addr   net.IP
		accept bool
	}{
		{net.ParseIP("127.0.0.1"), true},
		{net.ParseIP("127.0.0.2"), true},
		{net.ParseIP("127.0.0.3"), false},
	}

	for i, test := range tests {
		if f.Accept(&HostInfo{connectAddress: test.addr}) {
			if !test.accept {
				t.Errorf("%d: should not have been accepted but was", i)
			}
		} else if test.accept {
			t.Errorf("%d: should have been accepted but wasn't", i)
		}
	}
}

func TestFilter_AllowAll(t *testing.T) {
	f := AcceptAllFilter()
	tests := [...]struct {
		addr   net.IP
		accept bool
	}{
		{net.ParseIP("127.0.0.1"), true},
		{net.ParseIP("127.0.0.2"), true},
		{net.ParseIP("127.0.0.3"), true},
	}

	for i, test := range tests {
		if f.Accept(&HostInfo{connectAddress: test.addr}) {
			if !test.accept {
				t.Errorf("%d: should not have been accepted but was", i)
			}
		} else if test.accept {
			t.Errorf("%d: should have been accepted but wasn't", i)
		}
	}
}

func TestFilter_DenyAll(t *testing.T) {
	f := DenyAllFilter()
	tests := [...]struct {
		addr   net.IP
		accept bool
	}{
		{net.ParseIP("127.0.0.1"), false},
		{net.ParseIP("127.0.0.2"), false},
		{net.ParseIP("127.0.0.3"), false},
	}

	for i, test := range tests {
		if f.Accept(&HostInfo{connectAddress: test.addr}) {
			if !test.accept {
				t.Errorf("%d: should not have been accepted but was", i)
			}
		} else if test.accept {
			t.Errorf("%d: should have been accepted but wasn't", i)
		}
	}
}

func TestFilter_DataCentre(t *testing.T) {
	f := DataCentreHostFilter("dc1")
	tests := [...]struct {
		dc     string
		accept bool
	}{
		{"dc1", true},
		{"dc2", false},
	}

	for i, test := range tests {
		if f.Accept(&HostInfo{dataCenter: test.dc}) {
			if !test.accept {
				t.Errorf("%d: should not have been accepted but was", i)
			}
		} else if test.accept {
			t.Errorf("%d: should have been accepted but wasn't", i)
		}
	}
}
