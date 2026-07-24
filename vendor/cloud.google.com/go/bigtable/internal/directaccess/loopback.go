// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package directaccess

import (
	"fmt"
	"net"

	btopt "cloud.google.com/go/bigtable/internal/option"
)

// CheckLoopbackInterfaceUp verifies that at least one loopback interface is UP.
func CheckLoopbackInterfaceUp() error {
	btopt.Debugf(nil, "directaccess: Checking for UP loopback interfaces")
	ifaces, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("failed to list network interfaces: %w", err)
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 && iface.Flags&net.FlagUp != 0 {
			return nil
		}
	}
	return fmt.Errorf("no loopback interface found in UP state")
}

func skipLoopback(iface net.Interface) error {
	if iface.Flags&net.FlagLoopback != 0 {
		return fmt.Errorf("is loopback")
	}
	if iface.Flags&net.FlagUp != net.FlagUp {
		return fmt.Errorf("not up")
	}
	return nil
}

func onlyLoopback(iface net.Interface) error {
	if iface.Flags&net.FlagLoopback == 0 {
		return fmt.Errorf("not loopback")
	}
	if iface.Flags&net.FlagUp != net.FlagUp {
		return fmt.Errorf("not up")
	}
	return nil
}

// CheckLocalIPv6Addresses searches the host's non-loopback network interfaces
// to verify if the specified IPv6 address is currently plumbed and active.
// It returns the interface where the address is configured, or an error if not
func CheckLocalIPv6Addresses(ip *net.IP) (*net.Interface, error) {
	btopt.Debugf(nil, "directaccess: Searching for local IPv6 address: %s", ip.String())
	return findLocalAddress(func(i net.IP) bool { return i.To4() == nil && i.Equal(*ip) }, skipLoopback)
}

// CheckLocalIPv4Addresses searches the host's non-loopback network interfaces
// to verify if the specified IPv4 address is currently plumbed and active.
// It returns the interface where the address is configured, or an error if not found.
func CheckLocalIPv4Addresses(ip *net.IP) (*net.Interface, error) {
	btopt.Debugf(nil, "directaccess: Searching for local IPv4 address: %s", ip.String())
	return findLocalAddress(func(i net.IP) bool { return i.To4() != nil && i.Equal(*ip) }, skipLoopback)
}

// CheckLocalIPv6LoopbackAddress verifies that the standard IPv6 loopback
// address (::1) is configured and assigned to a local loopback interface.
func CheckLocalIPv6LoopbackAddress() error {
	btopt.Debugf(nil, "directaccess: Searching for IPv6 loopback (::1)")
	_, err := findLocalAddress(func(i net.IP) bool { return i.Equal(net.ParseIP("::1")) }, onlyLoopback)
	return err
}

// CheckLocalIPv4LoopbackAddress verifies that the standard IPv4 loopback
// address (127.0.0.1) is configured and assigned to a local loopback interface.
func CheckLocalIPv4LoopbackAddress() error {
	btopt.Debugf(nil, "directaccess: Searching for IPv4 loopback (127.0.0.1)")
	_, err := findLocalAddress(func(i net.IP) bool { return i.Equal(net.ParseIP("127.0.0.1")) }, onlyLoopback)
	return err
}

// findLocalAddress is a generic helper that iterates through all local network interfaces.
// It applies the ifaceFilter to skip unwanted interfaces (e.g., ignoring loopbacks
// or inactive NICs) and checks if any assigned IP satisfies the ipMatches condition.
// It returns the first matching network interface, or an error if exhausted.
func findLocalAddress(ipMatches func(net.IP) bool, ifaceFilter func(net.Interface) error) (*net.Interface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, iface := range ifaces {
		if err := ifaceFilter(iface); err != nil {
			continue
		}
		ifaddrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, ifaddr := range ifaddrs {
			if ip, ok := ifaddr.(*net.IPNet); ok && ipMatches(ip.IP) {
				return &iface, nil
			}
		}
	}
	btopt.Debugf(nil, "directaccess: Target address not found on any valid interface")
	return nil, fmt.Errorf("address not found on any interface")
}
