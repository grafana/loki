// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

/*
Package pubsub provides an easy way to publish and receive Google Cloud Pub/Sub
messages, hiding the details of the underlying server RPCs.
Pub/Sub is a many-to-many, asynchronous messaging system that decouples senders
and receivers.

If you are migrating from the v1 library, please read over the [migration guide].

More information about Pub/Sub is available at the [product documentation page].

See the [main Google Cloud Go package] for authentication, timeouts,
connection pooling and similar aspects of this package.

# Publishing

Pub/Sub messages are published to topics via publishers.
A Topic may be created like so:

	ctx := context.Background()
	client, _ := pubsub.NewClient(ctx, "my-project")
	topic, err := client.TopicAdminClient.CreateTopic(ctx, &pubsubpb.Topic{
		Name: "projects/my-project/topics/my-topic",
	})

A [Publisher] client can then be instantiated and used to publish messages.

	publisher := client.Publisher(topic.GetName())
	res := publisher.Publish(ctx, &pubsub.Message{Data: []byte("payload")})

[Publisher.Publish] queues the message for publishing and returns immediately. When enough
messages have accumulated, or enough time has elapsed, the batch of messages is
sent to the Pub/Sub service.

[Publisher.Publish] returns a [PublishResult], which behaves like a future: its Get method
blocks until the message has been sent to the service.

The first time you call [Publisher.Publish] on a [Publisher], goroutines are started in the
background. To clean up these goroutines, call [Publisher.Stop]:

	publisher.Stop()

# Receiving

To receive messages published to a topic, clients create a subscription
for the topic. There may be more than one subscription per topic; each message
that is published to the topic will be delivered to all associated subscriptions.

You then need to create a [Subscriber] client to pull messages from a subscription.

A subscription may be created like so:

	ctx := context.Background()
	client, _ := pubsub.NewClient(ctx, "my-project")
	subscription, err := client.SubscriptionAdminClient.CreateSubscription(ctx,
		&pubsubpb.Subscription{
			Name: "projects/my-project/subscriptions/my-sub",
			Topic: "projects/my-project/topics/my-topic"}
		),
	}

A [Subscriber] client can be instantiated like so:

	sub := client.Subscriber(subscription.GetName())

You then provide a callback to [Subscriber] which processes the messages.

	err := sub.Receive(ctx, func(ctx context.Context, m *Message) {
		log.Printf("Got message: %s", m.Data)
		m.Ack()
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		// Handle error.
	}

The callback is invoked concurrently by multiple goroutines, maximizing
throughput. To terminate a call to [Subscriber.Receive], cancel its context.

Once client code has processed the [Message], it must call Message.Ack or
Message.Nack. If Ack is not called, the Message will eventually be redelivered. Ack/Nack
MUST be called within the [Subscriber.Receive] handler function, and not from a goroutine.
Otherwise, flow control (e.g. ReceiveSettings.MaxOutstandingMessages) will
not be respected. Additionally, messages can get orphaned when Receive is canceled,
resulting in slow redelivery.

If the client cannot or does not want to process the message, it can call Message.Nack
to speed redelivery. For more information and configuration options, see
Ack Deadlines below.

Note: It is possible for a [Message] to be redelivered even if Message.Ack has
been called unless exactly once delivery is enabled. Applications should be aware
of these deliveries.

Note: This uses pubsub's streaming pull feature. This feature has properties that
may be surprising. Please refer to the [Streaming Pull API] for more details on
how streaming pull behaves.

# Emulator

To use an emulator with this library, you can set the PUBSUB_EMULATOR_HOST
environment variable to the address at which your emulator is running. This will
send requests to that address instead of to Pub/Sub. You can then create
and use a client as usual:

	// Set PUBSUB_EMULATOR_HOST environment variable.
	err := os.Setenv("PUBSUB_EMULATOR_HOST", "localhost:8085")
	if err != nil {
		// TODO: Handle error.
	}
	// Create client as usual.
	client, err := pubsub.NewClient(ctx, "my-project-id")
	if err != nil {
		// TODO: Handle error.
	}
	defer client.Close()

# Ack Deadlines

The default ack deadlines are suitable for most use cases, but may be
overridden. This section describes the tradeoffs that should be considered
when overriding the defaults.

Behind the scenes, each message returned by the Pub/Sub server has an
associated lease, known as an "ack deadline". Unless a message is
acknowledged within the ack deadline, or the client requests that
the ack deadline be extended, the message will become eligible for redelivery.

As a convenience, the pubsub client will automatically extend deadlines until
either:
  - Message.Ack or Message.Nack is called, or
  - The "MaxExtension" duration elapses from the time the message is fetched from
    the server. This defaults to 60m.

Ack deadlines are extended periodically by the client. The period between extensions,
as well as the length of the extension, automatically adjusts based on the time it takes the
subscriber application to ack messages (based on the 99th percentile of ack latency).
By default, this extension period is capped at 10m, but this limit can be configured
by the Min/MaxDurationPerAckExtension settings. This has the effect that subscribers that process
messages quickly have their message ack deadlines extended for a short amount, whereas
subscribers that process message slowly have their message ack deadlines extended
for a large amount. The net effect is fewer RPCs sent from the client library.

For example, consider a subscriber that takes 3 minutes to process each message.
Since the library has already recorded several 3-minute "ack latencies"s in a
percentile distribution, future message extensions are sent with a value of 3
minutes, every 3 minutes. Suppose the application crashes 5 seconds after the
library sends such an extension: the Pub/Sub server would wait the remaining
2m55s before re-sending the messages out to other subscribers.

Please note that the client library does not use the subscription's
AckDeadline for the MaxExtension value.

# Fine Tuning PubSub Receive Performance

This section describes how to adjust [ReceiveSettings] for best performance.

	Subscriber.ReceiveSettings.MaxExtension

This is the maximum amount of time that the client will extend a message's deadline.
This value should be set to the maximum expected processing time, plus some
buffer. The higher the extension allowed, the longer it takes before the server considers
messages lost and re-sends them to some other, healthy instance of your application.

	Subscriber.ReceiveSettings.MaxDurationPerAckExtension

This is the maximum amount of time to extend each message's deadline per
ModifyAckDeadline RPC. Normally, the deadline is determined by the 99th percentile
of previous message processing times. However, if normal processing time takes 10 minutes
but an error occurs while processing a message within 1 minute, a message will be
stuck and held by the client for the remaining 9 minutes. By setting the maximum amount
of time to extend a message's deadline on a per-RPC basis, you can decrease the amount
of time before message redelivery when errors occur. However, the downside is that more
ModifyAckDeadline RPCs will be sent.

	Subscriber.ReceiveSettings.MinDurationPerAckExtension

This is the minimum amount of time to extend each message's deadline per
ModifyAckDeadline RPC. This is the complement setting of MaxDurationPerAckExtension and
represents the lower bound of modack deadlines to send. If processing time is very
low, it may be better to issue fewer ModifyAckDeadline RPCs rather than every
10 seconds. Setting both Min/MaxDurationPerAckExtension to the same value
effectively removes the automatic derivation of deadlines and fixes it to the value
you wish to extend your messages' deadlines by each time.

	Subscriber.ReceiveSettings.MaxOutstandingMessages

This is the maximum number of messages that are to be processed by the callback
function at a time. Once this limit is reached, the client waits for messages
to be acked or nacked by the callback before requesting more messages from the server.

This value is set by default to a fairly conservatively low number. We strongly
encourage setting this number as high as memory allows, since a low setting will
artificially rate limit reception. Setting this value to -1 causes it to be unbounded.

	Subscriber.ReceiveSettings.MaxOutstandingBytes

This is the maximum amount of bytes (message size) that are to be processed by
the callback function at a time. Once this limit is reached, the client waits
for messages to be acked or nacked by the callback before requesting more
messages from the server.

Note that there sometimes can be more bytes pulled and being processed than
MaxOutstandingBytes allows. This is due to the fact that the server
does not consider byte size when tracking server-side flow control.
For example, if the client sets MaxOutstandingBytes to 50 KiB, but receives
a batch of messages totaling 100 KiB, there will be a temporary overflow of
message byte size until messages are acked.

Similar to MaxOutstandingMessages, you can set this higher to maximize
processing throughput. Setting this value to -1 causes it to be unbounded.

	Subscriber.ReceiveSettings.NumGoroutines

This is the number of goroutines spawned to receive messages from the Pubsub server.
Each goroutine opens a StreamingPull stream, so this also directly sets the number of
open StreamingPull streams.

According to the [Resource Limits] page, each stream can handle about 10 MB/s of messages.
Leaving this value as the default would be good for most use cases.

If increasing this value to be greater than 1, please set `EnablePerStreamFlowControl` to true.
This helps align the server-side flow control with what is available locally. For more
explanation of this issue, see [this issue].

Going above 100 streams can lead to poor behavior, such as acks/modacks not succeeding in a reasonable
amount of time and resulting in high message expiration rates. In these cases,
increase the number of subscriber client applications rather than increasing this value.

By default, the number of connections in the gRPC conn pool is min(4,GOMAXPROCS). Each connection supports
up to 100 streams. Thus, if you have 4 or more CPU cores, the default setting allows a maximum of 400 streams
which is already excessive for most use cases.
If you want to change the limits on the number of streams, you can change the number of connections
in the gRPC connection pool as shown below:

	 opts := []option.ClientOption{
		option.WithGRPCConnectionPool(2),
	 }
	 client, err := pubsub.NewClient(ctx, projID, opts...)

This [medium post] describes tuning Pub/Sub performance in more detail.

# General tips

Each application should use a single PubSub client instead of creating many.
In addition, when publishing to a single topic, a publisher should be instantiated
once and reused to take advantage of flow control and batching capabilities.

[product documentation page]: https://cloud.google.com/pubsub/docs.
[Streaming Pull API]: https://docs.cloud.google.com/pubsub/docs/pull#streamingpull-api
[main Google Cloud Go package]: https://pkg.go.dev/cloud.google.com/go
[migration guide]: https://github.com/googleapis/google-cloud-go/blob/main/pubsub/MIGRATING.md
[medium post]: https://medium.com/google-cloud/pub-sub-flow-control-batching-9ba9a75bce3b
[this issue]: https://issuetracker.google.com/352592079
[Resource Limits]: https://docs.cloud.google.com/pubsub/quotas#resource_limits
*/
package pubsub // import "cloud.google.com/go/pubsub/v2"
