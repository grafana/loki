# Network Configuration Settings

[Receivers](https://github.com/open-telemetry/opentelemetry-collector/blob/main/receiver/README.md)
leverage network configuration to set connection and transport information.

- `endpoint`: Configures the address for this network connection. For TCP and
  UDP networks, the address has the form "host:port". The host must be a
  literal IP address, or a host name that can be resolved to IP addresses. The
  port must be a literal port number or a service name. If the host is a
  literal IPv6 address it must be enclosed in square brackets, as in
  "[2001:db8::1]:80" or "[fe80::1%zone]:80". The zone specifies the scope of
  the literal IPv6 address as defined in RFC 4007.
- `transport`: Known protocols are "tcp", "tcp4" (IPv4-only), "tcp6"
  (IPv6-only), "udp", "udp4" (IPv4-only), "udp6" (IPv6-only), "ip", "ip4"
  (IPv4-only), "ip6" (IPv6-only), "unix", "unixgram" and "unixpacket".

Note that for TCP receivers only the `endpoint` configuration setting is
required.
