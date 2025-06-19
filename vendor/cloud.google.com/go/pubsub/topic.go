// Copyright 2016 Google LLC
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

package pubsub

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/iam"
	"cloud.google.com/go/internal/optional"
	ipubsub "cloud.google.com/go/internal/pubsub"
	vkit "cloud.google.com/go/pubsub/apiv1"
	pb "cloud.google.com/go/pubsub/apiv1/pubsubpb"
	"cloud.google.com/go/pubsub/internal/scheduler"
	gax "github.com/googleapis/gax-go/v2"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/api/support/bundler"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	fmpb "google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// MaxPublishRequestCount is the maximum number of messages that can be in
	// a single publish request, as defined by the PubSub service.
	MaxPublishRequestCount = 1000

	// MaxPublishRequestBytes is the maximum size of a single publish request
	// in bytes, as defined by the PubSub service.
	MaxPublishRequestBytes = 1e7
)

const (
	// TODO: math.MaxInt was added in Go 1.17. We should use that once 1.17
	// becomes the minimum supported version of Go.
	intSize = 32 << (^uint(0) >> 63)
	maxInt  = 1<<(intSize-1) - 1
)

// ErrOversizedMessage indicates that a message's size exceeds MaxPublishRequestBytes.
var ErrOversizedMessage = bundler.ErrOversizedItem

// Topic is a reference to a PubSub topic.
//
// The methods of Topic are safe for use by multiple goroutines.
type Topic struct {
	c *Client
	// The fully qualified identifier for the topic, in the format "projects/<projid>/topics/<name>"
	name string

	// Settings for publishing messages. All changes must be made before the
	// first call to Publish. The default is DefaultPublishSettings.
	PublishSettings PublishSettings

	mu        sync.RWMutex
	stopped   bool
	scheduler *scheduler.PublishScheduler

	flowController

	// EnableMessageOrdering enables delivery of ordered keys.
	EnableMessageOrdering bool

	// enableTracing enables OTel tracing of Pub/Sub messages on this topic.
	// This is configured at client instantiation, and allows
	// disabling tracing even when a tracer provider is detectd.
	enableTracing bool
}

// PublishSettings control the bundling of published messages.
type PublishSettings struct {

	// Publish a non-empty batch after this delay has passed.
	DelayThreshold time.Duration

	// Publish a batch when it has this many messages. The maximum is
	// MaxPublishRequestCount.
	CountThreshold int

	// Publish a batch when its size in bytes reaches this value.
	ByteThreshold int

	// The number of goroutines used in each of the data structures that are
	// involved along the the Publish path. Adjusting this value adjusts
	// concurrency along the publish path.
	//
	// Defaults to a multiple of GOMAXPROCS.
	NumGoroutines int

	// The maximum time that the client will attempt to publish a bundle of messages.
	Timeout time.Duration

	// The maximum number of bytes that the Bundler will keep in memory before
	// returning ErrOverflow. This is now superseded by FlowControlSettings.MaxOutstandingBytes.
	// If MaxOutstandingBytes is set, that value will override BufferedByteLimit.
	//
	// Defaults to DefaultPublishSettings.BufferedByteLimit.
	// Deprecated: Set `Topic.PublishSettings.FlowControlSettings.MaxOutstandingBytes` instead.
	BufferedByteLimit int

	// FlowControlSettings defines publisher flow control settings.
	FlowControlSettings FlowControlSettings

	// EnableCompression enables transport compression for Publish operations
	EnableCompression bool

	// CompressionBytesThreshold defines the threshold (in bytes) above which messages
	// are compressed for transport. Only takes effect if EnableCompression is true.
	CompressionBytesThreshold int
}

func (ps *PublishSettings) shouldCompress(batchSize int) bool {
	return ps.EnableCompression && batchSize > ps.CompressionBytesThreshold
}

// DefaultPublishSettings holds the default values for topics' PublishSettings.
var DefaultPublishSettings = PublishSettings{
	DelayThreshold: 10 * time.Millisecond,
	CountThreshold: 100,
	ByteThreshold:  1e6,
	Timeout:        60 * time.Second,
	// By default, limit the bundler to 10 times the max message size. The number 10 is
	// chosen as a reasonable amount of messages in the worst case whilst still
	// capping the number to a low enough value to not OOM users.
	BufferedByteLimit: 10 * MaxPublishRequestBytes,
	FlowControlSettings: FlowControlSettings{
		MaxOutstandingMessages: 1000,
		MaxOutstandingBytes:    -1,
		LimitExceededBehavior:  FlowControlIgnore,
	},
	// Publisher compression defaults matches Java's defaults
	// https://github.com/googleapis/java-pubsub/blob/7d33e7891db1b2e32fd523d7655b6c11ea140a8b/google-cloud-pubsub/src/main/java/com/google/cloud/pubsub/v1/Publisher.java#L717-L718
	EnableCompression:         false,
	CompressionBytesThreshold: 240,
}

// CreateTopic creates a new topic.
//
// The specified topic ID must start with a letter, and contain only letters
// ([A-Za-z]), numbers ([0-9]), dashes (-), underscores (_), periods (.),
// tildes (~), plus (+) or percent signs (%). It must be between 3 and 255
// characters in length, and must not start with "goog". For more information,
// see: https://cloud.google.com/pubsub/docs/admin#resource_names
//
// If the topic already exists an error will be returned.
func (c *Client) CreateTopic(ctx context.Context, topicID string) (*Topic, error) {
	t := c.Topic(topicID)
	_, err := c.pubc.CreateTopic(ctx, &pb.Topic{Name: t.name})
	if err != nil {
		return nil, err
	}
	return t, nil
}

