# JetStream Simplified Client

This doc covers the basic usage of the `jetstream` package in `nats.go` client.

- [JetStream Simplified Client](#jetstream-simplified-client)
  - [Overview](#overview)
  - [Basic usage](#basic-usage)
  - [Streams](#streams)
    - [Stream management (CRUD)](#stream-management--crud-)
    - [Listing streams and stream names](#listing-streams-and-stream-names)
    - [Stream-specific operations](#stream-specific-operations)
  - [Consumers](#consumers)
    - [Consumers management](#consumers-management)
    - [Listing consumers and consumer
      names](#listing-consumers-and-consumer-names)
    - [Ordered consumers](#ordered-consumers)
    - [Receiving messages from the
      consumer](#receiving-messages-from-the-consumer)
      - [Single fetch](#single-fetch)
      - [Continuous polling](#continuous-polling)
        - [Using `Consume()` receive messages in a
          callback](#using-consume-receive-messages-in-a-callback)
        - [Using `Messages()` to iterate over incoming
          messages](#using-messages-to-iterate-over-incoming-messages)
  - [Publishing on stream](#publishing-on-stream)
    - [Synchronous publish](#synchronous-publish)
    - [Async publish](#async-publish)

## Overview

`jetstream` package is a new client API to interact with NATS JetStream, aiming
to replace the JetStream client implementation from `nats` package. The main
goal of this package is to provide a simple and clear way to interact with
JetStream API. Key differences between `jetstream` and `nats` packages include:

- Using smaller, simpler interfaces to manage streams and consumers
- Using more granular and predictable approach to consuming messages from a
  stream, instead of relying on often complicated and unpredictable
  `Subscribe()` method (and all of its flavors)
- Allowing the usage of pull consumers to continuously receive incoming messages
  (including ordered consumer functionality)
- Separating JetStream context from core NATS

`jetstream` package provides several ways of interacting with the API:

- `JetStream` - top-level interface, used to create and manage streams,
  consumers and publishing messages
- `Stream` - used to manage consumers for a specific stream, as well as
  performing stream-specific operations (purging, fetching and deleting messages
  by sequence number, fetching stream info)
- `Consumer` - used to get information about a consumer as well as consuming
  messages
- `Msg` - used for message-specific operations - reading data, headers and
  metadata, as well as performing various types of acknowledgements

> __NOTE__: `jetstream` requires nats-server >= 2.9.0 to work correctly.

> __WARNING__: The new API is currently provided as a _preview_, and will
> deprecate previous JetStream subscribe APIs. It is encouraged to start
experimenting with the new APIs as soon as possible.

## Basic usage

```go
import (
    "context"
    "fmt"
    "time"

    "github.com/nats-io/nats.go"
    "github.com/nats-io/nats.go/jetstream"
)

func main() {
    // In the `jetstream` package, almost all API calls rely on `context.Context` for timeout/cancellation handling
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
    defer cancel()
    nc, _ := nats.Connect(nats.DefaultURL)

    // Create a JetStream management interface
    js, _ := jetstream.New(nc)

    // Create a stream
    s, _ := js.CreateStream(ctx, jetstream.StreamConfig{
        Name:     "ORDERS",
        Subjects: []string{"ORDERS.*"},
    })

    // Publish some messages
    for i := 0; i < 100; i++ {
        js.Publish(ctx, "ORDERS.new", []byte("hello"))
    }

    // Create durable consumer
    c, _ := s.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
        Durable:   "CONS",
        AckPolicy: jetstream.AckExplicitPolicy,
    })

    // Get 10 messages from the consumer
    msgs, _ := c.Fetch(10)
    var msg jetstream.Msg
    for msg := range msgs.Messages() {
        msg.Ack()
        fmt.Printf("Received a JetStream message: %s\n", string(msg.Data()))
    }
    if msgs.Error() {
        fmt.Println("Error during Fetch(): ", msgs.Error())
    }

    // Receive messages continuously in a callback
    cons, _ := c.Consume(ctx, func(msg jetstream.Msg) {
        msg.Ack()
        fmt.Printf("Received a JetStream message: %s\n", string(msg.Data()))
    })
    defer cons.Stop()


    // Iterate over messages continuously
    it, _ := cons.Messages()
    for i := 0; i < 10; i++ {
        msg, err := it.Next()
        if err != nil {
            log.Fatal(err)
        }
        msg.Ack()
        fmt.Println("Received a JetStream message: %s\n", string(msg.Data()))
    }
    it.Stop()
}
```

## Streams

`jetstream` provides methods to manage and list streams, as well as perform
stream-specific operations (purging, fetching/deleting messages by sequence id)

### Stream management (CRUD)

```go
js, _ := jetstream.New(nc)

// create a stream (this is an idempotent operation)
s, _ := js.CreateStream(ctx, jetstream.StreamConfig{
    Name:     "ORDERS",
    Subjects: []string{"ORDERS.*"},
})

// update a stream
s, _ = js.UpdateStream(ctx, jetstream.StreamConfig{
    Name:        "ORDERS",
    Subjects:    []string{"ORDERS.*"},
    Description: "updated stream",
})

// get stream handle
s, _ = js.Stream(ctx, "ORDERS")

// delete a stream
js.DeleteStream(ctx, "ORDERS")
```

### Listing streams and stream names

```go
// list streams
streams := js.ListStreams(ctx)
var err error
for err != nil {
    select {
    case s := <-streams.Info():
        fmt.Println(s.Config.Name)
    case err = <-streams.Err():
    }
}
if err != nil && !errors.Is(err, jetstream.ErrEndOfData) {
    fmt.Println("Unexpected error ocurred")
}

// list stream names
names := js.StreamNames(ctx)
for err != nil {
    select {
    case name := <-names.Name():
        fmt.Println(name)
    case err = <-names.Err():
    }
}
if err != nil && !errors.Is(err, jetstream.ErrEndOfData) {
    fmt.Println("Unexpected error ocurred")
}
```

### Stream-specific operations

Using `Stream` interface, it is also possible to:

- Purge a stream

```go
// remove all messages from a stream
_ = s.Purge(ctx)

// remove all messages from a stream that are stored on a specific subject
_ = s.Purge(ctx, jetstream.WithSubject("ORDERS.new"))

// remove all messages up to specified sequence number
_ = s.Purge(ctx, jetstream.WithSequence(100))

// remove messages, but keep 10 newest
_ = s.Purge(ctx, jetstream.WithKeep(10))
```

- Get and messages from stream

```go
// get message from stream with sequence number == 100
msg, _ := s.GetMsg(ctx, 100)

// get last message from "ORDERS.new" subject
msg, _ = s.GetLastMsgForSubject(ctx, "ORDERS.new")

// delete a message with sequence number == 100
_ = s.DeleteMsg(ctx, 100)
```

- Get information about a stream

```go
// Fetches latest stream info from server
info, _ := s.Info(ctx)
fmt.Println(info.Config.Name)

// Returns the most recently fetched StreamInfo, without making an API call to the server
cachedInfo := s.CachedInfo()
fmt.Println(cachedInfo.Config.Name)
```

## Consumers

Only pull consumers are supported in `jetstream` package. However, unlike the
JetStream API in `nats` package, pull consumers allow for continuous message
retrieval (similarly to how `nats.Subscribe()` works). Because of that, push
consumers can be easily replace by pull consumers for most of the use cases.

### Consumers management

CRUD operations on consumers can be achieved on 2 levels:

- on `JetStream` interface

```go
js, _ := jetstream.New(nc)

// create a consumer (this is an idempotent operation)
cons, _ := js.CreateOrUpdateConsumer(ctx, "ORDERS", jetstream.ConsumerConfig{
    Durable: "foo",
    AckPolicy: jetstream.AckExplicitPolicy,
})

// create an ephemeral pull consumer by not providing `Durable`
ephemeral, _ := js.CreateOrUpdateConsumer(ctx, "ORDERS", jetstream.ConsumerConfig{
    AckPolicy: jetstream.AckExplicitPolicy,
})

// get consumer handle
cons, _ = js.Consumer(ctx, "ORDERS", "foo")

// delete a consumer
js.DeleteConsumer(ctx, "ORDERS", "foo")
```

- on `Stream` interface

```go
// Create a JetStream management interface
js, _ := jetstream.New(nc)

// get stream handle
stream, _ := js.Stream(ctx, "ORDERS")

// create consumer
cons, _ := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
    Durable:   "foo",
    AckPolicy: jetstream.AckExplicitPolicy,
})

// get consumer handle
cons, _ = stream.Consumer(ctx, "ORDERS", "foo")

// delete a consumer
stream.DeleteConsumer(ctx, "foo")
```

`Consumer` interface, returned when creating/fetching consumers, allows fetching
`ConsumerInfo`:

```go
// Fetches latest consumer info from server
info, _ := cons.Info(ctx)
fmt.Println(info.Config.Durable)

// Returns the most recently fetched ConsumerInfo, without making an API call to the server
cachedInfo := cons.CachedInfo()
fmt.Println(cachedInfo.Config.Durable)
```

### Listing consumers and consumer names

```go
// list consumers
consumers := s.ListConsumers(ctx)
var err error
for err != nil {
    select {
    case s := <-consumers.Info():
        fmt.Println(s.Name)
    case err = <-consumers.Err():
    }
}
if err != nil && !errors.Is(err, jetstream.ErrEndOfData) {
    fmt.Println("Unexpected error occured")
}

// list consumer names
names := s.ConsumerNames(ctx)
for err != nil {
    select {
    case name := <-names.Name():
        fmt.Println(name)
    case err = <-names.Err():
    }
}
if err != nil && !errors.Is(err, jetstream.ErrEndOfData) {
    fmt.Println("Unexpected error occured")
}
```

### Ordered consumers

`jetstream`, in addition to basic named/ephemeral consumers, supports ordered
consumer functionality. Ordered is strictly processing messages in the order
that they were stored on the stream, providing a consistent and deterministic
message ordering. It is also resilient to consumer deletion.

Ordered consumers present the same set of message consumption methods as
standard pull consumers.

```go
js, _ := jetstream.New(nc)

// create a consumer (this is an idempotent operation)
cons, _ := js.OrderedConsumer(ctx, "ORDERS", jetstream.OrderedConsumerConfig{
    // Filter results from "ORDERS" stream by specific subject
    FilterSubjects: []{"ORDERS.A"},
})
```

### Receiving messages from the consumer

The `Consumer` interface covers allows fetching messages on demand, with
pre-defined batch size on bytes limit, or continuous push-like receiving of
messages.

#### __Single fetch__

This pattern pattern allows fetching a defined number of messages in a single
RPC.

- Using `Fetch` or `FetchBytes`, consumer will return up to the provided number
of messages/bytes. By default, `Fetch()` will wait 30 seconds before timing out
(this behavior can be configured using `FetchMaxWait()` option):

```go
// receive up to 10 messages from the stream
msgs, _ := c.Fetch(10)
for msg := range msgs.Messages() {
    fmt.Printf("Received a JetStream message: %s\n", string(msg.Data()))
}
if msgs.Error() != nil {
    // handle error
}

// receive up to 1024kB of data
msgs, _ := c.FetchBytes(1024)
for msg := range msgs.Messages() {
    fmt.Printf("Received a JetStream message: %s\n", string(msg.Data()))
}
if msgs.Error() != nil {
    // handle error
}
```

Similarly, `FetchNoWait()` can be used in order to only return messages from the
stream available at the time of sending request:

```go
// FetchNoWait will not wait for new messages if the whole batch is not available at the time of sending request.
msgs, _ := c.FetchNoWait(10)
for msg := range msgs.Messages() {
    fmt.Printf("Received a JetStream message: %s\n", string(msg.Data()))
}
if msgs.Error() != nil {
    // handle error
}
```

> __Warning__: Both `Fetch()` and `FetchNoWait()` have worse performance when
> used to continuously retrieve messages in comparison to `Messages()` or
`Consume()` methods, as they do not perform any optimizations (pre-buffering)
and new subscription is created for each execution.

#### Continuous polling

There are 2 ways to achieve push-like behavior using pull consumers in
`jetstream` package. Both `Messages()` and `Consume()` methods perform exactly
the same optimizations and can be used interchangeably.

Subject filtering is achieved by configuring a consumer with a `FilterSubject`
value.

##### Using `Consume()` receive messages in a callback

```go
cons, _ := js.CreateOrUpdateConsumer("ORDERS", jetstream.ConsumerConfig{
    AckPolicy: jetstream.AckExplicitPolicy,
    // receive messages from ORDERS.A subject only
    FilterSubject: "ORDERS.A"
}))

consContext, _ := c.Consume(func(msg jetstream.Msg) {
    fmt.Printf("Received a JetStream message: %s\n", string(msg.Data()))
})
defer consContext.Stop()
```

Similarly to `Messages()`, `Consume()` can be supplied with options to modify
the behavior of a single pull request:

- `PullMaxMessages(int)` - up to provided number of messages will be buffered
- `PullMaxBytes(int)` - up to provided number of bytes will be buffered. This
setting and `PullMaxMessages` are mutually exclusive
- `PullExpiry(time.Duration)` - timeout on a single pull request to the server
type PullThresholdMessages int
- `PullThresholdMessages(int)` - amount of messages which triggers refilling the
  buffer
- `PullThresholdBytes(int)` - amount of bytes which triggers refilling the
  buffer
- `PullHeartbeat(time.Duration)` - idle heartbeat duration for a single pull
request. An error will be triggered if at least 2 heartbeats are missed
- `WithConsumeErrHandler(func (ConsumeContext, error))` - when used, sets a
  custom error handler on `Consume()`, allowing e.g. tracking missing
  heartbeats.

> __NOTE__: `Stop()` should always be called on `ConsumeContext` to avoid
> leaking goroutines.

##### Using `Messages()` to iterate over incoming messages

```go
iter, _ := cons.Messages()
for {
    msg, err := iter.Next()
    // Next can return error, e.g. when iterator is closed or no heartbeats were received
    if err != nil {
        //handle error
    }
    fmt.Printf("Received a JetStream message: %s\n", string(msg.Data()))
    msg.Ack()
}
iter.Stop()
```

It can also be configured to only store up to defined number of messages/bytes
in the buffer.

```go
// a maximum of 10 messages or 1024 bytes will be stored in memory (whichever is encountered first)
iter, _ := cons.Messages(jetstream.PullMaxMessages(10), jetstream.PullMaxBytes(1024))
```

`Messages()` exposes the following options:

- `PullMaxMessages(int)` - up to provided number of messages will be buffered
- `PullMaxBytes(int)` - up to provided number of bytes will be buffered. This
setting and `PullMaxMessages` are mutually exclusive
- `PullExpiry(time.Duration)` - timeout on a single pull request to the server
type PullThresholdMessages int
- `PullThresholdMessages(int)` - amount of messages which triggers refilling the
  buffer
- `PullThresholdBytes(int)` - amount of bytes which triggers refilling the
  buffer
- `PullHeartbeat(time.Duration)` - idle heartbeat duration for a single pull
request. An error will be triggered if at least 2 heartbeats are missed (unless
`WithMessagesErrOnMissingHeartbeat(false)` is used)

## Publishing on stream

`JetStream` interface allows publishing messages on stream in 2 ways:

### __Synchronous publish__

```go
js, _ := jetstream.New(nc)

// Publish message on subject ORDERS.new
// Given subject has to belong to a stream
ack, err := js.PublishMsg(ctx, &nats.Msg{
    Data:    []byte("hello"),
    Subject: "ORDERS.new",
})
fmt.Printf("Published msg with sequence number %d on stream %q", ack.Sequence, ack.Stream)

// A helper method accepting subject and data as parameters
ack, err = js.Publish(ctx, "ORDERS.new", []byte("hello"))
```

Both `Publish()` and `PublishMsg()` can be supplied with options allowing
setting various headers. Additionally, for `PublishMsg()` headers can be set
directly on `nats.Msg`.

```go
// All 3 implementations are work identically 
ack, err := js.PublishMsg(ctx, &nats.Msg{
    Data:    []byte("hello"),
    Subject: "ORDERS.new",
    Header: nats.Header{
        "Nats-Msg-Id": []string{"id"},
    },
})

ack, err = js.PublishMsg(ctx, &nats.Msg{
    Data:    []byte("hello"),
    Subject: "ORDERS.new",
}, jetstream.WithMsgID("id"))

ack, err = js.Publish(ctx, "ORDERS.new", []byte("hello"), jetstream.WithMsgID("id"))
```

### __Async publish__

```go
js, _ := jetstream.New(nc)

// publish message and do not wait for ack
ackF, err := js.PublishMsgAsync(ctx, &nats.Msg{
    Data:    []byte("hello"),
    Subject: "ORDERS.new",
})

// block and wait for ack
select {
case ack := <-ackF.Ok():
    fmt.Printf("Published msg with sequence number %d on stream %q", ack.Sequence, ack.Stream)
case err := <-ackF.Err():
    fmt.Println(err)
}

// similarly to syncronous publish, there is a helper method accepting subject and data
ackF, err = js.PublishAsync("ORDERS.new", []byte("hello"))
```

Just as for synchronous publish, `PublishAsync()` and `PublishMsgAsync()` accept
options for setting headers.
