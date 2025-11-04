package wire

import (
	"errors"
	"net"
)

type tcpAddr struct {
	Addr string
}

func (a *tcpAddr) Network() string { return "tcp" }
func (a *tcpAddr) String() string  { return a.Addr }

func newTCPAddrFromString(s string) (net.Addr, error) {
	if s == "" {
		return nil, errors.New("empty address")
	}
	return &tcpAddr{Addr: s}, nil
}

func mustNewTCPAddrFromString(s string) net.Addr {
	addr, err := newTCPAddrFromString(s)
	if err != nil {
		panic(err)
	}
	return addr
}
