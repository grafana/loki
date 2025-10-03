# Migrating from Go PubSub v1 to v2

This guide shows how to migrate from the Go PubSub client library v1 version cloud.google.com/go to the v2 version cloud.google.com/go/pubsub/v2.

Note: The code snippets in this guide are meant to be a quick way of comparing the differences between the v1 and v2 packages and **don’t compile as-is**. For a list of all the samples, see the [updated samples](https://cloud.google.com/pubsub/docs/samples).

In line with Google's [OSS Library Breaking Change Policy](https://opensource.google/documentation/policies/library-breaking-change), support for the Go PubSub client library v1 version will continue until July 31st, 2026. This includes continued bug fixes and security patches for v1 version, but no new features would be introduced. We encourage all users to migrate to the Go PubSub client library v2 version before support expires for the earlier v1 version.

## New imports

There are two new packages:

* [cloud.google.com/go/v2](http://cloud.google.com/go/v2): The new main v2 package. 

* [cloud.google.com/go/v2/apiv1/pubsubpb](http://cloud.google.com/go/v2/apiv1/pubsubpb): The auto-generated protobuf Go types that are used as arguments for admin operations. 

For other relevant packages, see Additional References.

## Overview of the migration process

The following is an overview of the migration process. You can find more details about the classes in the later part of this document.

1. Import the new [cloud.google.com/go/v2](http://cloud.google.com/go/v2) package.

2. Migrate admin operations such as `CreateTopic` and `DeleteTopic` to the v2 version admin API.

3. Replace all instances of `Topic()` and `Subscription()` calls with `Publisher()` and `Subscriber()`.

4. Change the data plane client instantiation method. If you previously called `CreateTopic` and used the returned `Topic` to call the `Publish` RPC, you must now instead instantiate a `Publisher` client, and then use that to call `Publish`.

5. Change the subscriber settings that are renamed in the v2 version.

6. Remove references to deprecated settings `Synchronous`, `BufferedByteLimit`, and `UseLegacyFlowControl`.

7. Rename migrated error type: `ErrTopicStopped` to `ErrPublisherStopped`.

## Admin operations

The Pub/Sub admin plane is used to manage Pub/Sub resources like topics, subscriptions, and schemas. These admin operations include `Create`, `Get`, `Update`, `List`, and `Delete`. For subscriptions, seek and snapshots are also part of this layer.

One of the key differences between the v1 and v2 versions is the change to the admin API. Two new clients called `TopicAdminClient` and `SubscriptionAdminClient` are added that handle the admin operations for topics and subscriptions respectively.

For topics and subscriptions, you can access these admin clients as fields of the main client: `pubsub.Client.TopicAdminClient` and `pubsub.Client.SubscriptionAdminClient`. These clients are pre-initialized when calling `pubsub.NewClient`, and takes in the same `ClientOptions` when `NewClient` is called.

There is a mostly one-to-one mapping of existing admin methods to the new admin methods. There are some exceptions that are noted below.

### General RPCs

The new gRPC-based admin client generally takes in Go protobuf types and returns protobuf response types. If you have used other Google Cloud Go libraries like Compute Engine or Secret Manager, the process is similar.

Here is an example comparing a topic creation method in v1 and v2 libraries. In this case, [CreateTopic](https://pkg.go.dev/cloud.google.com/go/pubsub/v2/apiv1#TopicAdminClient.CreateTopic) takes in a generated protobuf type, [pubsubpb.Topic](https://pkg.go.dev/cloud.google.com/go/pubsub/v2/apiv1/pubsubpb#Topic) that is based on the topic defined in [pubsub.proto](https://github.com/googleapis/googleapis/blob/3808680f22d715ef59493e67a6fe82e5ae3e00dd/google/pubsub/v1/pubsub.proto#L678). A key difference here is that the `Name` field of the proto type is the **fully qualified name** for the topic (e.g. `projects/my-project/topics/my-topic`), rather than just the resource ID (e.g. `my-topic`). In addition, specifying this name is part of the [Topic](https://pkg.go.dev/cloud.google.com/go/pubsub/v2/apiv1/pubsubpb#Topic) struct rather than an argument for CreateTopic.

```go
// v1 way to create a topic

import (
	pubsub "cloud.google.com/go/pubsub"
)
...
projectID := "my-project"
topicID := "my-topic"
client, err := pubsub.NewClient(ctx, projectID)

topic, err := client.CreateTopic(ctx, topicID)
```

```go
// v2 way to create a topic
import (
	"cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
)
...
projectID := "my-project"
topicID := "my-topic"
client, err := pubsub.NewClient(ctx, projectID)

topicpb := &pubsubpb.Topic{
	Name: fmt.Sprintf("projects/%s/topics/%s", projectID, topicID),
}
topic, err := client.TopicAdminClient.CreateTopic(ctx, topicpb)
```

The v1 library's `CreateTopicWithConfig` is fully removed. You can specify topic configurations by passing in the fields into [pubsubpb.Topic](https://pkg.go.dev/cloud.google.com/go/pubsub/v2/apiv1/pubsubpb#Topic) while calling `TopicAdminClient.CreateTopic`.

```go
// v1 way to create a topic with settings

import (
	pubsub "cloud.google.com/go/pubsub"
)
...
projectID := "my-project"
topicID := "my-topic"
client, err := pubsub.NewClient(ctx, projectID)

// Create a new topic with the given name and config.
topicConfig := &pubsub.TopicConfig{
	RetentionDuration: 24 * time.Hour,
	MessageStoragePolicy: pubsub.MessageStoragePolicy{
		AllowedPersistenceRegions: []string{"us-east1"},
	},
}
topic, err := client.CreateTopicWithConfig(ctx, "topicName", topicConfig)
```

```go
// v2 way to create a topic with settings
import (
	"cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
)
...
projectID := "my-project"
topicID := "my-topic"
client, err := pubsub.NewClient(ctx, projectID)

topicpb := &pubsubpb.Topic{
	Name: fmt.Sprintf("projects/%s/topics/%s", projectID, topicID),
	MessageRetentionDuration: durationpb.New(24 * time.Hour),
	MessageStoragePolicy: &pubsubpb.MessageStoragePolicy{
		AllowedPersistenceRegions: []string{"us-central1"},
	},
}
topic, err := client.TopicAdminClient.CreateTopic(ctx, topicpb)
```

For code that creates a subscription, the migration process is similar to the topic creation method. Use the `pubsubpb.Subscription` type and `SubscriptionAdminClient.CreateSubscription` method.

```go
s := &pubsubpb.Subscription{
	Name: fmt.Sprintf("projects/%s/subscriptions/%s", projectID, subID),
}
topic, err := client.SubscriptionAdminClient.CreateSubscription(ctx, s)
```

The [new proto types](https://pkg.go.dev/cloud.google.com/go/pubsub/v2/apiv1/pubsubpb) and their fields might differ slightly from the current v1 version types. The new types are based on the Pub/Sub proto. Here are some of those differences:

* In the `CreateTopic` example shown in an earlier part of this guide, the message retention duration is defined as `RetentionDuration` in the v1 as a Go duration, but in the v2 version it is `MessageRetentionDuration` of type [durationpb.Duration](https://pkg.go.dev/google.golang.org/protobuf/types/known/durationpb#hdr-Conversion_from_a_Go_Duration).

* Generated protobuf code doesn't follow Go styling guides for initialisms. For example, `KMSKeyName` is defined as `KmsKeyName` in the v2 version.

* The v1 version uses custom optional types for certain fields for durations and boolean values. In the v2, `time.Duration` fields are defined by a protobuf specific [durationpb.Duration](https://pkg.go.dev/google.golang.org/protobuf/types/known/durationpb). Optional booleans now use Go boolean values directly.

```go
// V2 subscription of initializing a subscription with configuration.
s := &pubsubpb.Subscription{
	Name: fmt.Sprintf("projects/%s/subscriptions/%s", projectID, subID),
	TopicMessageRetentionDuration: durationpb.New(1 * time.Hour),
	EnableExactlyOnceDelivery: true,
}
topic, err := client.SubscriptionAdminClient.CreateSubscription(ctx, s)
```

For more information, see the method calls and arguments defined by the [new clients](https://pkg.go.dev/cloud.google.com/go/pubsub/v2/apiv1) and [Go protobuf types](https://pkg.go.dev/cloud.google.com/go/pubsub/v2/apiv1/pubsubpb).

### Delete RPCs

Let’s look at the differences for another operation: DeleteTopic.

```go
// v1 way to delete a topic
import (
	pubsub "cloud.google.com/go/pubsub"
)
...
projectID := "my-project"
topicID := "my-topic"
client, err := pubsub.NewClient(ctx, projectID)

topic := client.Topic(topicID)
topic.Delete(ctx)
```

```go
// v2 way to delete a topic
import (
	"cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
)
...
projectID := "my-project"
topicID := "my-topic"
client, err := pubsub.NewClient(ctx, projectID)

req := &pubsubpb.DeleteTopicRequest{
	Topic: fmt.Sprintf("projects/%s/topics/%s", projectID, topicID),
}
client.TopicAdminClient.DeleteTopic(ctx, req)
```

In this case, you have to instantiate a `DeleteTopicRequest` struct and pass that into the `DeleteTopic` call. This includes specifying the **full path** of the topic, which includes the project ID, instead of just the topic ID.

### Update RPCs

When trying to update resources, you will need to declare the new object you are modifying by creating a proto object, and explicitly defining the field name.

You may need to specify a [FieldMask protobuf type](https://pkg.go.dev/google.golang.org/protobuf/types/known/fieldmaskpb) along with the resource you are modifying if you only want to edit specific fields and leave the others the same. The strings to pass into the update field mask must be the name of the field of the resource you are editing, written in `snake_case` (such as `enable_exactly_once_delivery` or `message_storage_policy`). These must match the field names in the [resource message definition in proto](https://github.com/googleapis/googleapis/blob/master/google/pubsub/v1/pubsub.proto).

If a field mask is not present on update, the operation applies to all fields (as if a field mask of all fields has been specified) and overrides the entire resource.

```go
// v1 way to update subscriptions
projectID := "my-project"
subID := "my-subscription"
client, err := pubsub.NewClient(ctx, projectID)

cfg := pubsub.SubscriptionConfigToUpdate{EnableExactlyOnceDelivery: true}
subConfig, err := client.Subscription(subID).Update(ctx, cfg)
```

```go
// v2 way to update subscriptions
import (
	"cloud.google.com/go/pubsub/v2"
	pb "cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

projectID := "my-project"
subID := "my-subscription"
client, err := pubsub.NewClient(ctx, projectID)
updateReq := &pb.UpdateSubscriptionRequest{
	Subscription: &pb.Subscription{
		Name: fmt.Sprintf("projects/%s/subscriptions/%s", projectID, subID),
		EnableExactlyOnceDelivery: true
	},
	UpdateMask: &fieldmaskpb.FieldMask{
		Paths: []string{"enable_exactly_once_delivery"},
	},
}
sub, err := client.SubscriptionAdminClient.UpdateSubscription(ctx, updateReq)
```

### Exists method removed

The `Exists` methods for topic, subscription, and schema are removed in the v2 version. You can check if a resource exists by performing a Get call: (e.g. `GetTopic`). 

For publishing and subscribing, we recommend following the pattern of [optimistically expecting a resource to exist](https://cloud.google.com/pubsub/docs/samples/pubsub-optimistic-subscribe#pubsub_optimistic_subscribe-go) and then handling the `NOT_FOUND` error, which saves a network call if the resource does exist.

### RPCs involving one-of fields

RPCs that include one-of fields require instantiating specific Go generated protobuf structs that satisfy the interface type. This may involve generating structs that look duplicated. This is because in the generated code, the outer struct is the interface that satisfies the one-of condition while the inner struct is a wrapper around the actual one-of.

Let’s look at an example:

```go
// v1 way to create topic ingestion from kinesis

import (
	"cloud.google.com/go/pubsub"
)
...
cfg := &pubsub.TopicConfig{
	IngestionDataSourceSettings: &pubsub.IngestionDataSourceSettings{
		Source: &pubsub.IngestionDataSourceAWSKinesis{
			StreamARN:         streamARN,
			ConsumerARN:       consumerARN,
			AWSRoleARN:        awsRoleARN,
			GCPServiceAccount: gcpServiceAccount,
		},
	},
}

topic, err := client.CreateTopicWithConfig(ctx, topicID, cfg)
```

```go
// v2 way to create topic ingestion from kinesis

import (
	"cloud.google.com/go/pubsub/v2"
	pb "cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
)
...
topicpb := &pb.Topic{
	IngestionDataSourceSettings: &pb.IngestionDataSourceSettings{
		Source: &pb.IngestionDataSourceSettings_AwsKinesis_{
			AwsKinesis: &pb.IngestionDataSourceSettings_AwsKinesis{
				StreamArn:         streamARN,
				ConsumerArn:       consumerARN,
				AwsRoleArn:        awsRoleARN,
				GcpServiceAccount: gcpServiceAccount,
			},
		},
	},
}

topic, err := client.TopicAdminClient.CreateTopic(ctx, topicpb)
```

In the above example, `IngestionDataSourceSettings_AwsKinesis_` is a wrapper struct around `IngestionDataSourceSettings_AwsKinesis`. The former satisfies the interface type of being an ingestion data source, while the latter contains the actual fields of the settings.

Another example of an instantiation is with [Single Message Transforms](https://cloud.google.com/pubsub/docs/smts/smts-overview).

```go
import (
	"cloud.google.com/go/pubsub"
)
projectID := "my-project"
topicID := "my-topic"
client, err := pubsub.NewClient(ctx, projectID)
...

code := `function redactSSN(message, metadata) {...}`
transform := pubsub.MessageTransform{
	Transform: pubsub.JavaScriptUDF{
		FunctionName: "redactSSN",
		Code:         code,
	},
}
cfg := &pubsub.TopicConfig{
	MessageTransforms: []pubsub.MessageTransform{transform},
}
t, err := client.CreateTopicWithConfig(ctx, topicID, cfg)
```

```go
import (
	"cloud.google.com/go/pubsub/v2"
	pb "cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
)
...
projectID := "my-project"
topicID := "my-topic"
client, err := pubsub.NewClient(ctx, projectID)

code := `function redactSSN(message, metadata) {...}`
transform := pb.MessageTransform{
	Transform: &pb.MessageTransform_JavascriptUdf{
		JavascruptUdf: &pb.JavascriptUDF {
			FunctionName: "redactSSN",
			Code: 		  code,
		},
	},
}

topicpb := &pb.Topic{
	Name: fmt.Sprintf("projects/%s/topics/%s", projectID, topicID),
	MessageTransforms: []*pb.MessageTransform{transform},
}
topic, err := client.TopicAdminClient.CreateTopic(ctx, topicpb)
```

In this case, `MessageTransform_JavascriptUdf` satisfies the interface, while `JavascriptUdf` holds the actual strings relevant for the message transform.

### Seek / snapshots

Seek and snapshot RPCs are also part of the admin layer. Use the [SubscriptionAdminClient](https://pkg.go.dev/cloud.google.com/go/pubsub/v2/apiv1#SubscriptionAdminClient) to Seek to specific time or snapshot.

```go
// v2 way to call seek on a subscription

import (
	"cloud.google.com/go/pubsub/v2"
	"google.golang.org/protobuf/types/known/timestamppb"
	pb "cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
)
...
projectID := "my-project-id"
subscriptionID := "my-subscription-id"

now := time.Now()

client, err := pubsub.NewClient(ctx, projectID)
...
client.SubscriptionAdminClient.Seek(ctx, &pb.SeekRequest{
	Subscription: fmt.Sprintf("projects/%s/subscriptions/%s", projectID, subID),
	Target: &pb.SeekRequest_Time{
		Time: timestamppb.New(now),
	},
})
```

### Call Options (retries and timeouts)

In the v2, [pubsub.NewClientWithConfig](https://pkg.go.dev/cloud.google.com/go/pubsub#NewClientWithConfig) is still the correct method to invoke to add RPC specific retries and timeouts. However, the helper struct is renamed from `ClientConfig.PublisherCallOptions`  to `TopicAdminCallOptions`. The same is true for Subscription calls, which is now named `SubscriptionAdminCallOptions.`

```go
// Simplified v2 code
import (
	opts "cloud.google.com/go/pubsub/v2/apiv1"
	"cloud.google.com/go/pubsub/v2"
)

tco := &opts.TopicAdminCallOptions{
	CreateTopic: []gax.CallOption{
		gax.WithRetry(func() gax.Retryer {
			return gax.OnCodes([]codes.Code{
				codes.Unavailable,
			}, gax.Backoff{
				Initial:    200 * time.Millisecond,
				Max:        30000 * time.Millisecond,
				Multiplier: 1.25,
			})
		}),
	},
}

client, err := NewClientWithConfig(ctx, "my-project", &ClientConfig{
	TopicAdminCallOptions: tco,
},
defer client.Close()
```

## Schemas

The existing `Schema` client is replaced by a new `SchemaClient`, which behaves similarly to the topic and subscription admin clients in the new v2 version. Since schemas are less commonly used than publishing and subscribing, the Pub/Sub client does not preinitialize these for you. Instead, you must call the `NewSchemaClient` method in [cloud.google.com/go/pubsub/v2/apiv1](http://cloud.google.com/go/pubsub/v2/apiv1).

```go
// Simplified v2 code
import (
	pubsub "cloud.google.com/go/pubsub/v2/apiv1"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
)
...

projectID := "my-project-id"
schemaID := "my-schema"
ctx := context.Background()
client, err := pubsub.NewSchemaClient(ctx)
if err != nil {
	return fmt.Errorf("pubsub.NewSchemaClient: %w", err)
}
defer client.Close()

req := &pubsubpb.GetSchemaRequest{
	Name: fmt.Sprintf("projects/%s/schemas/%s", projectID, schemaID),
	View: pubsubpb.SchemaView_FULL,
}
s, err := client.GetSchema(ctx, req)

```

The main difference with the new auto generated schema client is that you cannot pass in a project ID at client instantiation. Instead, all references to schemas are done by its fully qualified resource name (such as `projects/my-project/schemas/my-schema`).

## Data plane operations

In contrast with admin operations that deal with resource management, the data plane deals with **publishing** and **receiving** messages.

In the current v1 version, the data plane clients are intermixed with the admin plane structs: [Topic](https://pkg.go.dev/cloud.google.com/go/pubsub#Topic) and [Subscription](https://pkg.go.dev/cloud.google.com/go/pubsub#Subscription). For example, the `Topic` struct has the [Publish](https://pkg.go.dev/cloud.google.com/go/pubsub#Topic.Publish) method.

```go
// Simplified v1 code
client, err := pubsub.NewClient(ctx, projectID)
...
topic := client.Topic("my-topic")
topic.Publish(ctx, "message")
```

In the v2 version, replace `Topic` with `Publisher` to publish messages.

```go
// Simplified v2 code
client, err := pubsub.NewClient(ctx, projectID)
...
publisher := client.Publisher("my-topic")
publisher.Publish(ctx, "message")
```

Similarly, the v1 version Subscription has [Receive](https://pkg.go.dev/cloud.google.com/go/pubsub#Subscription.Receive) for pulling messages. Replace `Subscription` with `Subscriber` to pull messages.

```go
// Simplified v2 code
client, err := pubsub.NewClient(ctx, projectID)
...
subscriber := client.Subscriber("my-subscription")
subscriber.Receive(ctx, ...)
```

### Instantiation from admin

In the v1 version, it is possible to call `CreateTopic` to create a topic and then call `Publish` on the returned topic. Since the v2 version `CreateTopic` returns a generated protobuf [topic](https://pkg.go.dev/cloud.google.com/go/pubsub/v2/apiv1/pubsubpb#Topic) that doesn’t have a `Publish` method, you must instantiate your own `Publisher` client to publish messages.

```go
// Simplified v2 code
client, err := pubsub.NewClient(ctx, projectID)
...

topicpb := &pb.Topic{
	Name: fmt.Sprintf("projects/%s/topics/%s", projectID, topicID),
}
topic, err := client.TopicAdminClient.CreateTopic(ctx, topicpb)

// Instantiate the publisher from the topic name.
publisher := client.Publisher(topic.GetName())
publisher.Publish(ctx, "message")
```

### TopicInProject and SubscriptionInProject removed

To make this transition easier, the Publisher and Subscriber methods can take in either the resource ID (such as `my-topic`) or a fully qualified name (such as `projects/p/topics/topic`) as arguments. This makes it easier to use the fully qualified topic name (accessible through `topic.GetName())` rather than needing to parse out just the resource ID. If you use the resource ID, the publisher and subscriber clients assume you are referring to the project ID defined when instantiating the base pubsub client.

The previous `TopicInProject` and `SubscriptionInProject` methods are removed from the v2 version. To create a publisher or subscriber in a different project, use the fully qualified name like in the sample above.

### Renamed settings

Two subscriber flow control settings are renamed:

* `MinExtensionPeriod` → `MinDurationPerAckExtension`

* `MaxExtensionPeriod` → `MaxDurationPerAckExtension`

### Default settings changes

To align with other client libraries, we will be changing the default value for `ReceiveSettings.NumGoroutines` to 1\. This is a better default for most users as each stream can handle 10 MB/s and will reduce the number of idle streams for lower throughput applications.

### Removed settings

`PublishSettings.BufferedByteLimit` is removed. This was already superseded by the existing `PublishSettings.MaxOutstandingBytes`.

`ReceiveSettings.Synchronous` used to make the library use the synchronous `Pull` API for the mechanism to receive messages, but we are requiring only using the StreamingPull API in the v2.

Lastly, we will be removing `ReceiveSettings.UseLegacyFlowControl`, since server side flow control is now a mature feature and should be relied upon for managing flow control.

### Renamed Error Type

Because of the change to the data plane clients (now named `Publisher` and `Subscriber)`, we renamed one error type to match this. `ErrTopicStopped` is now `ErrPublisherStopped`.

## Relevant packages

* [cloud.google.com/go/pubsub/v2](http://cloud.google.com/go/pubsub/v2) is the base v2 package.

* [cloud.google.com/go/pubsub/v2/apiv1](http://cloud.google.com/go/pubsub/v2/apiv1) is used for initializing SchemaClient.

* [cloud.google.com/go/pubsub/v2/apiv1/pubsubpb](http://cloud.google.com/go/pubsub/v2/apiv1/pubsubpb) is used for creating admin protobuf requests.

* [cloud.google.com/go/iam/apiv1/iampb](http://cloud.google.com/go/iam/apiv1/iampb) is used for IAM requests.

* [google.golang.org/protobuf/types/known/durationpb](http://google.golang.org/protobuf/types/known/durationpb) is used for proto duration type in place of Go duration.

* [google.golang.org/protobuf/types/known/fieldmaskpb](http://google.golang.org/protobuf/types/known/fieldmaskpb) is used for masking which fields are updated in update calls.

## FAQ

**Q: Why does the new admin API package mention both v2 and apiv1?**

The new Pub/Sub v2 package is `cloud.google.com/go/v2`. All of the new v2 code lives in the v2 directory. The apiv1 version denotes that the Pub/Sub server API is still under v1 and is **not** changing.

**Q: Why are you changing the admin API surface?**

One goal we had for this new Pub/Sub package is to reduce confusion between the data and admin plane surfaces. Particularly, the way that this package references topics and subscriptions was inconsistent with other Pub/Sub libraries in other languages. For example, creating a topic does not automatically create a publisher client in the Java or Python client libraries. Instead, we want it to be clear that creating a topic is a server side operation and creating a publisher client is a client operation.

In the past, we have seen users be confused about why setting topic.PublishSettings doesn't persist the settings across applications. This is because we are actually setting the ephemeral PublishSettings of the client, which isn't saved to the server.

Another goal is to improve development velocity by leveraging our auto generation tools that already exist for other Go products. With this change, changes that only affect the admin plane (including recent features such as topic ingestion settings and export subscriptions) can be released sooner.
