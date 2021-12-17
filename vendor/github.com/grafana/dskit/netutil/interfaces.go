package netutil

import (
	"fmt"
	"net"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

func ipForAddr(addr net.Addr) (net.IP, bool) {
	switch a := addr.(type) {
	case *net.IPAddr:
		return a.IP, true
	case *net.IPNet:
		return a.IP, true
	default:
		return net.IPv4zero, false
	}
}

func PrivateNetworkInterfaces() []string {
	ifaces := []string{}

	all, err := net.Interfaces()
	if err != nil {
		return ifaces
	}

IFACES:
	for _, iface := range all {
		if iface.Flags&net.FlagLoopback == 0 && iface.Flags&net.FlagUp != 0 {
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				ip, ok := ipForAddr(addr)
				if !ok {
					continue
				}
				if !ip.IsPrivate() {
					continue IFACES
				}
			}
			ifaces = append(ifaces, iface.Name)
		}
	}
	return ifaces
}

// FirstAddressOf returns the first IPv4 address of the supplied interface
// names, omitting any 169.254.x.x automatic private IPs if possible.
func FirstAddressOf(names []string, logger log.Logger) (string, error) {
	var ipAddr net.IP
	for _, name := range names {
		inf, err := net.InterfaceByName(name)
		if err != nil {
			level.Warn(logger).Log("msg", "error getting interface", "inf", name, "err", err)
			continue
		}
		addrs, err := inf.Addrs()
		if err != nil {
			level.Warn(logger).Log("msg", "error getting addresses for interface", "inf", name, "err", err)
			continue
		}
		if len(addrs) <= 0 {
			level.Warn(logger).Log("msg", "no addresses found for interface", "inf", name, "err", err)
			continue
		}
		if ip := filterIPs(addrs); !ip.IsUnspecified() {
			ipAddr = ip
		}
		if isAPIPA(ipAddr) || ipAddr.IsUnspecified() {
			continue
		}
		return ipAddr.String(), nil
	}
	if ipAddr.IsUnspecified() {
		return "", fmt.Errorf("no address found for %s", names)
	}
	if isAPIPA(ipAddr) {
		level.Warn(logger).Log("msg", "using automatic private ip", "address", ipAddr)
	}
	return ipAddr.String(), nil
}

func isAPIPA(ip4 net.IP) bool {
	return ip4[0] == 169 && ip4[1] == 254
}

// filterIPs attempts to return the first non automatic private IP (APIPA /
// 169.254.x.x) if possible, only returning APIPA if available and no other
// valid IP is found.
func filterIPs(addrs []net.Addr) net.IP {
	ipAddr := net.IPv4zero
	for _, addr := range addrs {
		if ip, ok := ipForAddr(addr); ok {
			if ip4 := ip.To4(); ip4 != nil {
				ipAddr = ip4
				if isAPIPA(ip4) {
					return ipAddr
				}
			}
		}
	}
	return ipAddr
}
