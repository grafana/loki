# NATS - Go Client
A [Go](http://golang.org) client for the [NATS messaging system](https://nats.io).

[![License Apache 2][License-Image]][License-Url] [![Go Report Card][ReportCard-Image]][ReportCard-Url] [![Build Status][Build-Status-Image]][Build-Status-Url] [![GoDoc][GoDoc-Image]][GoDoc-Url] [![Coverage Status][Coverage-image]][Coverage-Url]

[License-Url]: https://www.apache.org/licenses/LICENSE-2.0
[License-Image]: https://img.shields.io/badge/License-Apache2-blue.svg
[ReportCard-Url]: https://goreportcard.com/report/github.com/nats-io/nats.go
[ReportCard-Image]: https://goreportcard.com/badge/github.com/nats-io/nats.go
[Build-Status-Url]: https://travis-ci.com/github/nats-io/nats.go
[Build-Status-Image]: https://travis-ci.com/nats-io/nats.go.svg?branch=main
[GoDoc-Url]: https://pkg.go.dev/github.com/nats-io/nats.go
[GoDoc-Image]: https://img.shields.io/badge/GoDoc-reference-007d9c
[Coverage-Url]: https://coveralls.io/r/nats-io/nats.go?branch=main
[Coverage-image]: https://coveralls.io/repos/github/nats-io/nats.go/badge.svg?branch=main

## Installation

```bash
# Go client
go get github.com/nats-io/nats.go/

# Server
go get github.com/nats-io/nats-server
```

When using or transitioning to Go modules support:

```bash
# Go client latest or explicit version
go get github.com/nats-io/nats.go/@latest
go get github.com/nats-io/nats.go/@v1.27.0

# For latest NATS Server, add /v2 at the end
go get github.com/nats-io/nats-server/v2

# NATS Server v1 is installed otherwise
# go get github.com/nats-io/nats-server
```

## Basic Usage

```go
import "github.com/nats-io/nats.go"

// Connect to a server
nc, _ := nats.Connect(nats.DefaultURL)

// Simple Publisher
nc.Publish("foo", []byte("Hello World"))

// Simple Async Subscriber
nc.Subscribe("foo", func(m *nats.Msg) {
    fmt.Printf("Received a message: %s\n", string(m.Data))
})

// Responding to a request message
nc.Subscribe("request", func(m *nats.Msg) {
    m.Respond([]byte("answer is 42"))
})

// Simple Sync Subscriber
sub, err := nc.SubscribeSync("foo")
m, err := sub.NextMsg(timeout)

// Channel Subscriber
ch := make(chan *nats.Msg, 64)
sub, err := nc.ChanSubscribe("foo", ch)
msg := <- ch

// Unsubscribe
sub.Unsubscribe()

// Drain
sub.Drain()

// Requests
msg, err := nc.Request("help", []byte("help me"), 10*time.Millisecond)

// Replies
nc.Subscribe("help", func(m *nats.Msg) {
    nc.Publish(m.Reply, []byte("I can help!"))
})

// Drain connection (Preferred for responders)
// Close() not needed if this is called.
nc.Drain()

// Close connection
nc.Close()
```

## JetStream Basic Usage

> __NOTE__
>
> We encourage you to try out a new, simplified version on JetStream API.
> The new API is currently in preview and is available under `jetstream` package.
>
> You can find more information on the new API [here](https://github.com/nats-io/nats.go/blob/main/jetstream/README.md)

```go
import "github.com/nats-io/nats.go"

// Connect to NATS
nc, _ := nats.Connect(nats.DefaultURL)

// Create JetStream Context
js, _ := nc.JetStream(nats.PublishAsyncMaxPending(256))

// Simple Stream Publisher
js.Publish("ORDERS.scratch", []byte("hello"))

// Simple Async Stream Publisher
for i := 0; i < 500; i++ {
	js.PublishAsync("ORDERS.scratch", []byte("hello"))
}
select {
case <-js.PublishAsyncComplete():
case <-time.After(5 * time.Second):
	fmt.Println("Did not resolve in time")
}

// Simple Async Ephemeral Consumer
js.Subscribe("ORDERS.*", func(m *nats.Msg) {
	fmt.Printf("Received a JetStream message: %s\n", string(m.Data))
})

// Simple Sync Durable Consumer (optional SubOpts at the end)
sub, err := js.SubscribeSync("ORDERS.*", nats.Durable("MONITOR"), nats.MaxDeliver(3))
m, err := sub.NextMsg(timeout)

// Simple Pull Consumer
sub, err := js.PullSubscribe("ORDERS.*", "MONITOR")
msgs, err := sub.Fetch(10)

// Unsubscribe
sub.Unsubscribe()

// Drain
sub.Drain()
```

## JetStream Basic Management

```go
import "github.com/nats-io/nats.go"

// Connect to NATS
nc, _ := nats.Connect(nats.DefaultURL)

// Create JetStream Context
js, _ := nc.JetStream()

// Create a Stream
js.AddStream(&nats.StreamConfig{
	Name:     "ORDERS",
	Subjects: []string{"ORDERS.*"},
})

// Update a Stream
js.UpdateStream(&nats.StreamConfig{
	Name:     "ORDERS",
	MaxBytes: 8,
})

// Create a Consumer
js.AddConsumer("ORDERS", &nats.ConsumerConfig{
	Durable: "MONITOR",
})

// Delete Consumer
js.DeleteConsumer("ORDERS", "MONITOR")

// Delete Stream
js.DeleteStream("ORDERS")
```

## Encoded Connections

```go

nc, _ := nats.Connect(nats.DefaultURL)
c, _ := nats.NewEncodedConn(nc, nats.JSON_ENCODER)
defer c.Close()

// Simple Publisher
c.Publish("foo", "Hello World")

// Simple Async Subscriber
c.Subscribe("foo", func(s string) {
    fmt.Printf("Received a message: %s\n", s)
})

// EncodedConn can Publish any raw Go type using the registered Encoder
type person struct {
     Name     string
     Address  string
     Age      int
}

// Go type Subscriber
c.Subscribe("hello", func(p *person) {
    fmt.Printf("Received a person: %+v\n", p)
})

me := &person{Name: "derek", Age: 22, Address: "140 New Montgomery Street, San Francisco, CA"}

// Go type Publisher
c.Publish("hello", me)

// Unsubscribe
sub, err := c.Subscribe("foo", nil)
// ...
sub.Unsubscribe()

// Requests
var response string
err = c.Request("help", "help me", &response, 10*time.Millisecond)
if err != nil {
    fmt.Printf("Request failed: %v\n", err)
}

// Replying
c.Subscribe("help", func(subj, reply string, msg string) {
    c.Publish(reply, "I can help!")
})

// Close connection
c.Close();
```

## New Authentication (Nkeys and User Credentials)
This requires server with version >= 2.0.0

NATS servers have a new security and authentication mechanism to authenticate with user credentials and Nkeys.
The simplest form is to use the helper method UserCredentials(credsFilepath).
```go
nc, err := nats.Connect(url, nats.UserCredentials("user.creds"))
```

The helper methods creates two callback handlers to present the user JWT and sign the nonce challenge from the server.
The core client library never has direct access to your private key and simply performs the callback for signing the server challenge.
The helper will load and wipe and erase memory it uses for each connect or reconnect.

The helper also can take two entries, one for the JWT and one for the NKey seed file.
```go
nc, err := nats.Connect(url, nats.UserCredentials("user.jwt", "user.nk"))
```

You can also set the callback handlers directly and manage challenge signing directly.
```go
nc, err := nats.Connect(url, nats.UserJWT(jwtCB, sigCB))
```

Bare Nkeys are also supported. The nkey seed should be in a read only file, e.g. seed.txt
```bash
> cat seed.txt
# This is my seed nkey!
SUAGMJH5XLGZKQQWAWKRZJIGMOU4HPFUYLXJMXOO5NLFEO2OOQJ5LPRDPM
```

This is a helper function which will load and decode and do the proper signing for the server nonce.
It will clear memory in between invocations.
You can choose to use the low level option and provide the public key and a signature callback on your own.

```go
opt, err := nats.NkeyOptionFromSeed("seed.txt")
nc, err := nats.Connect(serverUrl, opt)

// Direct
nc, err := nats.Connect(serverUrl, nats.Nkey(pubNkey, sigCB))
```

## TLS

```go
// tls as a scheme will enable secure connections by default. This will also verify the server name.
nc, err := nats.Connect("tls://nats.demo.io:4443")

// If you are using a self-signed certificate, you need to have a tls.Config with RootCAs setup.
// We provide a helper method to make this case easier.
nc, err = nats.Connect("tls://localhost:4443", nats.RootCAs("./configs/certs/ca.pem"))

// If the server requires client certificate, there is an helper function for that too:
cert := nats.ClientCert("./configs/certs/client-cert.pem", "./configs/certs/client-key.pem")
nc, err = nats.Connect("tls://localhost:4443", cert)

// You can also supply a complete tls.Config

certFile := "./configs/certs/client-cert.pem"
keyFile := "./configs/certs/client-key.pem"
cert, err := tls.LoadX509KeyPair(certFile, keyFile)
if err != nil {
    t.Fatalf("error parsing X509 certificate/key pair: %v", err)
}

config := &tls.Config{
    ServerName: 	opts.Host,
    Certificates: 	[]tls.Certificate{cert},
    RootCAs:    	pool,
    MinVersion: 	tls.VersionTLS12,
}

nc, err = nats.Connect("nats://localhost:4443", nats.Secure(config))
if err != nil {
	t.Fatalf("Got an error on Connect with Secure Options: %+v\n", err)
}

```

## Using Go Channels (netchan)

```go
nc, _ := nats.Connect(nats.DefaultURL)
ec, _ := nats.NewEncodedConn(nc, nats.JSON_ENCODER)
defer ec.Close()

type person struct {
     Name     string
     Address  string
     Age      int
}

recvCh := make(chan *person)
ec.BindRecvChan("hello", recvCh)

sendCh := make(chan *person)
ec.BindSendChan("hello", sendCh)

me := &person{Name: "derek", Age: 22, Address: "140 New Montgomery Street"}

// Send via Go channels
sendCh <- me

// Receive via Go channels
who := <- recvCh
```

## Wildcard Subscriptions

```go

// "*" matches any token, at any level of the subject.
nc.Subscribe("foo.*.baz", func(m *Msg) {
    fmt.Printf("Msg received on [%s] : %s\n", m.Subject, string(m.Data));
})

nc.Subscribe("foo.bar.*", func(m *Msg) {
    fmt.Printf("Msg received on [%s] : %s\n", m.Subject, string(m.Data));
})

// ">" matches any length of the tail of a subject, and can only be the last token
// E.g. 'foo.>' will match 'foo.bar', 'foo.bar.baz', 'foo.foo.bar.bax.22'
nc.Subscribe("foo.>", func(m *Msg) {
    fmt.Printf("Msg received on [%s] : %s\n", m.Subject, string(m.Data));
})

// Matches all of the above
nc.Publish("foo.bar.baz", []byte("Hello World"))

```

## Queue Groups

```go
// All subscriptions with the same queue name will form a queue group.
// Each message will be delivered to only one subscriber per queue group,
// using queuing semantics. You can have as many queue groups as you wish.
// Normal subscribers will continue to work as expected.

nc.QueueSubscribe("foo", "job_workers", func(_ *Msg) {
  received += 1;
})
```

## Advanced Usage

```go

// Normally, the library will return an error when trying to connect and
// there is no server running. The RetryOnFailedConnect option will set
// the connection in reconnecting state if it failed to connect right away.
nc, err := nats.Connect(nats.DefaultURL,
    nats.RetryOnFailedConnect(true),
    nats.MaxReconnects(10),
    nats.ReconnectWait(time.Second),
    nats.ReconnectHandler(func(_ *nats.Conn) {
        // Note that this will be invoked for the first asynchronous connect.
    }))
if err != nil {
    // Should not return an error even if it can't connect, but you still
    // need to check in case there are some configuration errors.
}

// Flush connection to server, returns when all messages have been processed.
nc.Flush()
fmt.Println("All clear!")

// FlushTimeout specifies a timeout value as well.
err := nc.FlushTimeout(1*time.Second)
if err != nil {
    fmt.Println("All clear!")
} else {
    fmt.Println("Flushed timed out!")
}

// Auto-unsubscribe after MAX_WANTED messages received
const MAX_WANTED = 10
sub, err := nc.Subscribe("foo")
sub.AutoUnsubscribe(MAX_WANTED)

// Multiple connections
nc1 := nats.Connect("nats://host1:4222")
nc2 := nats.Connect("nats://host2:4222")

nc1.Subscribe("foo", func(m *Msg) {
    fmt.Printf("Received a message: %s\n", string(m.Data))
})

nc2.Publish("foo", []byte("Hello World!"));

```

## Clustered Usage

```go

var servers = "nats://localhost:1222, nats://localhost:1223, nats://localhost:1224"

nc, err := nats.Connect(servers)

// Optionally set ReconnectWait and MaxReconnect attempts.
// This example means 10 seconds total per backend.
nc, err = nats.Connect(servers, nats.MaxReconnects(5), nats.ReconnectWait(2 * time.Second))

// You can also add some jitter for the reconnection.
// This call will add up to 500 milliseconds for non TLS connections and 2 seconds for TLS connections.
// If not specified, the library defaults to 100 milliseconds and 1 second, respectively.
nc, err = nats.Connect(servers, nats.ReconnectJitter(500*time.Millisecond, 2*time.Second))

// You can also specify a custom reconnect delay handler. If set, the library will invoke it when it has tried
// all URLs in its list. The value returned will be used as the total sleep time, so add your own jitter.
// The library will pass the number of times it went through the whole list.
nc, err = nats.Connect(servers, nats.CustomReconnectDelay(func(attempts int) time.Duration {
    return someBackoffFunction(attempts)
}))

// Optionally disable randomization of the server pool
nc, err = nats.Connect(servers, nats.DontRandomize())

// Setup callbacks to be notified on disconnects, reconnects and connection closed.
nc, err = nats.Connect(servers,
	nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
		fmt.Printf("Got disconnected! Reason: %q\n", err)
	}),
	nats.ReconnectHandler(func(nc *nats.Conn) {
		fmt.Printf("Got reconnected to %v!\n", nc.ConnectedUrl())
	}),
	nats.ClosedHandler(func(nc *nats.Conn) {
		fmt.Printf("Connection closed. Reason: %q\n", nc.LastError())
	})
)

// When connecting to a mesh of servers with auto-discovery capabilities,
// you may need to provide a username/password or token in order to connect
// to any server in that mesh when authentication is required.
// Instead of providing the credentials in the initial URL, you will use
// new option setters:
nc, err = nats.Connect("nats://localhost:4222", nats.UserInfo("foo", "bar"))

// For token based authentication:
nc, err = nats.Connect("nats://localhost:4222", nats.Token("S3cretT0ken"))

// You can even pass the two at the same time in case one of the server
// in the mesh requires token instead of user name and password.
nc, err = nats.Connect("nats://localhost:4222",
    nats.UserInfo("foo", "bar"),
    nats.Token("S3cretT0ken"))

// Note that if credentials are specified in the initial URLs, they take
// precedence on the credentials specified through the options.
// For instance, in the connect call below, the client library will use
// the user "my" and password "pwd" to connect to localhost:4222, however,
// it will use username "foo" and password "bar" when (re)connecting to
// a different server URL that it got as part of the auto-discovery.
nc, err = nats.Connect("nats://my:pwd@localhost:4222", nats.UserInfo("foo", "bar"))

```

## Context support (+Go 1.7)

```go
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

nc, err := nats.Connect(nats.DefaultURL)

// Request with context
msg, err := nc.RequestWithContext(ctx, "foo", []byte("bar"))

// Synchronous subscriber with context
sub, err := nc.SubscribeSync("foo")
msg, err := sub.NextMsgWithContext(ctx)

// Encoded Request with context
c, err := nats.NewEncodedConn(nc, nats.JSON_ENCODER)
type request struct {
	Message string `json:"message"`
}
type response struct {
	Code int `json:"code"`
}
req := &request{Message: "Hello"}
resp := &response{}
err := c.RequestWithContext(ctx, "foo", req, resp)
```

## License

Unless otherwise noted, the NATS source files are distributed
under the Apache Version 2.0 license found in the LICENSE file.

[![FOSSA Status](https://app.fossa.io/api/projects/git%2Bgithub.com%2Fnats-io%2Fgo-nats.svg?type=large)](https://app.fossa.io/projects/git%2Bgithub.com%2Fnats-io%2Fgo-nats?ref=badge_large)
