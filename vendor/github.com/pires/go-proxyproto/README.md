# go-proxyproto

[![Actions Status](https://github.com/pires/go-proxyproto/workflows/test/badge.svg)](https://github.com/pires/go-proxyproto/actions)
[![Coverage Status](https://coveralls.io/repos/github/pires/go-proxyproto/badge.svg?branch=main)](https://coveralls.io/github/pires/go-proxyproto?branch=main)
[![Go Report Card](https://goreportcard.com/badge/github.com/pires/go-proxyproto)](https://goreportcard.com/report/github.com/pires/go-proxyproto)
[![Go Reference](https://pkg.go.dev/badge/github.com/pires/go-proxyproto.svg)](https://pkg.go.dev/github.com/pires/go-proxyproto)


A Go library implementation of the
[HAProxy PROXY protocol specification 3.4](https://www.haproxy.org/download/3.4/doc/proxy-protocol.txt).
It covers protocol versions 1 (text) and 2 (binary), carrying the original
client connection information across NAT and proxy layers.

Use it on either side of a PROXY-aware hop: send headers with `Header.WriteTo`
or `Header.FormatUDPDatagram`, and receive them with `Listener`, `NewConn`,
`Read`, or `ParseUDPDatagram`. The stream APIs are for TCP and Unix streams;
UDP uses packet helpers because the spec requires a header in every datagram.

## Installation

```shell
$ go get github.com/pires/go-proxyproto
```

## Examples

The fastest way to get started is the runnable programs under
[`examples/`](examples) and the API examples on
[pkg.go.dev](https://pkg.go.dev/github.com/pires/go-proxyproto#pkg-examples):

| Goal | Where to look |
| ---- | ------------- |
| Minimal client | [`examples/client`](examples/client) |
| Minimal server | [`examples/server`](examples/server) |
| HTTP server | [`examples/httpserver`](examples/httpserver) |
| Server + client over TLS (PROXY header before TLS) | [`examples/tlsserver`](examples/tlsserver), [`examples/tlsclient`](examples/tlsclient) |
| UDP receiver and sender | [`examples/udpserver`](examples/udpserver), [`examples/udpclient`](examples/udpclient) |
| UDP `net.PacketConn` wrapper pattern | [`examples/udppacketconn`](examples/udppacketconn) |
| `Listener`, `NewConn`, `Read`, UDP, and TLS API examples | [Package examples](https://pkg.go.dev/github.com/pires/go-proxyproto#pkg-examples) |

## Usage

Use the runnable examples above for complete programs. The core API shape is
small.

### Client side

```go
header := proxyproto.HeaderProxyFromAddrs(1, sourceAddr, destinationAddr)
_, err := header.WriteTo(conn) // write the PROXY header before application data
```

See [`examples/client`](examples/client) for a complete TCP client.

### Server side

```go
proxyListener := &proxyproto.Listener{Listener: ln}
conn, err := proxyListener.Accept()
// Connections must open with a PROXY header (the default policy is REQUIRE);
// conn.RemoteAddr() then reports the client address from that header.
```

See [`examples/server`](examples/server) for a complete TCP server. For HTTP/1
and HTTP/2, see [`examples/httpserver`](examples/httpserver), which uses
[`helper/http2`](helper/http2) so one server can accept proxied HTTP/1 and HTTP/2
connections.

> [!WARNING]
> The zero-value configuration requires the PROXY header but still honors it
> from any peer. It is not safe for listeners reachable by untrusted clients
> without a trusted-source policy. See [Security](#security).

## Security

The PROXY header replaces what your application sees as the client address, so
whoever is allowed to send one can spoof their origin. The spec (section 2)
says receivers "MUST not try to guess" whether the header is present, and
requires access filtering so only trusted proxies can use the protocol.

Important stream defaults and policies:

- With no policy configured, `Listener` and `NewConn` use
  `proxyproto.DefaultPolicy`, which is `REQUIRE`. A connection that does not
  open with a PROXY header fails its first I/O with `ErrNoProxyProtocol`, so
  header presence is never guessed.
- `REQUIRE` still honors headers from any peer. Use it only when the listener
  is reachable exclusively by trusted proxies, such as a private network
  segment behind your load balancer.
- Deployments that need historical optional-header behavior can restore it
  process-wide with `proxyproto.DefaultPolicy = proxyproto.USE`, per
  connection with `WithPolicy(USE)`, or per listener with a policy returning
  `USE`.
- For exposed listeners, restrict the senders with `TrustProxyHeaderFrom` or
  `TrustProxyHeaderFromRanges`. Trusted peers must send a header; untrusted
  peers are dropped by `Accept`. For example:

```go
proxyListener := &proxyproto.Listener{
	Listener: ln,
	// Connections from the load balancer must open with a PROXY header
	// (REQUIRE); connections from any other source are dropped. For CIDR
	// ranges, use TrustProxyHeaderFromRanges([]string{"10.0.0.0/24"}).
	// For mixed traffic (e.g. optional headers from some sources), spell the
	// two policies out with PolicyFromRanges(ranges, matched, unmatched).
	ConnPolicy: proxyproto.TrustProxyHeaderFrom(net.ParseIP("10.0.0.10")),
}
```

For UDP, `ParseUDPDatagram` has no built-in trusted-source policy because it
only sees packet bytes. Check the `net.PacketConn.ReadFrom` sender address
yourself before trusting `header.SourceAddr`.

Related knobs are documented in the
[package docs](https://pkg.go.dev/github.com/pires/go-proxyproto):
`ReadHeaderTimeout` (default 10s), `MaxV2HeaderSize` (default 4KiB),
`V1AcceptIPv4InTCP6` (default off), and `Listener.ValidateHeader`.

## UDP

The spec requires the header and proxied payload in the same UDP datagram, and
the receiver must parse the header independently for every datagram. `Listener`
and `Conn` cannot provide those semantics; use `ParseUDPDatagram` and
`Header.FormatUDPDatagram` with your own `net.PacketConn`. Headers with
`UDPv4`/`UDPv6` families carried over stream connections describe the proxied
protocol and remain fully supported.

- [`examples/udpserver`](examples/udpserver) and
  [`examples/udpclient`](examples/udpclient) show direct per-datagram parsing
  and formatting.
- [`examples/udppacketconn`](examples/udppacketconn) sketches a
  `net.PacketConn` wrapper with reply routing through the relaying proxy.
- [`ExampleParseUDPDatagram`](https://pkg.go.dev/github.com/pires/go-proxyproto#example-ParseUDPDatagram)
  and
  [`ExampleHeader_FormatUDPDatagram`](https://pkg.go.dev/github.com/pires/go-proxyproto#example-Header.FormatUDPDatagram)
  show the same APIs as package examples.

The library deliberately does not export a `net.PacketConn` wrapper. A wrapper
that returns the client address from the header also needs an application-owned
client-to-proxy flow table for replies, including bounds, expiry, and spoofing
policy.

### TLS

When combining PROXY protocol with TLS, match the wrapper order to the upstream
order. If the header is sent in cleartext before the handshake, put proxyproto
inside TLS: `tls.NewListener(&proxyproto.Listener{Listener: l}, tlsConfig)`.
If the header is sent inside the TLS session, decrypt first:
`&proxyproto.Listener{Listener: tls.NewListener(l, tlsConfig)}`. In both cases
`conn.RemoteAddr()` reports the client carried by the PROXY header.

Runnable code lives in [`examples/tlsserver`](examples/tlsserver) and
[`examples/tlsclient`](examples/tlsclient). Package examples show both
orderings.

## Special notes

### AWS

AWS Network Load Balancer (NLB) does not send the PROXY v2 header until the
client sends payload: the target group attribute
`proxy_protocol_v2.client_to_server.header_placement` defaults to
`on_first_ack_with_payload`. Server-first protocols such as SMTP, FTP, and SSH
fail in that mode; contact AWS support to change the attribute to
`on_first_ack` so the header arrives before the backend speaks.
