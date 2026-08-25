package wire

import (
	"fmt"
	"net"
	"net/netip"
)

func addrPortStrToAddr(addrPortStr string) (*net.TCPAddr, error) {
	addrPort, err := netip.ParseAddrPort(addrPortStr)
	if err != nil {
		return nil, fmt.Errorf("parse addr port from %s: %w", addrPortStr, err)
	}
	return net.TCPAddrFromAddrPort(addrPort), nil
}