// CreateTopicWithConfig creates a topic from TopicConfig.
//
// The specified topic ID must start with a letter, and contain only letters
// ([A-Za-z]), numbers ([0-9]), dashes (-), underscores (_), periods (.),
// tildes (~), plus (+) or percent signs (%). It must be between 3 and 255
// characters in length, and must not start with "goog". For more information,
// see: https://cloud.google.com/pubsub/docs/admin#resource_names.
//
// If the topic already exists, an error will be returned.
func (c *Client) CreateTopicWithConfig(ctx context.Context, topicID string, tc *TopicConfig) (*Topic, error) {
	t := c.Topic(topicID)
	topic := tc.toProto()
	topic.Name = t.name
	_, err := c.pubc.CreateTopic(ctx, topic)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// Topic creates a reference to a topic in the client's project.
//
// If a Topic's Publish method is called, it has background goroutines
// associated with it. Clean them up by calling Topic.Stop.
//
// Avoid creating many Topic instances if you use them to publish.
func (c *Client) Topic(id string) *Topic {
	return c.TopicInProject(id, c.projectID)
}

// TopicInProject creates a reference to a topic in the given project.
//
// If a Topic's Publish method is called, it has background goroutines
// associated with it. Clean them up by calling Topic.Stop.
//
// Avoid creating many Topic instances if you use them to publish.
func (c *Client) TopicInProject(id, projectID string) *Topic {
	return newTopic(c, fmt.Sprintf("projects/%s/topics/%s", projectID, id))
}

func newTopic(c *Client, name string) *Topic {
	return &Topic{
		c:               c,
		name:            name,
		PublishSettings: DefaultPublishSettings,
		enableTracing:   c.enableTracing,
	}
}

// TopicState denotes the possible states for a topic.
type TopicState int

const (
	// TopicStateUnspecified is the default value. This value is unused.
	TopicStateUnspecified = iota

	// TopicStateActive means the topic does not have any persistent errors.
	TopicStateActive

	// TopicStateIngestionResourceError means ingestion from the data source
	// has encountered a permanent error.
	// See the more detailed error state in the corresponding ingestion
	// source configuration.
	TopicStateIngestionResourceError
)

// TopicConfig describes the configuration of a topic.
type TopicConfig struct {
	// The fully qualified identifier for the topic, in the format "projects/<projid>/topics/<name>"
	name string

	// The set of labels for the topic.
	Labels map[string]string

	// The topic's message storage policy.
	MessageStoragePolicy MessageStoragePolicy

	// The name of the Cloud KMS key to be used to protect access to messages
	// published to this topic, in the format
	// "projects/P/locations/L/keyRings/R/cryptoKeys/K".
	KMSKeyName string

	// Schema defines the schema settings upon topic creation.
	SchemaSettings *SchemaSettings

	// RetentionDuration configures the minimum duration to retain a message
	// after it is published to the topic. If this field is set, messages published
	// to the topic in the last `RetentionDuration` are always available to subscribers.
	// For instance, it allows any attached subscription to [seek to a
	// timestamp](https://cloud.google.com/pubsub/docs/replay-overview#seek_to_a_time)
	// that is up to `RetentionDuration` in the past. If this field is
	// not set, message retention is controlled by settings on individual
	// subscriptions. Cannot be more than 31 days or less than 10 minutes.
	//
	// For more information, see https://cloud.google.com/pubsub/docs/replay-overview#topic_message_retention.
	RetentionDuration optional.Duration

	// State is an output-only field indicating the state of the topic.
	State TopicState

	// IngestionDataSourceSettings are settings for ingestion from a
	// data source into this topic.
	IngestionDataSourceSettings *IngestionDataSourceSettings

	// MessageTransforms are the transforms to be applied to messages published to the topic.
	// Transforms are applied in the order specified.
	MessageTransforms []MessageTransform
}

// String returns the printable globally unique name for the topic config.
// This method only works when the topic config is returned from the server,
// such as when calling `client.Topic` or `client.Topics`.
// Otherwise, this will return an empty string.
func (t *TopicConfig) String() string {
	return t.name
}

// ID returns the unique identifier of the topic within its project.
// This method only works when the topic config is returned from the server,
// such as when calling `client.Topic` or `client.Topics`.
// Otherwise, this will return an empty string.
func (t *TopicConfig) ID() string {
	slash := strings.LastIndex(t.name, "/")
	if slash == -1 {
		return ""
	}
	return t.name[slash+1:]
}

func (tc *TopicConfig) toProto() *pb.Topic {
	var retDur *durationpb.Duration
	if tc.RetentionDuration != nil {
		retDur = durationpb.New(optional.ToDuration(tc.RetentionDuration))
	}
	pbt := &pb.Topic{
		Labels:                      tc.Labels,
		MessageStoragePolicy:        messageStoragePolicyToProto(&tc.MessageStoragePolicy),
		KmsKeyName:                  tc.KMSKeyName,
		SchemaSettings:              schemaSettingsToProto(tc.SchemaSettings),
		MessageRetentionDuration:    retDur,
		IngestionDataSourceSettings: tc.IngestionDataSourceSettings.toProto(),
		MessageTransforms:           messageTransformsToProto(tc.MessageTransforms),
	}
	return pbt
}

// TopicConfigToUpdate describes how to update a topic.
type TopicConfigToUpdate struct {
	// If non-nil, the current set of labels is completely
	// replaced by the new set.
	Labels map[string]string

	// If non-nil, the existing policy (containing the list of regions)
	// is completely replaced by the new policy.
	//
	// Use the zero value &MessageStoragePolicy{} to reset the topic back to
	// using the organization's Resource Location Restriction policy.
	//
	// If nil, the policy remains unchanged.
	//
	// This field has beta status. It is not subject to the stability guarantee
	// and may change.
	MessageStoragePolicy *MessageStoragePolicy

	// If set to a positive duration between 10 minutes and 31 days, RetentionDuration is changed.
	// If set to a negative value, this clears RetentionDuration from the topic.
	// If nil, the retention duration remains unchanged.
	RetentionDuration optional.Duration

	// Schema defines the schema settings upon topic creation.
	//
	// Use the zero value &SchemaSettings{} to remove the schema from the topic.
	SchemaSettings *SchemaSettings

	// IngestionDataSourceSettings are settings for ingestion from a
	// data source into this topic.
	//
	// When changing this value, the entire data source settings object must be applied,
	// rather than just the differences. This includes the source and logging settings.
	//
	// Use the zero value &IngestionDataSourceSettings{} to remove the ingestion settings from the topic.
	IngestionDataSourceSettings *IngestionDataSourceSettings

	// If non-nil, the entire list of message transforms is replaced with the following.
	MessageTransforms []MessageTransform
}

func protoToTopicConfig(pbt *pb.Topic) TopicConfig {
	tc := TopicConfig{
		name:                        pbt.Name,
		Labels:                      pbt.Labels,
		MessageStoragePolicy:        protoToMessageStoragePolicy(pbt.MessageStoragePolicy),
		KMSKeyName:                  pbt.KmsKeyName,
		SchemaSettings:              protoToSchemaSettings(pbt.SchemaSettings),
		State:                       TopicState(pbt.State),
		IngestionDataSourceSettings: protoToIngestionDataSourceSettings(pbt.IngestionDataSourceSettings),
		MessageTransforms:           protoToMessageTransforms(pbt.MessageTransforms),
	}
	if pbt.GetMessageRetentionDuration() != nil {
		tc.RetentionDuration = pbt.GetMessageRetentionDuration().AsDuration()
	}
	return tc
}

// DetachSubscriptionResult is the response for the DetachSubscription method.
// Reserved for future use.
type DetachSubscriptionResult struct{}

// DetachSubscription detaches a subscription from its topic. All messages
// retained in the subscription are dropped. Subsequent `Pull` and `StreamingPull`
// requests will return FAILED_PRECONDITION. If the subscription is a push
// subscription, pushes to the endpoint will stop.
func (c *Client) DetachSubscription(ctx context.Context, sub string) (*DetachSubscriptionResult, error) {
	_, err := c.pubc.DetachSubscription(ctx, &pb.DetachSubscriptionRequest{
		Subscription: sub,
	})
	if err != nil {
		return nil, err
	}
	return &DetachSubscriptionResult{}, nil
}

// MessageStoragePolicy constrains how messages published to the topic may be stored. It
// is determined when the topic is created based on the policy configured at
// the project level.
type MessageStoragePolicy struct {
	// AllowedPersistenceRegions is the list of GCP regions where messages that are published
	// to the topic may be persisted in storage. Messages published by publishers running in
	// non-allowed GCP regions (or running outside of GCP altogether) will be
	// routed for storage in one of the allowed regions.
	//
	// If empty, it indicates a misconfiguration at the project or organization level, which
	// will result in all Publish operations failing. This field cannot be empty in updates.
	//
	// If nil, then the policy is not defined on a topic level. When used in updates, it resets
	// the regions back to the organization level Resource Location Restriction policy.
	//
	// For more information, see
	// https://cloud.google.com/pubsub/docs/resource-location-restriction#pubsub-storage-locations.
	AllowedPersistenceRegions []string
}

func protoToMessageStoragePolicy(msp *pb.MessageStoragePolicy) MessageStoragePolicy {
	if msp == nil {
		return MessageStoragePolicy{}
	}
	return MessageStoragePolicy{AllowedPersistenceRegions: msp.AllowedPersistenceRegions}
}

func messageStoragePolicyToProto(msp *MessageStoragePolicy) *pb.MessageStoragePolicy {
	if msp == nil || msp.AllowedPersistenceRegions == nil {
		return nil
	}
	return &pb.MessageStoragePolicy{AllowedPersistenceRegions: msp.AllowedPersistenceRegions}
}

// IngestionDataSourceSettings enables ingestion from a data source into this topic.
type IngestionDataSourceSettings struct {
	Source IngestionDataSource

	PlatformLogsSettings *PlatformLogsSettings
}

// IngestionDataSource is the kind of ingestion source to be used.
type IngestionDataSource interface {
	isIngestionDataSource() bool
}

// AWSKinesisState denotes the possible states for ingestion from Amazon Kinesis Data Streams.
type AWSKinesisState int

const (
	// AWSKinesisStateUnspecified is the default value. This value is unused.
	AWSKinesisStateUnspecified = iota

	// AWSKinesisStateActive means ingestion is active.
	AWSKinesisStateActive

	// AWSKinesisStatePermissionDenied means encountering an error while consumign data from Kinesis.
	// This can happen if:
	//   - The provided `aws_role_arn` does not exist or does not have the
	//     appropriate permissions attached.
	//   - The provided `aws_role_arn` is not set up properly for Identity
	//     Federation using `gcp_service_account`.
	//   - The Pub/Sub SA is not granted the
	//     `iam.serviceAccounts.getOpenIdToken` permission on
	//     `gcp_service_account`.
	AWSKinesisStatePermissionDenied

	// AWSKinesisStatePublishPermissionDenied means permission denied encountered while publishing to the topic.
	// This can happen due to Pub/Sub SA has not been granted the appropriate publish
	// permissions https://cloud.google.com/pubsub/docs/access-control#pubsub.publisher
	AWSKinesisStatePublishPermissionDenied

	// AWSKinesisStateStreamNotFound means the Kinesis stream does not exist.
	AWSKinesisStateStreamNotFound

	// AWSKinesisStateConsumerNotFound means the Kinesis consumer does not exist.
	AWSKinesisStateConsumerNotFound
)

// IngestionDataSourceAWSKinesis are ingestion settings for Amazon Kinesis Data Streams.
type IngestionDataSourceAWSKinesis struct {
	// State is an output-only field indicating the state of the kinesis connection.
	State AWSKinesisState

	// StreamARN is the Kinesis stream ARN to ingest data from.
	StreamARN string

	// ConsumerARn is the Kinesis consumer ARN to used for ingestion in Enhanced
	// Fan-Out mode. The consumer must be already created and ready to be used.
	ConsumerARN string

	// AWSRoleARn is the AWS role ARN to be used for Federated Identity authentication
	// with Kinesis. Check the Pub/Sub docs for how to set up this role and the
	// required permissions that need to be attached to it.
	AWSRoleARN string

	// GCPServiceAccount is the GCP service account to be used for Federated Identity
	// authentication with Kinesis (via a `AssumeRoleWithWebIdentity` call for
	// the provided role). The `aws_role_arn` must be set up with
	// `accounts.google.com:sub` equals to this service account number.
	GCPServiceAccount string
}

var _ IngestionDataSource = (*IngestionDataSourceAWSKinesis)(nil)

func (i *IngestionDataSourceAWSKinesis) isIngestionDataSource() bool {
	return true
}

// CloudStorageIngestionState denotes the possible states for ingestion from Cloud Storage.
type CloudStorageIngestionState int

const (
	// CloudStorageIngestionStateUnspecified is the default value. This value is unused.
	CloudStorageIngestionStateUnspecified = iota

	// CloudStorageIngestionStateActive means ingestion is active.
	CloudStorageIngestionStateActive

	// CloudStorageIngestionPermissionDenied means encountering an error while calling the Cloud Storage API.
	// This can happen if the Pub/Sub SA has not been granted the
	// [appropriate permissions](https://cloud.google.com/storage/docs/access-control/iam-permissions):
	// - storage.objects.list: to list the objects in a bucket.
	// - storage.objects.get: to read the objects in a bucket.
	// - storage.buckets.get: to verify the bucket exists.
	CloudStorageIngestionPermissionDenied

	// CloudStorageIngestionPublishPermissionDenied means encountering an error when publishing to the topic.
	// This can happen if the Pub/Sub SA has not been granted the [appropriate publish
	// permissions](https://cloud.google.com/pubsub/docs/access-control#pubsub.publisher)
	CloudStorageIngestionPublishPermissionDenied

	// CloudStorageIngestionBucketNotFound means the provided bucket doesn't exist.
	CloudStorageIngestionBucketNotFound

	// CloudStorageIngestionTooManyObjects means the bucket has too many objects, ingestion will be paused.
	CloudStorageIngestionTooManyObjects
)

// IngestionDataSourceCloudStorage are ingestion settings for Cloud Storage.
type IngestionDataSourceCloudStorage struct {
	// State is an output-only field indicating the state of the Cloud storage ingestion source.
	State CloudStorageIngestionState

	// Bucket is the Cloud Storage bucket. The bucket name must be without any
	// prefix like "gs://". See the bucket naming requirements (https://cloud.google.com/storage/docs/buckets#naming).
	Bucket string

	// InputFormat is the format of objects in Cloud Storage.
	// Defaults to TextFormat.
	InputFormat ingestionDataSourceCloudStorageInputFormat

	// MinimumObjectCreateTime means objects with a larger or equal creation timestamp will be
	// ingested.
	MinimumObjectCreateTime time.Time

	// MatchGlob is the pattern used to match objects that will be ingested. If
	// empty, all objects will be ingested. See the [supported
	// patterns](https://cloud.google.com/storage/docs/json_api/v1/objects/list#list-objects-and-prefixes-using-glob).
	MatchGlob string
}

var _ IngestionDataSource = (*IngestionDataSourceCloudStorage)(nil)

func (i *IngestionDataSourceCloudStorage) isIngestionDataSource() bool {
	return true
}

type ingestionDataSourceCloudStorageInputFormat interface {
	isCloudStorageIngestionInputFormat() bool
}

var _ ingestionDataSourceCloudStorageInputFormat = (*IngestionDataSourceCloudStorageTextFormat)(nil)
var _ ingestionDataSourceCloudStorageInputFormat = (*IngestionDataSourceCloudStorageAvroFormat)(nil)
var _ ingestionDataSourceCloudStorageInputFormat = (*IngestionDataSourceCloudStoragePubSubAvroFormat)(nil)

// IngestionDataSourceCloudStorageTextFormat means Cloud Storage data will be interpreted as text.
type IngestionDataSourceCloudStorageTextFormat struct {
	Delimiter string
}

func (i *IngestionDataSourceCloudStorageTextFormat) isCloudStorageIngestionInputFormat() bool {
	return true
}

// IngestionDataSourceCloudStorageAvroFormat means Cloud Storage data will be interpreted in Avro format.
type IngestionDataSourceCloudStorageAvroFormat struct{}

func (i *IngestionDataSourceCloudStorageAvroFormat) isCloudStorageIngestionInputFormat() bool {
	return true
}

// IngestionDataSourceCloudStoragePubSubAvroFormat is used assuming the data was written using Cloud
// Storage subscriptions https://cloud.google.com/pubsub/docs/cloudstorage.
type IngestionDataSourceCloudStoragePubSubAvroFormat struct{}

func (i *IngestionDataSourceCloudStoragePubSubAvroFormat) isCloudStorageIngestionInputFormat() bool {
	return true
}

// EventHubsState denotes the possible states for ingestion from Event Hubs.
type EventHubsState int

const (
	// EventHubsStateUnspecified is the default value. This value is unused.
	EventHubsStateUnspecified = iota

	// EventHubsStateActive means the state is active.
	EventHubsStateActive

	// EventHubsStatePermissionDenied indicates encountered permission denied error
	// while consuming data from Event Hubs.
	// This can happen when `client_id`, or `tenant_id` are invalid. Or the
	// right permissions haven't been granted.
	EventHubsStatePermissionDenied

	// EventHubsStatePublishPermissionDenied indicates permission denied encountered
	// while publishing to the topic.
	EventHubsStatePublishPermissionDenied

	// EventHubsStateNamespaceNotFound indicates the provided Event Hubs namespace couldn't be found.
	EventHubsStateNamespaceNotFound

	// EventHubsStateNotFound indicates the provided Event Hub couldn't be found.
	EventHubsStateNotFound

	// EventHubsStateSubscriptionNotFound indicates the provided Event Hubs subscription couldn't be found.
	EventHubsStateSubscriptionNotFound

	// EventHubsStateResourceGroupNotFound indicates the provided Event Hubs resource group couldn't be found.
	EventHubsStateResourceGroupNotFound
)

// IngestionDataSourceAzureEventHubs are ingestion settings for Azure Event Hubs.
type IngestionDataSourceAzureEventHubs struct {
	// Output only field that indicates the state of the Event Hubs ingestion source.
	State EventHubsState

	// Name of the resource group within the Azure subscription
	ResourceGroup string

	// Name of the Event Hubs namespace
	Namespace string

	// Rame of the Event Hub.
	EventHub string

	// Client ID of the Azure application that is being used to authenticate Pub/Sub.
	ClientID string

	// Tenant ID of the Azure application that is being used to authenticate Pub/Sub.
	TenantID string

	// The Azure subscription ID
	SubscriptionID string

	// GCPServiceAccount is the GCP service account to be used for Federated Identity
	// authentication.
	GCPServiceAccount string
}

var _ IngestionDataSource = (*IngestionDataSourceAzureEventHubs)(nil)

func (i *IngestionDataSourceAzureEventHubs) isIngestionDataSource() bool {
	return true
}

// AmazonMSKState denotes the possible states for ingestion from Amazon MSK.
type AmazonMSKState int

const (
	// AmazonMSKStateUnspecified is the default value. This value is unused.
	AmazonMSKStateUnspecified = iota

	// AmazonMSKActive indicates MSK topic is active.
	AmazonMSKActive

	// AmazonMSKPermissionDenied indicates permission denied encountered while consuming data from Amazon MSK.
	AmazonMSKPermissionDenied

	// AmazonMSKPublishPermissionDenied indicates permission denied encountered while publishing to the topic.
	AmazonMSKPublishPermissionDenied

	// AmazonMSKClusterNotFound indicates the provided Msk cluster wasn't found.
	AmazonMSKClusterNotFound

	// AmazonMSKTopicNotFound indicates the provided topic wasn't found.
	AmazonMSKTopicNotFound
)

// IngestionDataSourceAmazonMSK are ingestion settings for Amazon MSK.
type IngestionDataSourceAmazonMSK struct {
	// An output-only field that indicates the state of the Amazon
	// MSK ingestion source.
	State AmazonMSKState

	// The Amazon Resource Name (ARN) that uniquely identifies the
	// cluster.
	ClusterARN string

	// The name of the topic in the Amazon MSK cluster that Pub/Sub
	// will import from.
	Topic string

	// AWS role ARN to be used for Federated Identity authentication
	// with Amazon MSK. Check the Pub/Sub docs for how to set up this role and
	// the required permissions that need to be attached to it.
	AWSRoleARN string

	// The GCP service account to be used for Federated Identity
	// authentication with Amazon MSK (via a `AssumeRoleWithWebIdentity` call
	// for the provided role). The `aws_role_arn` must be set up with
	// `accounts.google.com:sub` equals to this service account number.
	GCPServiceAccount string
}

var _ IngestionDataSource = (*IngestionDataSourceAmazonMSK)(nil)

func (i *IngestionDataSourceAmazonMSK) isIngestionDataSource() bool {
	return true
}

// ConfluentCloudState denotes state of ingestion topic with confluent cloud
type ConfluentCloudState int

const (
	// ConfluentCloudStateUnspecified is the default value. This value is unused.
	ConfluentCloudStateUnspecified = iota

	// ConfluentCloudActive indicates the state is active.
	ConfluentCloudActive = 1

	// ConfluentCloudPermissionDenied indicates permission denied encountered
	// while consuming data from Confluent Cloud.
	ConfluentCloudPermissionDenied = 2

	// ConfluentCloudPublishPermissionDenied indicates permission denied encountered
	// while publishing to the topic.
	ConfluentCloudPublishPermissionDenied = 3

	// ConfluentCloudUnreachableBootstrapServer indicates the provided bootstrap
	// server address is unreachable.
	ConfluentCloudUnreachableBootstrapServer = 4

	// ConfluentCloudClusterNotFound indicates the provided cluster wasn't found.
	ConfluentCloudClusterNotFound = 5

	// ConfluentCloudTopicNotFound indicates the provided topic wasn't found.
	ConfluentCloudTopicNotFound = 6
)

// IngestionDataSourceConfluentCloud are ingestion settings for confluent cloud.
type IngestionDataSourceConfluentCloud struct {
	// An output-only field that indicates the state of the
	// Confluent Cloud ingestion source.
	State ConfluentCloudState

	// The address of the bootstrap server. The format is url:port.
	BootstrapServer string

	// The id of the cluster.
	ClusterID string

	// The name of the topic in the Confluent Cloud cluster that
	// Pub/Sub will import from.
	Topic string

	// The id of the identity pool to be used for Federated Identity
	// authentication with Confluent Cloud. See
	// https://docs.confluent.io/cloud/current/security/authenticate/workload-identities/identity-providers/oauth/identity-pools.html#add-oauth-identity-pools.
	IdentityPoolID string

	// The GCP service account to be used for Federated Identity
	// authentication with `identity_pool_id`.
	GCPServiceAccount string
}

var _ IngestionDataSource = (*IngestionDataSourceConfluentCloud)(nil)

func (i *IngestionDataSourceConfluentCloud) isIngestionDataSource() bool {
	return true
}

func protoToIngestionDataSourceSettings(pbs *pb.IngestionDataSourceSettings) *IngestionDataSourceSettings {
	if pbs == nil {
		return nil
	}

	s := &IngestionDataSourceSettings{}
	if k := pbs.GetAwsKinesis(); k != nil {
		s.Source = &IngestionDataSourceAWSKinesis{
			State:             AWSKinesisState(k.State),
			StreamARN:         k.GetStreamArn(),
			ConsumerARN:       k.GetConsumerArn(),
			AWSRoleARN:        k.GetAwsRoleArn(),
			GCPServiceAccount: k.GetGcpServiceAccount(),
		}
	} else if cs := pbs.GetCloudStorage(); cs != nil {
		var format ingestionDataSourceCloudStorageInputFormat
		switch t := cs.InputFormat.(type) {
		case *pb.IngestionDataSourceSettings_CloudStorage_TextFormat_:
			format = &IngestionDataSourceCloudStorageTextFormat{
				Delimiter: *t.TextFormat.Delimiter,
			}
		case *pb.IngestionDataSourceSettings_CloudStorage_AvroFormat_:
			format = &IngestionDataSourceCloudStorageAvroFormat{}
		case *pb.IngestionDataSourceSettings_CloudStorage_PubsubAvroFormat:
			format = &IngestionDataSourceCloudStoragePubSubAvroFormat{}
		}
		s.Source = &IngestionDataSourceCloudStorage{
			State:                   CloudStorageIngestionState(cs.GetState()),
			Bucket:                  cs.GetBucket(),
			InputFormat:             format,
			MinimumObjectCreateTime: cs.GetMinimumObjectCreateTime().AsTime(),
			MatchGlob:               cs.GetMatchGlob(),
		}
	} else if e := pbs.GetAzureEventHubs(); e != nil {
		s.Source = &IngestionDataSourceAzureEventHubs{
			State:             EventHubsState(e.GetState()),
			ResourceGroup:     e.GetResourceGroup(),
			Namespace:         e.GetNamespace(),
			EventHub:          e.GetEventHub(),
			ClientID:          e.GetClientId(),
			TenantID:          e.GetTenantId(),
			SubscriptionID:    e.GetSubscriptionId(),
			GCPServiceAccount: e.GetGcpServiceAccount(),
		}
	} else if m := pbs.GetAwsMsk(); m != nil {
		s.Source = &IngestionDataSourceAmazonMSK{
			State:             AmazonMSKState(m.GetState()),
			ClusterARN:        m.GetClusterArn(),
			Topic:             m.GetTopic(),
			AWSRoleARN:        m.GetAwsRoleArn(),
			GCPServiceAccount: m.GetGcpServiceAccount(),
		}
	} else if c := pbs.GetConfluentCloud(); c != nil {
		s.Source = &IngestionDataSourceConfluentCloud{
			State:             ConfluentCloudState(c.GetState()),
			BootstrapServer:   c.GetBootstrapServer(),
			ClusterID:         c.GetClusterId(),
			Topic:             c.GetTopic(),
			IdentityPoolID:    c.GetIdentityPoolId(),
			GCPServiceAccount: c.GetGcpServiceAccount(),
		}
	}

	if pbs.PlatformLogsSettings != nil {
		s.PlatformLogsSettings = &PlatformLogsSettings{
			Severity: PlatformLogsSeverity(pbs.PlatformLogsSettings.Severity),
		}
	}

	return s
}

func (i *IngestionDataSourceSettings) toProto() *pb.IngestionDataSourceSettings {
	if i == nil {
		return nil
	}
	// An empty/zero-valued config is treated the same as nil and clearing this setting.
	if (IngestionDataSourceSettings{}) == *i {
		return nil
	}
	pbs := &pb.IngestionDataSourceSettings{}
	if i.PlatformLogsSettings != nil {
		pbs.PlatformLogsSettings = &pb.PlatformLogsSettings{
			Severity: pb.PlatformLogsSettings_Severity(i.PlatformLogsSettings.Severity),
		}
	}
	if out := i.Source; out != nil {
		if k, ok := out.(*IngestionDataSourceAWSKinesis); ok {
			pbs.Source = &pb.IngestionDataSourceSettings_AwsKinesis_{
				AwsKinesis: &pb.IngestionDataSourceSettings_AwsKinesis{
					State:             pb.IngestionDataSourceSettings_AwsKinesis_State(k.State),
					StreamArn:         k.StreamARN,
					ConsumerArn:       k.ConsumerARN,
					AwsRoleArn:        k.AWSRoleARN,
					GcpServiceAccount: k.GCPServiceAccount,
				},
			}
		}
		if cs, ok := out.(*IngestionDataSourceCloudStorage); ok {
			switch format := cs.InputFormat.(type) {
			case *IngestionDataSourceCloudStorageTextFormat:
				pbs.Source = &pb.IngestionDataSourceSettings_CloudStorage_{
					CloudStorage: &pb.IngestionDataSourceSettings_CloudStorage{
						State:  pb.IngestionDataSourceSettings_CloudStorage_State(cs.State),
						Bucket: cs.Bucket,
						InputFormat: &pb.IngestionDataSourceSettings_CloudStorage_TextFormat_{
							TextFormat: &pb.IngestionDataSourceSettings_CloudStorage_TextFormat{
								Delimiter: &format.Delimiter,
							},
						},
						MinimumObjectCreateTime: timestamppb.New(cs.MinimumObjectCreateTime),
						MatchGlob:               cs.MatchGlob,
					},
				}
			case *IngestionDataSourceCloudStorageAvroFormat:
				pbs.Source = &pb.IngestionDataSourceSettings_CloudStorage_{
					CloudStorage: &pb.IngestionDataSourceSettings_CloudStorage{
						Bucket: cs.Bucket,
						InputFormat: &pb.IngestionDataSourceSettings_CloudStorage_AvroFormat_{
							AvroFormat: &pb.IngestionDataSourceSettings_CloudStorage_AvroFormat{},
						},
						MinimumObjectCreateTime: timestamppb.New(cs.MinimumObjectCreateTime),
						MatchGlob:               cs.MatchGlob,
					},
				}
			case *IngestionDataSourceCloudStoragePubSubAvroFormat:
				pbs.Source = &pb.IngestionDataSourceSettings_CloudStorage_{
					CloudStorage: &pb.IngestionDataSourceSettings_CloudStorage{
						State:  pb.IngestionDataSourceSettings_CloudStorage_State(cs.State),
						Bucket: cs.Bucket,
						InputFormat: &pb.IngestionDataSourceSettings_CloudStorage_PubsubAvroFormat{
							PubsubAvroFormat: &pb.IngestionDataSourceSettings_CloudStorage_PubSubAvroFormat{},
						},
						MinimumObjectCreateTime: timestamppb.New(cs.MinimumObjectCreateTime),
						MatchGlob:               cs.MatchGlob,
					},
				}
			}
		}
		if e, ok := out.(*IngestionDataSourceAzureEventHubs); ok {
			pbs.Source = &pb.IngestionDataSourceSettings_AzureEventHubs_{
				AzureEventHubs: &pb.IngestionDataSourceSettings_AzureEventHubs{
					ResourceGroup:     e.ResourceGroup,
					Namespace:         e.Namespace,
					EventHub:          e.EventHub,
					ClientId:          e.ClientID,
					TenantId:          e.TenantID,
					SubscriptionId:    e.SubscriptionID,
					GcpServiceAccount: e.GCPServiceAccount,
				},
			}
		}
		if m, ok := out.(*IngestionDataSourceAmazonMSK); ok {
			pbs.Source = &pb.IngestionDataSourceSettings_AwsMsk_{
				AwsMsk: &pb.IngestionDataSourceSettings_AwsMsk{
					ClusterArn:        m.ClusterARN,
					Topic:             m.Topic,
					AwsRoleArn:        m.AWSRoleARN,
					GcpServiceAccount: m.GCPServiceAccount,
				},
			}
		}
		if c, ok := out.(*IngestionDataSourceConfluentCloud); ok {
			pbs.Source = &pb.IngestionDataSourceSettings_ConfluentCloud_{
				ConfluentCloud: &pb.IngestionDataSourceSettings_ConfluentCloud{
					BootstrapServer:   c.BootstrapServer,
					ClusterId:         c.ClusterID,
					Topic:             c.Topic,
					IdentityPoolId:    c.IdentityPoolID,
					GcpServiceAccount: c.GCPServiceAccount,
				},
			}
		}
	}
	return pbs
}

// PlatformLogsSettings configures logging produced by Pub/Sub.
// Currently only valid on Cloud Storage ingestion topics.
type PlatformLogsSettings struct {
	Severity PlatformLogsSeverity
}

// PlatformLogsSeverity are the severity levels of Platform Logs.
type PlatformLogsSeverity int32

const (
	// PlatformLogsSeverityUnspecified is the default value. Logs level is unspecified. Logs will be disabled.
	PlatformLogsSeverityUnspecified PlatformLogsSeverity = iota
	// PlatformLogsSeverityDisabled means logs will be disabled.
	PlatformLogsSeverityDisabled
	// PlatformLogsSeverityDebug means debug logs and higher-severity logs will be written.
	PlatformLogsSeverityDebug
	// PlatformLogsSeverityInfo means info logs and higher-severity logs will be written.
	PlatformLogsSeverityInfo
	// PlatformLogsSeverityWarning means warning logs and higher-severity logs will be written.
	PlatformLogsSeverityWarning
	// PlatformLogsSeverityError means only error logs will be written.
	PlatformLogsSeverityError
)

// Config returns the TopicConfig for the topic.
func (t *Topic) Config(ctx context.Context) (TopicConfig, error) {
	pbt, err := t.c.pubc.GetTopic(ctx, &pb.GetTopicRequest{Topic: t.name})
	if err != nil {
		return TopicConfig{}, err
	}
	return protoToTopicConfig(pbt), nil
}

// Update changes an existing topic according to the fields set in cfg. It returns
// the new TopicConfig.
func (t *Topic) Update(ctx context.Context, cfg TopicConfigToUpdate) (TopicConfig, error) {
	req := t.updateRequest(cfg)
	if len(req.UpdateMask.Paths) == 0 {
		return TopicConfig{}, errors.New("pubsub: UpdateTopic call with nothing to update")
	}
	rpt, err := t.c.pubc.UpdateTopic(ctx, req)
	if err != nil {
		return TopicConfig{}, err
	}
	return protoToTopicConfig(rpt), nil
}

func (t *Topic) updateRequest(cfg TopicConfigToUpdate) *pb.UpdateTopicRequest {
	pt := &pb.Topic{Name: t.name}
	var paths []string
	if cfg.Labels != nil {
		pt.Labels = cfg.Labels
		paths = append(paths, "labels")
	}
	if cfg.MessageStoragePolicy != nil {
		pt.MessageStoragePolicy = messageStoragePolicyToProto(cfg.MessageStoragePolicy)
		paths = append(paths, "message_storage_policy")
	}
	if cfg.RetentionDuration != nil {
		r := optional.ToDuration(cfg.RetentionDuration)
		pt.MessageRetentionDuration = durationpb.New(r)
		if r < 0 {
			// Clear MessageRetentionDuration if sentinel value is read.
			pt.MessageRetentionDuration = nil
		}
		paths = append(paths, "message_retention_duration")
	}
	// Updating SchemaSettings' field masks are more complicated here
	// since each field should be able to be independently edited, while
	// preserving the current values for everything else. We also denote
	// the zero value SchemaSetting to mean clearing or removing schema
	// from the topic.
	if cfg.SchemaSettings != nil {
		pt.SchemaSettings = schemaSettingsToProto(cfg.SchemaSettings)
		clearSchema := true
		if pt.SchemaSettings.Schema != "" {
			paths = append(paths, "schema_settings.schema")
			clearSchema = false
		}
		if pt.SchemaSettings.Encoding != pb.Encoding_ENCODING_UNSPECIFIED {
			paths = append(paths, "schema_settings.encoding")
			clearSchema = false
		}
		if pt.SchemaSettings.FirstRevisionId != "" {
			paths = append(paths, "schema_settings.first_revision_id")
			clearSchema = false
		}
		if pt.SchemaSettings.LastRevisionId != "" {
			paths = append(paths, "schema_settings.last_revision_id")
			clearSchema = false
		}
		// Clear the schema if all of its values are equal to the zero value.
		if clearSchema {
			paths = append(paths, "schema_settings")
			pt.SchemaSettings = nil
		}
	}
	if cfg.IngestionDataSourceSettings != nil {
		pt.IngestionDataSourceSettings = cfg.IngestionDataSourceSettings.toProto()
		paths = append(paths, "ingestion_data_source_settings")
	}
	if cfg.MessageTransforms != nil {
		pt.MessageTransforms = messageTransformsToProto(cfg.MessageTransforms)
		paths = append(paths, "message_transforms")
	}
	return &pb.UpdateTopicRequest{
		Topic:      pt,
		UpdateMask: &fmpb.FieldMask{Paths: paths},
	}
}

// Topics returns an iterator which returns all of the topics for the client's project.
func (c *Client) Topics(ctx context.Context) *TopicIterator {
	it := c.pubc.ListTopics(ctx, &pb.ListTopicsRequest{Project: c.fullyQualifiedProjectName()})
	return &TopicIterator{
		c:  c,
		it: it,
		next: func() (string, error) {
			topic, err := it.Next()
			if err != nil {
				return "", err
			}
			return topic.Name, nil
		},
	}
}

// TopicIterator is an iterator that returns a series of topics.
type TopicIterator struct {
	c    *Client
	it   *vkit.TopicIterator
	next func() (string, error)
}

// Next returns the next topic. If there are no more topics, iterator.Done will be returned.
func (tps *TopicIterator) Next() (*Topic, error) {
	topicName, err := tps.next()
	if err != nil {
		return nil, err
	}
	return newTopic(tps.c, topicName), nil
}

// NextConfig returns the next topic config. If there are no more topics,
// iterator.Done will be returned.
// This call shares the underlying iterator with calls to `TopicIterator.Next`.
// If you wish to use mix calls, create separate iterator instances for both.
func (t *TopicIterator) NextConfig() (*TopicConfig, error) {
	tpb, err := t.it.Next()
	if err != nil {
		return nil, err
	}
	cfg := protoToTopicConfig(tpb)
	return &cfg, nil
}

// ID returns the unique identifier of the topic within its project.
func (t *Topic) ID() string {
	slash := strings.LastIndex(t.name, "/")
	if slash == -1 {
		// name is not a fully-qualified name.
		panic("bad topic name")
	}
	return t.name[slash+1:]
}

// String returns the printable globally unique name for the topic.
func (t *Topic) String() string {
	return t.name
}

// Delete deletes the topic.
func (t *Topic) Delete(ctx context.Context) error {
	return t.c.pubc.DeleteTopic(ctx, &pb.DeleteTopicRequest{Topic: t.name})
}

// Exists reports whether the topic exists on the server.
func (t *Topic) Exists(ctx context.Context) (bool, error) {
	if t.name == "_deleted-topic_" {
		return false, nil
	}
	_, err := t.c.pubc.GetTopic(ctx, &pb.GetTopicRequest{Topic: t.name})
	if err == nil {
		return true, nil
	}
	if status.Code(err) == codes.NotFound {
		return false, nil
	}
	return false, err
}

// IAM returns the topic's IAM handle.
func (t *Topic) IAM() *iam.Handle {
	return iam.InternalNewHandle(t.c.pubc.Connection(), t.name)
}

// Subscriptions returns an iterator which returns the subscriptions for this topic.
//
// Some of the returned subscriptions may belong to a project other than t.
func (t *Topic) Subscriptions(ctx context.Context) *SubscriptionIterator {
	it := t.c.pubc.ListTopicSubscriptions(ctx, &pb.ListTopicSubscriptionsRequest{
		Topic: t.name,
	})
	return &SubscriptionIterator{
		c:    t.c,
		next: it.Next,
	}
}

// ErrTopicStopped indicates that topic has been stopped and further publishing will fail.
var ErrTopicStopped = errors.New("pubsub: Stop has been called for this topic")

// A PublishResult holds the result from a call to Publish.
//
// Call Get to obtain the result of the Publish call. Example:
//
//	// Get blocks until Publish completes or ctx is done.
//	id, err := r.Get(ctx)
//	if err != nil {
//	    // TODO: Handle error.
//	}
type PublishResult = ipubsub.PublishResult

var errTopicOrderingNotEnabled = errors.New("Topic.EnableMessageOrdering=false, but an OrderingKey was set in Message. Please remove the OrderingKey or turn on Topic.EnableMessageOrdering")

// Publish publishes msg to the topic asynchronously. Messages are batched and
// sent according to the topic's PublishSettings. Publish never blocks.
//
// Publish returns a non-nil PublishResult which will be ready when the
// message has been sent (or has failed to be sent) to the server.
//
// Publish creates goroutines for batching and sending messages. These goroutines
// need to be stopped by calling t.Stop(). Once stopped, future calls to Publish
// will immediately return a PublishResult with an error.
func (t *Topic) Publish(ctx context.Context, msg *Message) *PublishResult {
	var createSpan trace.Span
	if t.enableTracing {
		opts := getPublishSpanAttributes(t.c.projectID, t.ID(), msg)
		opts = append(opts, trace.WithAttributes(semconv.CodeFunction("Publish")))
		ctx, createSpan = startSpan(ctx, createSpanName, t.ID(), opts...)
	}
	ctx, err := tag.New(ctx, tag.Insert(keyStatus, "OK"), tag.Upsert(keyTopic, t.name))
	if err != nil {
		log.Printf("pubsub: cannot create context with tag in Publish: %v", err)
	}

	r := ipubsub.NewPublishResult()
	if !t.EnableMessageOrdering && msg.OrderingKey != "" {
		ipubsub.SetPublishResult(r, "", errTopicOrderingNotEnabled)
		spanRecordError(createSpan, errTopicOrderingNotEnabled)
		return r
	}

	// Calculate the size of the encoded proto message by accounting
	// for the length of an individual PubSubMessage and Data/Attributes field.
	msgSize := proto.Size(&pb.PubsubMessage{
		Data:        msg.Data,
		Attributes:  msg.Attributes,
		OrderingKey: msg.OrderingKey,
	})
	if t.enableTracing {
		createSpan.SetAttributes(semconv.MessagingMessageBodySize(len(msg.Data)))
	}

	t.initBundler()
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.stopped {
		ipubsub.SetPublishResult(r, "", ErrTopicStopped)
		spanRecordError(createSpan, ErrTopicStopped)
		return r
	}

	var batcherSpan trace.Span
	var fcSpan trace.Span

	if t.enableTracing {
		_, fcSpan = startSpan(ctx, publishFCSpanName, "")
	}
	if err := t.flowController.acquire(ctx, msgSize); err != nil {
		t.scheduler.Pause(msg.OrderingKey)
		ipubsub.SetPublishResult(r, "", err)
		spanRecordError(fcSpan, err)
		return r
	}
	if t.enableTracing {
		fcSpan.End()
	}

	bmsg := &bundledMessage{
		msg:        msg,
		res:        r,
		size:       msgSize,
		createSpan: createSpan,
	}

	if t.enableTracing {
		_, batcherSpan = startSpan(ctx, batcherSpanName, "")
		bmsg.batcherSpan = batcherSpan

		// Inject the context from the first publish span rather than from flow control / batching.
		injectPropagation(ctx, msg)
	}

	if err := t.scheduler.Add(msg.OrderingKey, bmsg, msgSize); err != nil {
		t.scheduler.Pause(msg.OrderingKey)
		ipubsub.SetPublishResult(r, "", err)
		spanRecordError(createSpan, err)
	}

	return r
}

// Stop sends all remaining published messages and stop goroutines created for handling
// publishing. Returns once all outstanding messages have been sent or have
// failed to be sent.
func (t *Topic) Stop() {
	t.mu.Lock()
	noop := t.stopped || t.scheduler == nil
	t.stopped = true
	t.mu.Unlock()
	if noop {
		return
	}
	t.scheduler.FlushAndStop()
}

// Flush blocks until all remaining messages are sent.
func (t *Topic) Flush() {
	if t.stopped || t.scheduler == nil {
		return
	}
	t.scheduler.Flush()
}

type bundledMessage struct {
	msg  *Message
	res  *PublishResult
	size int
	// createSpan is the entire publish createSpan (from user calling Publish to the publish RPC resolving).
	createSpan trace.Span
	// batcherSpan traces the message batching operation in publish scheduler.
	batcherSpan trace.Span
}

func (t *Topic) initBundler() {
	t.mu.RLock()
	noop := t.stopped || t.scheduler != nil
	t.mu.RUnlock()
	if noop {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	// Must re-check, since we released the lock.
	if t.stopped || t.scheduler != nil {
		return
	}

	timeout := t.PublishSettings.Timeout

	workers := t.PublishSettings.NumGoroutines
	// Unless overridden, allow many goroutines per CPU to call the Publish RPC
	// concurrently. The default value was determined via extensive load
	// testing (see the loadtest subdirectory).
	if t.PublishSettings.NumGoroutines == 0 {
		workers = 25 * runtime.GOMAXPROCS(0)
	}

	t.scheduler = scheduler.NewPublishScheduler(workers, func(bundle interface{}) {
		// Use a context detached from the one passed to NewClient.
		ctx := context.Background()
		if timeout != 0 {
			var cancel func()
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		bmsgs := bundle.([]*bundledMessage)
		if t.enableTracing {
			for _, m := range bmsgs {
				m.batcherSpan.End()
				m.createSpan.AddEvent(eventPublishStart, trace.WithAttributes(semconv.MessagingBatchMessageCount(len(bmsgs))))
			}
		}
		t.publishMessageBundle(ctx, bmsgs)
		if t.enableTracing {
			for _, m := range bmsgs {
				m.createSpan.AddEvent(eventPublishEnd)
				m.createSpan.End()
			}
		}
	})
	t.scheduler.DelayThreshold = t.PublishSettings.DelayThreshold
	t.scheduler.BundleCountThreshold = t.PublishSettings.CountThreshold
	if t.scheduler.BundleCountThreshold > MaxPublishRequestCount {
		t.scheduler.BundleCountThreshold = MaxPublishRequestCount
	}
	t.scheduler.BundleByteThreshold = t.PublishSettings.ByteThreshold

	fcs := DefaultPublishSettings.FlowControlSettings
	fcs.LimitExceededBehavior = t.PublishSettings.FlowControlSettings.LimitExceededBehavior
	if t.PublishSettings.FlowControlSettings.MaxOutstandingBytes > 0 {
		b := t.PublishSettings.FlowControlSettings.MaxOutstandingBytes
		fcs.MaxOutstandingBytes = b

		// If MaxOutstandingBytes is set, disable BufferedByteLimit by setting it to maxint.
		// This is because there's no way to set "unlimited" for BufferedByteLimit,
		// and simply setting it to MaxOutstandingBytes occasionally leads to issues where
		// BufferedByteLimit is reached even though there are resources available.
		t.PublishSettings.BufferedByteLimit = maxInt
	}
	if t.PublishSettings.FlowControlSettings.MaxOutstandingMessages > 0 {
		fcs.MaxOutstandingMessages = t.PublishSettings.FlowControlSettings.MaxOutstandingMessages
	}

	t.flowController = newTopicFlowController(fcs)

	bufferedByteLimit := DefaultPublishSettings.BufferedByteLimit
	if t.PublishSettings.BufferedByteLimit > 0 {
		bufferedByteLimit = t.PublishSettings.BufferedByteLimit
	}
	t.scheduler.BufferedByteLimit = bufferedByteLimit

	// Calculate the max limit of a single bundle. 5 comes from the number of bytes
	// needed to be reserved for encoding the PubsubMessage repeated field.
	t.scheduler.BundleByteLimit = MaxPublishRequestBytes - calcFieldSizeString(t.name) - 5
}

// ErrPublishingPaused is a custom error indicating that the publish paused for the specified ordering key.
type ErrPublishingPaused struct {
	OrderingKey string
}

func (e ErrPublishingPaused) Error() string {
	return fmt.Sprintf("pubsub: Publishing for ordering key, %s, paused due to previous error. Call topic.ResumePublish(orderingKey) before resuming publishing", e.OrderingKey)

}

func (t *Topic) publishMessageBundle(ctx context.Context, bms []*bundledMessage) {
	ctx, err := tag.New(ctx, tag.Insert(keyStatus, "OK"), tag.Upsert(keyTopic, t.name))
	if err != nil {
		log.Printf("pubsub: cannot create context with tag in publishMessageBundle: %v", err)
	}
	numMsgs := len(bms)
	pbMsgs := make([]*pb.PubsubMessage, numMsgs)
	var orderingKey string
	if numMsgs != 0 {
		// extract the ordering key for this batch. since
		// messages in the same batch share the same ordering
		// key, it doesn't matter which we read from.
		orderingKey = bms[0].msg.OrderingKey
	}

	if t.enableTracing {
		links := make([]trace.Link, 0, numMsgs)
		for _, bm := range bms {
			if bm.createSpan.SpanContext().IsSampled() {
				links = append(links, trace.Link{SpanContext: bm.createSpan.SpanContext()})
			}
		}

		projectID, topicID := parseResourceName(t.name)
		var pSpan trace.Span
		opts := getCommonOptions(projectID, topicID)
		// Add link to publish RPC span of createSpan(s).
		opts = append(opts, trace.WithLinks(links...))
		opts = append(
			opts,
			trace.WithAttributes(
				semconv.MessagingBatchMessageCount(numMsgs),
				semconv.CodeFunction("publishMessageBundle"),
			),
		)
		ctx, pSpan = startSpan(ctx, publishRPCSpanName, topicID, opts...)
		defer pSpan.End()

		// Add the reverse link to createSpan(s) of publish RPC span.
		if pSpan.SpanContext().IsSampled() {
			for _, bm := range bms {
				bm.createSpan.AddLink(trace.Link{
					SpanContext: pSpan.SpanContext(),
					Attributes: []attribute.KeyValue{
						semconv.MessagingOperationName(publishRPCSpanName),
					},
				})
			}
		}
	}
	var batchSize int
	for i, bm := range bms {
		pbMsgs[i] = &pb.PubsubMessage{
			Data:        bm.msg.Data,
			Attributes:  bm.msg.Attributes,
			OrderingKey: bm.msg.OrderingKey,
		}
		batchSize = batchSize + proto.Size(pbMsgs[i])
		bm.msg = nil // release bm.msg for GC
	}

	var res *pb.PublishResponse
	start := time.Now()
	if orderingKey != "" && t.scheduler.IsPaused(orderingKey) {
		err = ErrPublishingPaused{OrderingKey: orderingKey}
	} else {
		// Apply custom publish retryer on top of user specified retryer and
		// default retryer.
		opts := t.c.pubc.CallOptions.Publish
		var settings gax.CallSettings
		for _, opt := range opts {
			opt.Resolve(&settings)
		}
		r := &publishRetryer{defaultRetryer: settings.Retry()}
		gaxOpts := []gax.CallOption{
			gax.WithGRPCOptions(grpc.MaxCallSendMsgSize(maxSendRecvBytes)),
			gax.WithRetry(func() gax.Retryer { return r }),
		}
		if t.PublishSettings.shouldCompress(batchSize) {
			gaxOpts = append(gaxOpts, gax.WithGRPCOptions(grpc.UseCompressor(gzip.Name)))
		}
		res, err = t.c.pubc.Publish(ctx, &pb.PublishRequest{
			Topic:    t.name,
			Messages: pbMsgs,
		}, gaxOpts...)
	}
	end := time.Now()
	if err != nil {
		t.scheduler.Pause(orderingKey)
		// Update context with error tag for OpenCensus,
		// using same stats.Record() call as success case.
		ctx, _ = tag.New(ctx, tag.Upsert(keyStatus, "ERROR"),
			tag.Upsert(keyError, err.Error()))
	}
	stats.Record(ctx,
		PublishLatency.M(float64(end.Sub(start)/time.Millisecond)),
		PublishedMessages.M(int64(len(bms))))
	for i, bm := range bms {
		t.flowController.release(ctx, bm.size)
		if err != nil {
			ipubsub.SetPublishResult(bm.res, "", err)
			spanRecordError(bm.createSpan, err)
		} else {
			ipubsub.SetPublishResult(bm.res, res.MessageIds[i], nil)
			if t.enableTracing {
				bm.createSpan.SetAttributes(semconv.MessagingMessageIDKey.String(res.MessageIds[i]))
			}
		}
	}
}

// ResumePublish resumes accepting messages for the provided ordering key.
// Publishing using an ordering key might be paused if an error is
// encountered while publishing, to prevent messages from being published
// out of order.
func (t *Topic) ResumePublish(orderingKey string) {
	t.mu.RLock()
	noop := t.scheduler == nil
	t.mu.RUnlock()
	if noop {
		return
	}

	t.scheduler.Resume(orderingKey)
}
