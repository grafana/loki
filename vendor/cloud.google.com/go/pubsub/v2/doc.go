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

If you are migrating from the v1 library, please read over the migration guide:
https://github.com/googleapis/google-cloud-go/blob/main/pubsub/MIGRATING.md

More information about Pub/Sub is available at
https://cloud.google.com/pubsub/docs.

See https://godoc.org/cloud.google.com/go for authentication, timeouts,
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
may be surprising. Please refer to https://cloud.google.com/pubsub/docs/pull#streamingpull
for more details on how streaming pull behaves.

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

As the PubSub client receives messages from the PubSub server, it puts them into
the callback function passed to Receive. The user must Ack or Nack a message
in this function. Each invocation by the client of the passed-in callback occurs
in a goroutine; that is, messages are processed concurrently.

The buffer holds a maximum of MaxOutstandingMessages messages or MaxOutstandingBytes
bytes, and the client stops requesting more messages from the server whenever the buffer
is full. Messages in the buffer have an ack deadline; that is, the server keeps a
deadline for each outstanding message. When that deadline expires, the server considers
the message lost and redelivers the message. Each message in the buffer automatically has
its deadline periodically extended. If a message is held beyond its deadline,
for example if your program hangs, the message will be redelivered.

This medium post describes tuning Pub/Sub performance in more detail
https://medium.com/google-cloud/pub-sub-flow-control-batching-9ba9a75bce3b

- Subscription.ReceiveSettings.MaxExtension

This is the maximum amount of time that the client will extend a message's deadline.
This value should be set to the maximum expected processing time, plus some
buffer. It is fairly safe to set it quite high; the only downside is that it will take
longer to recover from hanging programs. The higher the extension allowed, the longer
it takes before the server considers messages lost and re-sends them to some
other, healthy instance of your application.

- Subscription.ReceiveSettings.MaxDurationPerAckExtension

This is the maximum amount of time to extend each message's deadline per
ModifyAckDeadline RPC. Normally, the deadline is determined by the 99th percentile
of previous message processing times. However, if normal processing time takes 10 minutes
but an error occurs while processing a message within 1 minute, a message will be
stuck and held by the client for the remaining 9 minutes. By setting the maximum amount
of time to extend a message's deadline on a per-RPC basis, you can decrease the amount
of time before message redelivery when errors occur. However, the downside is that more
ModifyAckDeadline RPCs will be sent.

- Subscription.ReceiveSettings.MinDurationPerAckExtension

This is the minimum amount of time to extend each message's deadline per
ModifyAckDeadline RPC. This is the complement setting of MaxDurationPerAckExtension and
represents the lower bound of modack deadlines to send. If processing time is very
low, it may be better to issue fewer ModifyAckDeadline RPCs rather than every
10 seconds. Setting both Min/MaxDurationPerAckExtension to the same value
effectively removes the automatic derivation of deadlines and fixes it to the value
you wish to extend your messages' deadlines by each time.

- Subscription.ReceiveSettings.MaxOutstandingMessages

This is the maximum number of messages that are to be processed by the callback
function at a time. Once this limit is reached, the client waits for messages
to be acked or nacked by the callback before requesting more messages from the server.

This value is set by default to a fairly conservatively low number. We strongly
encourage setting this number as high as memory allows, since a low setting will
artificially rate limit reception. Setting this value to -1 causes it to be unbounded.

- Subscription.ReceiveSettings.MaxOutstandingBytes

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

Similar to MaxOutstandingMessages, we recommend setting this higher to maximize
processing throughput. Setting this value to -1 causes it to be unbounded.

- Subscription.ReceiveSettings.NumGoroutines

This is the number of goroutines spawned to receive messages from the Pubsub server,
where each goroutine opens a StreamingPull stream. This setting affects the rate of
message intake from server to local buffer.

Setting this value to 1 is sufficient for many workloads. Each stream can handle about
10 MB/s of messages, so if your throughput is under this, set NumGoroutines=1.
Reducing the number of streams can improve the performance by decreasing overhead.
Currently, there is an issue where setting NumGoroutines greater than 1 results in poor
behavior interacting with flow control. Since each StreamingPull stream has its own flow
control, the server-side flow control will not match what is available locally.

Going above 100 streams can lead to increasingly poor behavior, such as acks/modacks not
succeeding in a reasonable amount of time, leading to message expiration. In these cases,
we recommend horizontally scaling by increasing the number of subscriber client applications.

# General tips

Each application should use a single PubSub client instead of creating many.
In addition, when publishing to a single topic, a publisher should be instantiated
once and reused to take advantage of flow control and batching capabilities.
*/
package pubsub // import "cloud.google.com/go/pubsub/v2"
