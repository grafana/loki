// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package confignet // import "go.opentelemetry.io/collector/config/confignet"

import (
	"net"
)

// NetAddr represents a network endpoint address.
type NetAddr struct {
	// Endpoint configures the address for this network connection.
	// For TCP and UDP networks, the address has the form "host:port". The host must be a literal IP address,
	// or a host name that can be resolved to IP addresses. The port must be a literal port number or a service name.
	// If the host is a literal IPv6 address it must be enclosed in square brackets, as in "[2001:db8::1]:80" or
	// "[fe80::1%zone]:80". The zone specifies the scope of the literal IPv6 address as defined in RFC 4007.
	Endpoint string `mapstructure:"endpoint"`

	// Transport to use. Known protocols are "tcp", "tcp4" (IPv4-only), "tcp6" (IPv6-only), "udp", "udp4" (IPv4-only),
	// "udp6" (IPv6-only), "ip", "ip4" (IPv4-only), "ip6" (IPv6-only), "unix", "unixgram" and "unixpacket".
	Transport string `mapstructure:"transport"`
}

// Dial equivalent with net.Dial for this address.
func (na *NetAddr) Dial() (net.Conn, error) {
	return net.Dial(na.Transport, na.Endpoint)
}

// Listen equivalent with net.Listen for this address.
func (na *NetAddr) Listen() (net.Listener, error) {
	return net.Listen(na.Transport, na.Endpoint)
}

// TCPAddr represents a TCP endpoint address.
type TCPAddr struct {
	// Endpoint configures the address for this network connection.
	// The address has the form "host:port". The host must be a literal IP address, or a host name that can be
	// resolved to IP addresses. The port must be a literal port number or a service name.
	// If the host is a literal IPv6 address it must be enclosed in square brackets, as in "[2001:db8::1]:80" or
	// "[fe80::1%zone]:80". The zone specifies the scope of the literal IPv6 address as defined in RFC 4007.
	Endpoint string `mapstructure:"endpoint"`
}

// Dial equivalent with net.Dial for this address.
func (na *TCPAddr) Dial() (net.Conn, error) {
	return net.Dial("tcp", na.Endpoint)
}

// Listen equivalent with net.Listen for this address.
func (na *TCPAddr) Listen() (net.Listener, error) {
	return net.Listen("tcp", na.Endpoint)
}
