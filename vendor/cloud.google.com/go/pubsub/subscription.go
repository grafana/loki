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
	"io"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/iam"
	"cloud.google.com/go/internal/optional"
	ipubsub "cloud.google.com/go/internal/pubsub"
	pb "cloud.google.com/go/pubsub/apiv1/pubsubpb"
	"cloud.google.com/go/pubsub/internal/scheduler"
	gax "github.com/googleapis/gax-go/v2"
	"golang.org/x/sync/errgroup"
	fmpb "google.golang.org/genproto/protobuf/field_mask"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	durpb "google.golang.org/protobuf/types/known/durationpb"

	vkit "cloud.google.com/go/pubsub/apiv1"
)

// Subscription is a reference to a PubSub subscription.
type Subscription struct {
	c *Client

	// The fully qualified identifier for the subscription, in the format "projects/<projid>/subscriptions/<name>"
	name string

	// Settings for pulling messages. Configure these before calling Receive.
	ReceiveSettings ReceiveSettings

	mu            sync.Mutex
	receiveActive bool

	enableOrdering bool
}

// Subscription creates a reference to a subscription.
func (c *Client) Subscription(id string) *Subscription {
	return c.SubscriptionInProject(id, c.projectID)
}

// SubscriptionInProject creates a reference to a subscription in a given project.
func (c *Client) SubscriptionInProject(id, projectID string) *Subscription {
	return &Subscription{
		c:    c,
		name: fmt.Sprintf("projects/%s/subscriptions/%s", projectID, id),
	}
}

// String returns the globally unique printable name of the subscription.
func (s *Subscription) String() string {
	return s.name
}

// ID returns the unique identifier of the subscription within its project.
func (s *Subscription) ID() string {
	slash := strings.LastIndex(s.name, "/")
	if slash == -1 {
		// name is not a fully-qualified name.
		panic("bad subscription name")
	}
	return s.name[slash+1:]
}

// Subscriptions returns an iterator which returns all of the subscriptions for the client's project.
func (c *Client) Subscriptions(ctx context.Context) *SubscriptionIterator {
	it := c.subc.ListSubscriptions(ctx, &pb.ListSubscriptionsRequest{
		Project: c.fullyQualifiedProjectName(),
	})
	return &SubscriptionIterator{
		c:  c,
		it: it,
		next: func() (string, error) {
			sub, err := it.Next()
			if err != nil {
				return "", err
			}
			return sub.Name, nil
		},
	}
}

// SubscriptionIterator is an iterator that returns a series of subscriptions.
type SubscriptionIterator struct {
	c    *Client
	it   *vkit.SubscriptionIterator
	next func() (string, error)
}

// Next returns the next subscription. If there are no more subscriptions, iterator.Done will be returned.
func (subs *SubscriptionIterator) Next() (*Subscription, error) {
	subName, err := subs.next()
	if err != nil {
		return nil, err
	}
	return &Subscription{c: subs.c, name: subName}, nil
}

// NextConfig returns the next subscription config. If there are no more subscriptions,
// iterator.Done will be returned.
// This call shares the underlying iterator with calls to `SubscriptionIterator.Next`.
// If you wish to use mix calls, create separate iterator instances for both.
func (subs *SubscriptionIterator) NextConfig() (*SubscriptionConfig, error) {
	spb, err := subs.it.Next()
	if err != nil {
		return nil, err
	}
	cfg, err := protoToSubscriptionConfig(spb, subs.c)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// PushConfig contains configuration for subscriptions that operate in push mode.
type PushConfig struct {
	// A URL locating the endpoint to which messages should be pushed.
	Endpoint string

	// Endpoint configuration attributes. See https://cloud.google.com/pubsub/docs/reference/rest/v1/projects.subscriptions#pushconfig for more details.
	Attributes map[string]string

	// AuthenticationMethod is used by push endpoints to verify the source
	// of push requests.
	// It can be used with push endpoints that are private by default to
	// allow requests only from the Cloud Pub/Sub system, for example.
	// This field is optional and should be set only by users interested in
	// authenticated push.
	AuthenticationMethod AuthenticationMethod
}

func (pc *PushConfig) toProto() *pb.PushConfig {
	if pc == nil {
		return nil
	}
	pbCfg := &pb.PushConfig{
		Attributes:   pc.Attributes,
		PushEndpoint: pc.Endpoint,
	}
	if authMethod := pc.AuthenticationMethod; authMethod != nil {
		switch am := authMethod.(type) {
		case *OIDCToken:
			pbCfg.AuthenticationMethod = am.toProto()
		default: // TODO: add others here when GAIC adds more definitions.
		}
	}
	return pbCfg
}

// AuthenticationMethod is used by push points to verify the source of push requests.
// This interface defines fields that are part of a closed alpha that may not be accessible
// to all users.
type AuthenticationMethod interface {
	isAuthMethod() bool
}

// OIDCToken allows PushConfigs to be authenticated using
// the OpenID Connect protocol https://openid.net/connect/
type OIDCToken struct {
	// Audience to be used when generating OIDC token. The audience claim
	// identifies the recipients that the JWT is intended for. The audience
	// value is a single case-sensitive string. Having multiple values (array)
	// for the audience field is not supported. More info about the OIDC JWT
	// token audience here: https://tools.ietf.org/html/rfc7519#section-4.1.3
	// Note: if not specified, the Push endpoint URL will be used.
	Audience string

	// The service account email to be used for generating the OpenID Connect token.
	// The caller of:
	//  * CreateSubscription
	//  * UpdateSubscription
	//  * ModifyPushConfig
	// calls must have the iam.serviceAccounts.actAs permission for the service account.
	// See https://cloud.google.com/iam/docs/understanding-roles#service-accounts-roles.
	ServiceAccountEmail string
}

var _ AuthenticationMethod = (*OIDCToken)(nil)

func (oidcToken *OIDCToken) isAuthMethod() bool { return true }

func (oidcToken *OIDCToken) toProto() *pb.PushConfig_OidcToken_ {
	if oidcToken == nil {
		return nil
	}
	return &pb.PushConfig_OidcToken_{
		OidcToken: &pb.PushConfig_OidcToken{
			Audience:            oidcToken.Audience,
			ServiceAccountEmail: oidcToken.ServiceAccountEmail,
		},
	}
}

// BigQueryConfigState denotes the possible states for a BigQuery Subscription.
type BigQueryConfigState int

const (
	// BigQueryConfigStateUnspecified is the default value. This value is unused.
	BigQueryConfigStateUnspecified = iota

	// BigQueryConfigActive means the subscription can actively send messages to BigQuery.
	BigQueryConfigActive

	// BigQueryConfigPermissionDenied means the subscription cannot write to the BigQuery table because of permission denied errors.
	BigQueryConfigPermissionDenied

	// BigQueryConfigNotFound means the subscription cannot write to the BigQuery table because it does not exist.
	BigQueryConfigNotFound

	// BigQueryConfigSchemaMismatch means the subscription cannot write to the BigQuery table due to a schema mismatch.
	BigQueryConfigSchemaMismatch
)

// BigQueryConfig configures the subscription to deliver to a BigQuery table.
type BigQueryConfig struct {
	// The name of the table to which to write data, of the form
	// {projectId}:{datasetId}.{tableId}
	Table string

	// When true, use the topic's schema as the columns to write to in BigQuery,
	// if it exists.
	UseTopicSchema bool

	// When true, write the subscription name, message_id, publish_time,
	// attributes, and ordering_key to additional columns in the table. The
	// subscription name, message_id, and publish_time fields are put in their own
	// columns while all other message properties (other than data) are written to
	// a JSON object in the attributes column.
	WriteMetadata bool

	// When true and use_topic_schema is true, any fields that are a part of the
	// topic schema that are not part of the BigQuery table schema are dropped
	// when writing to BigQuery. Otherwise, the schemas must be kept in sync and
	// any messages with extra fields are not written and remain in the
	// subscription's backlog.
	DropUnknownFields bool

	// This is an output-only field that indicates whether or not the subscription can
	// receive messages. This field is set only in responses from the server;
	// it is ignored if it is set in any requests.
	State BigQueryConfigState
}

func (bc *BigQueryConfig) toProto() *pb.BigQueryConfig {
	if bc == nil {
		return nil
	}
	pbCfg := &pb.BigQueryConfig{
		Table:             bc.Table,
		UseTopicSchema:    bc.UseTopicSchema,
		WriteMetadata:     bc.WriteMetadata,
		DropUnknownFields: bc.DropUnknownFields,
		State:             pb.BigQueryConfig_State(bc.State),
	}
	return pbCfg
}

// SubscriptionState denotes the possible states for a Subscription.
type SubscriptionState int

const (
	// SubscriptionStateUnspecified is the default value. This value is unused.
	SubscriptionStateUnspecified = iota

	// SubscriptionStateActive means the subscription can actively send messages to BigQuery.
	SubscriptionStateActive

	// SubscriptionStateResourceError means the subscription receive messages because of an
	// error with the resource to which it pushes messages.
	// See the more detailed error state in the corresponding configuration.
	SubscriptionStateResourceError
)

// SubscriptionConfig describes the configuration of a subscription.
type SubscriptionConfig struct {
	// The fully qualified identifier for the subscription, in the format "projects/<projid>/subscriptions/<name>"
	name string

	// The topic from which this subscription is receiving messages.
	Topic *Topic

	// If push delivery is used with this subscription, this field is
	// used to configure it. Either `PushConfig` or `BigQueryConfig` can be set,
	// but not both. If both are empty, then the subscriber will pull and ack
	// messages using API methods.
	PushConfig PushConfig

	// If delivery to BigQuery is used with this subscription, this field is
	// used to configure it. Either `PushConfig` or `BigQueryConfig` can be set,
	// but not both. If both are empty, then the subscriber will pull and ack
	// messages using API methods.
	BigQueryConfig BigQueryConfig

	// The default maximum time after a subscriber receives a message before
	// the subscriber should acknowledge the message. Note: messages which are
	// obtained via Subscription.Receive need not be acknowledged within this
	// deadline, as the deadline will be automatically extended.
	AckDeadline time.Duration

	// Whether to retain acknowledged messages. If true, acknowledged messages
	// will not be expunged until they fall out of the RetentionDuration window.
	RetainAckedMessages bool

	// How long to retain messages in backlog, from the time of publish. If
	// RetainAckedMessages is true, this duration affects the retention of
	// acknowledged messages, otherwise only unacknowledged messages are retained.
	// Defaults to 7 days. Cannot be longer than 7 days or shorter than 10 minutes.
	RetentionDuration time.Duration

	// Expiration policy specifies the conditions for a subscription's expiration.
	// A subscription is considered active as long as any connected subscriber is
	// successfully consuming messages from the subscription or is issuing
	// operations on the subscription. If `expiration_policy` is not set, a
	// *default policy* with `ttl` of 31 days will be used. The minimum allowed
	// value for `expiration_policy.ttl` is 1 day.
	//
	// Use time.Duration(0) to indicate that the subscription should never expire.
	ExpirationPolicy optional.Duration

	// The set of labels for the subscription.
	Labels map[string]string

	// EnableMessageOrdering enables message ordering on this subscription.
	// This value is only used for subscription creation and update, and
	// is not read locally in calls like Subscription.Receive().
	//
	// If set to false, even if messages are published with ordering keys,
	// messages will not be delivered in order.
	//
	// When calling Subscription.Receive(), the client will check this
	// value with a call to Subscription.Config(), which requires the
	// roles/viewer or roles/pubsub.viewer role on your service account.
	// If that call fails, mesages with ordering keys will be delivered in order.
	EnableMessageOrdering bool

	// DeadLetterPolicy specifies the conditions for dead lettering messages in
	// a subscription. If not set, dead lettering is disabled.
	DeadLetterPolicy *DeadLetterPolicy

	// Filter is an expression written in the Cloud Pub/Sub filter language. If
	// non-empty, then only `PubsubMessage`s whose `attributes` field matches the
	// filter are delivered on this subscription. If empty, then no messages are
	// filtered out. Cannot be changed after the subscription is created.
	Filter string

	// RetryPolicy specifies how Cloud Pub/Sub retries message delivery.
	RetryPolicy *RetryPolicy

	// Detached indicates whether the subscription is detached from its topic.
	// Detached subscriptions don't receive messages from their topic and don't
	// retain any backlog. `Pull` and `StreamingPull` requests will return
	// FAILED_PRECONDITION. If the subscription is a push subscription, pushes to
	// the endpoint will not be made.
	Detached bool

	// TopicMessageRetentionDuration indicates the minimum duration for which a message is
	// retained after it is published to the subscription's topic. If this field is
	// set, messages published to the subscription's topic in the last
	// `TopicMessageRetentionDuration` are always available to subscribers.
	// You can enable both topic and subscription retention for the same topic.
	// In this situation, the maximum of the retention durations takes effect.
	//
	// This is an output only field, meaning it will only appear in responses from the backend
	// and will be ignored if sent in a request.
	TopicMessageRetentionDuration time.Duration

	// EnableExactlyOnceDelivery configures Pub/Sub to provide the following guarantees
	// for the delivery of a message with a given MessageID on this subscription:
	//
	// The message sent to a subscriber is guaranteed not to be resent
	// before the message's acknowledgement deadline expires.
	// An acknowledged message will not be resent to a subscriber.
	//
	// Note that subscribers may still receive multiple copies of a message
	// when `enable_exactly_once_delivery` is true if the message was published
	// multiple times by a publisher client. These copies are considered distinct
	// by Pub/Sub and have distinct MessageID values.
	//
	// Lastly, to guarantee messages have been acked or nacked properly, you must
	// call Message.AckWithResponse() or Message.NackWithResponse(). These return an
	// AckResponse which will be ready if the message has been acked (or failed to be acked).
	EnableExactlyOnceDelivery bool

	// State indicates whether or not the subscription can receive messages.
	// This is an output-only field that indicates whether or not the subscription can
	// receive messages. This field is set only in responses from the server;
	// it is ignored if it is set in any requests.
	State SubscriptionState
}

// String returns the globally unique printable name of the subscription config.
// This method only works when the subscription config is returned from the server,
// such as when calling `client.Subscription` or `client.Subscriptions`.
// Otherwise, this will return an empty string.
func (s *SubscriptionConfig) String() string {
	return s.name
}

// ID returns the unique identifier of the subscription within its project.
// This method only works when the subscription config is returned from the server,
// such as when calling `client.Subscription` or `client.Subscriptions`.
// Otherwise, this will return an empty string.
func (s *SubscriptionConfig) ID() string {
	slash := strings.LastIndex(s.name, "/")
	if slash == -1 {
		return ""
	}
	return s.name[slash+1:]
}

func (cfg *SubscriptionConfig) toProto(name string) *pb.Subscription {
	var pbPushConfig *pb.PushConfig
	if cfg.PushConfig.Endpoint != "" || len(cfg.PushConfig.Attributes) != 0 || cfg.PushConfig.AuthenticationMethod != nil {
		pbPushConfig = cfg.PushConfig.toProto()
	}
	var pbBigQueryConfig *pb.BigQueryConfig
	if cfg.BigQueryConfig.Table != "" {
		pbBigQueryConfig = cfg.BigQueryConfig.toProto()
	}
	var retentionDuration *durpb.Duration
	if cfg.RetentionDuration != 0 {
		retentionDuration = durpb.New(cfg.RetentionDuration)
	}
	var pbDeadLetter *pb.DeadLetterPolicy
	if cfg.DeadLetterPolicy != nil {
		pbDeadLetter = cfg.DeadLetterPolicy.toProto()
	}
	var pbRetryPolicy *pb.RetryPolicy
	if cfg.RetryPolicy != nil {
		pbRetryPolicy = cfg.RetryPolicy.toProto()
	}
	return &pb.Subscription{
		Name:                      name,
		Topic:                     cfg.Topic.name,
		PushConfig:                pbPushConfig,
		BigqueryConfig:            pbBigQueryConfig,
		AckDeadlineSeconds:        trunc32(int64(cfg.AckDeadline.Seconds())),
		RetainAckedMessages:       cfg.RetainAckedMessages,
		MessageRetentionDuration:  retentionDuration,
		Labels:                    cfg.Labels,
		ExpirationPolicy:          expirationPolicyToProto(cfg.ExpirationPolicy),
		EnableMessageOrdering:     cfg.EnableMessageOrdering,
		DeadLetterPolicy:          pbDeadLetter,
		Filter:                    cfg.Filter,
		RetryPolicy:               pbRetryPolicy,
		Detached:                  cfg.Detached,
		EnableExactlyOnceDelivery: cfg.EnableExactlyOnceDelivery,
	}
}

func protoToSubscriptionConfig(pbSub *pb.Subscription, c *Client) (SubscriptionConfig, error) {
	rd := time.Hour * 24 * 7
	if pbSub.MessageRetentionDuration != nil {
		rd = pbSub.MessageRetentionDuration.AsDuration()
	}
	var expirationPolicy time.Duration
	if ttl := pbSub.ExpirationPolicy.GetTtl(); ttl != nil {
		expirationPolicy = ttl.AsDuration()
	}
	dlp := protoToDLP(pbSub.DeadLetterPolicy)
	rp := protoToRetryPolicy(pbSub.RetryPolicy)
	subC := SubscriptionConfig{
		name:                          pbSub.Name,
		Topic:                         newTopic(c, pbSub.Topic),
		AckDeadline:                   time.Second * time.Duration(pbSub.AckDeadlineSeconds),
		RetainAckedMessages:           pbSub.RetainAckedMessages,
		RetentionDuration:             rd,
		Labels:                        pbSub.Labels,
		ExpirationPolicy:              expirationPolicy,
		EnableMessageOrdering:         pbSub.EnableMessageOrdering,
		DeadLetterPolicy:              dlp,
		Filter:                        pbSub.Filter,
		RetryPolicy:                   rp,
		Detached:                      pbSub.Detached,
		TopicMessageRetentionDuration: pbSub.TopicMessageRetentionDuration.AsDuration(),
		EnableExactlyOnceDelivery:     pbSub.EnableExactlyOnceDelivery,
		State:                         SubscriptionState(pbSub.State),
	}
	if pc := protoToPushConfig(pbSub.PushConfig); pc != nil {
		subC.PushConfig = *pc
	}
	if bq := protoToBQConfig(pbSub.GetBigqueryConfig()); bq != nil {
		subC.BigQueryConfig = *bq
	}
	return subC, nil
}

func protoToPushConfig(pbPc *pb.PushConfig) *PushConfig {
	if pbPc == nil {
		return nil
	}
	pc := &PushConfig{
		Endpoint:   pbPc.PushEndpoint,
		Attributes: pbPc.Attributes,
	}
	if am := pbPc.AuthenticationMethod; am != nil {
		if oidcToken, ok := am.(*pb.PushConfig_OidcToken_); ok && oidcToken != nil && oidcToken.OidcToken != nil {
			pc.AuthenticationMethod = &OIDCToken{
				Audience:            oidcToken.OidcToken.GetAudience(),
				ServiceAccountEmail: oidcToken.OidcToken.GetServiceAccountEmail(),
			}
		}
	}
	return pc
}

func protoToBQConfig(pbBQ *pb.BigQueryConfig) *BigQueryConfig {
	if pbBQ == nil {
		return nil
	}
	bq := &BigQueryConfig{
		Table:             pbBQ.GetTable(),
		UseTopicSchema:    pbBQ.GetUseTopicSchema(),
		DropUnknownFields: pbBQ.GetDropUnknownFields(),
		WriteMetadata:     pbBQ.GetWriteMetadata(),
		State:             BigQueryConfigState(pbBQ.State),
	}
	return bq
}

// DeadLetterPolicy specifies the conditions for dead lettering messages in
// a subscription.
type DeadLetterPolicy struct {
	DeadLetterTopic     string
	MaxDeliveryAttempts int
}

func (dlp *DeadLetterPolicy) toProto() *pb.DeadLetterPolicy {
	if dlp == nil || dlp.DeadLetterTopic == "" {
		return nil
	}
	return &pb.DeadLetterPolicy{
		DeadLetterTopic:     dlp.DeadLetterTopic,
		MaxDeliveryAttempts: int32(dlp.MaxDeliveryAttempts),
	}
}
func protoToDLP(pbDLP *pb.DeadLetterPolicy) *DeadLetterPolicy {
	if pbDLP == nil {
		return nil
	}
	return &DeadLetterPolicy{
		DeadLetterTopic:     pbDLP.GetDeadLetterTopic(),
		MaxDeliveryAttempts: int(pbDLP.MaxDeliveryAttempts),
	}
}

// RetryPolicy specifies how Cloud Pub/Sub retries message delivery.
//
// Retry delay will be exponential based on provided minimum and maximum
// backoffs. https://en.wikipedia.org/wiki/Exponential_backoff.
//
// RetryPolicy will be triggered on NACKs or acknowledgement deadline exceeded
// events for a given message.
//
// Retry Policy is implemented on a best effort basis. At times, the delay
// between consecutive deliveries may not match the configuration. That is,
// delay can be more or less than configured backoff.
type RetryPolicy struct {
	// MinimumBackoff is the minimum delay between consecutive deliveries of a
	// given message. Value should be between 0 and 600 seconds. Defaults to 10 seconds.
	MinimumBackoff optional.Duration
	// MaximumBackoff is the maximum delay between consecutive deliveries of a
	// given message. Value should be between 0 and 600 seconds. Defaults to 600 seconds.
	MaximumBackoff optional.Duration
}

func (rp *RetryPolicy) toProto() *pb.RetryPolicy {
	if rp == nil {
		return nil
	}
	// If RetryPolicy is the empty struct, take this as an instruction
	// to remove RetryPolicy from the subscription.
	if rp.MinimumBackoff == nil && rp.MaximumBackoff == nil {
		return nil
	}

	// Initialize minDur and maxDur to be negative, such that if the conversion from an
	// optional fails, RetryPolicy won't be updated in the proto as it will remain nil.
	var minDur time.Duration = -1
	var maxDur time.Duration = -1
	if rp.MinimumBackoff != nil {
		minDur = optional.ToDuration(rp.MinimumBackoff)
	}
	if rp.MaximumBackoff != nil {
		maxDur = optional.ToDuration(rp.MaximumBackoff)
	}

	var minDurPB, maxDurPB *durpb.Duration
	if minDur > 0 {
		minDurPB = durpb.New(minDur)
	}
	if maxDur > 0 {
		maxDurPB = durpb.New(maxDur)
	}

	return &pb.RetryPolicy{
		MinimumBackoff: minDurPB,
		MaximumBackoff: maxDurPB,
	}
}

func protoToRetryPolicy(rp *pb.RetryPolicy) *RetryPolicy {
	if rp == nil {
		return nil
	}
	var minBackoff, maxBackoff time.Duration
	if rp.MinimumBackoff != nil {
		minBackoff = rp.MinimumBackoff.AsDuration()
	}
	if rp.MaximumBackoff != nil {
		maxBackoff = rp.MaximumBackoff.AsDuration()
	}

	retryPolicy := &RetryPolicy{
		MinimumBackoff: minBackoff,
		MaximumBackoff: maxBackoff,
	}
	return retryPolicy
}

// ReceiveSettings configure the Receive method.
// A zero ReceiveSettings will result in values equivalent to DefaultReceiveSettings.
type ReceiveSettings struct {
	// MaxExtension is the maximum period for which the Subscription should
	// automatically extend the ack deadline for each message.
	//
	// The Subscription will automatically extend the ack deadline of all
	// fetched Messages up to the duration specified. Automatic deadline
	// extension beyond the initial receipt may be disabled by specifying a
	// duration less than 0.
	MaxExtension time.Duration

	// MaxExtensionPeriod is the maximum duration by which to extend the ack
	// deadline at a time. The ack deadline will continue to be extended by up
	// to this duration until MaxExtension is reached. Setting MaxExtensionPeriod
	// bounds the maximum amount of time before a message redelivery in the
	// event the subscriber fails to extend the deadline.
	//
	// MaxExtensionPeriod must be between 10s and 600s (inclusive). This configuration
	// can be disabled by specifying a duration less than (or equal to) 0.
	MaxExtensionPeriod time.Duration

	// MinExtensionPeriod is the the min duration for a single lease extension attempt.
	// By default the 99th percentile of ack latency is used to determine lease extension
	// periods but this value can be set to minimize the number of extraneous RPCs sent.
	//
	// MinExtensionPeriod must be between 10s and 600s (inclusive). This configuration
	// can be disabled by specifying a duration less than (or equal to) 0.
	// Defaults to off but set to 60 seconds if the subscription has exactly-once delivery enabled,
	// which will be added in a future release.
	MinExtensionPeriod time.Duration

	// MaxOutstandingMessages is the maximum number of unprocessed messages
	// (unacknowledged but not yet expired). If MaxOutstandingMessages is 0, it
	// will be treated as if it were DefaultReceiveSettings.MaxOutstandingMessages.
	// If the value is negative, then there will be no limit on the number of
	// unprocessed messages.
	MaxOutstandingMessages int

	// MaxOutstandingBytes is the maximum size of unprocessed messages
	// (unacknowledged but not yet expired). If MaxOutstandingBytes is 0, it will
	// be treated as if it were DefaultReceiveSettings.MaxOutstandingBytes. If
	// the value is negative, then there will be no limit on the number of bytes
	// for unprocessed messages.
	MaxOutstandingBytes int

	// UseLegacyFlowControl disables enforcing flow control settings at the Cloud
	// PubSub server and the less accurate method of only enforcing flow control
	// at the client side is used.
	// The default is false.
	UseLegacyFlowControl bool

	// NumGoroutines is the number of goroutines that each datastructure along
	// the Receive path will spawn. Adjusting this value adjusts concurrency
	// along the receive path.
	//
	// NumGoroutines defaults to DefaultReceiveSettings.NumGoroutines.
	//
	// NumGoroutines does not limit the number of messages that can be processed
	// concurrently. Even with one goroutine, many messages might be processed at
	// once, because that goroutine may continually receive messages and invoke the
	// function passed to Receive on them. To limit the number of messages being
	// processed concurrently, set MaxOutstandingMessages.
	NumGoroutines int

	// Synchronous switches the underlying receiving mechanism to unary Pull.
	// When Synchronous is false, the more performant StreamingPull is used.
	// StreamingPull also has the benefit of subscriber affinity when using
	// ordered delivery.
	// When Synchronous is true, NumGoroutines is set to 1 and only one Pull
	// RPC will be made to poll messages at a time.
	// The default is false.
	//
	// Deprecated.
	// Previously, users might use Synchronous mode since StreamingPull had a limitation
	// where MaxOutstandingMessages was not always respected with large batches of
	// small messages. With server side flow control, this is no longer an issue
	// and we recommend switching to the default StreamingPull mode by setting
	// Synchronous to false.
	// Synchronous mode does not work with exactly once delivery.
	Synchronous bool
}

// For synchronous receive, the time to wait if we are already processing
// MaxOutstandingMessages. There is no point calling Pull and asking for zero
// messages, so we pause to allow some message-processing callbacks to finish.
//
// The wait time is large enough to avoid consuming significant CPU, but
// small enough to provide decent throughput. Users who want better
// throughput should not be using synchronous mode.
//
// Waiting might seem like polling, so it's natural to think we could do better by
// noticing when a callback is finished and immediately calling Pull. But if
// callbacks finish in quick succession, this will result in frequent Pull RPCs that
// request a single message, which wastes network bandwidth. Better to wait for a few
// callbacks to finish, so we make fewer RPCs fetching more messages.
//
// This value is unexported so the user doesn't have another knob to think about. Note that
// it is the same value as the one used for nackTicker, so it matches this client's
// idea of a duration that is short, but not so short that we perform excessive RPCs.
const synchronousWaitTime = 100 * time.Millisecond

// DefaultReceiveSettings holds the default values for ReceiveSettings.
var DefaultReceiveSettings = ReceiveSettings{
	MaxExtension:           60 * time.Minute,
	MaxExtensionPeriod:     0,
	MinExtensionPeriod:     0,
	MaxOutstandingMessages: 1000,
	MaxOutstandingBytes:    1e9, // 1G
	NumGoroutines:          10,
}

// Delete deletes the subscription.
func (s *Subscription) Delete(ctx context.Context) error {
	return s.c.subc.DeleteSubscription(ctx, &pb.DeleteSubscriptionRequest{Subscription: s.name})
}

// Exists reports whether the subscription exists on the server.
func (s *Subscription) Exists(ctx context.Context) (bool, error) {
	_, err := s.c.subc.GetSubscription(ctx, &pb.GetSubscriptionRequest{Subscription: s.name})
	if err == nil {
		return true, nil
	}
	if status.Code(err) == codes.NotFound {
		return false, nil
	}
	return false, err
}

// Config fetches the current configuration for the subscription.
func (s *Subscription) Config(ctx context.Context) (SubscriptionConfig, error) {
	pbSub, err := s.c.subc.GetSubscription(ctx, &pb.GetSubscriptionRequest{Subscription: s.name})
	if err != nil {
		return SubscriptionConfig{}, err
	}
	cfg, err := protoToSubscriptionConfig(pbSub, s.c)
	if err != nil {
		return SubscriptionConfig{}, err
	}
	return cfg, nil
}

// SubscriptionConfigToUpdate describes how to update a subscription.
type SubscriptionConfigToUpdate struct {
	// If non-nil, the push config is changed. Cannot be set at the same time as BigQueryConfig.
	// If currently in push mode, set this value to the zero value to revert to a Pull based subscription.
	PushConfig *PushConfig

	// If non-nil, the bigquery config is changed. Cannot be set at the same time as PushConfig.
	// If currently in bigquery mode, set this value to the zero value to revert to a Pull based subscription,
	BigQueryConfig *BigQueryConfig

	// If non-zero, the ack deadline is changed.
	AckDeadline time.Duration

	// If set, RetainAckedMessages is changed.
	RetainAckedMessages optional.Bool

	// If non-zero, RetentionDuration is changed.
	RetentionDuration time.Duration

	// If non-zero, Expiration is changed.
	ExpirationPolicy optional.Duration

	// If non-nil, DeadLetterPolicy is changed. To remove dead lettering from
	// a subscription, use the zero value for this struct.
	DeadLetterPolicy *DeadLetterPolicy

	// If non-nil, the current set of labels is completely
	// replaced by the new set.
	// This field has beta status. It is not subject to the stability guarantee
	// and may change.
	Labels map[string]string

	// If non-nil, RetryPolicy is changed. To remove an existing retry policy
	// (to redeliver messages as soon as possible) use a pointer to the zero value
	// for this struct.
	RetryPolicy *RetryPolicy

	// If set, EnableExactlyOnce is changed.
	EnableExactlyOnceDelivery optional.Bool
}

// Update changes an existing subscription according to the fields set in cfg.
// It returns the new SubscriptionConfig.
//
// Update returns an error if no fields were modified.
func (s *Subscription) Update(ctx context.Context, cfg SubscriptionConfigToUpdate) (SubscriptionConfig, error) {
	req := s.updateRequest(&cfg)
	if err := cfg.validate(); err != nil {
		return SubscriptionConfig{}, fmt.Errorf("pubsub: UpdateSubscription %w", err)
	}
	if len(req.UpdateMask.Paths) == 0 {
		return SubscriptionConfig{}, errors.New("pubsub: UpdateSubscription call with nothing to update")
	}
	rpsub, err := s.c.subc.UpdateSubscription(ctx, req)
	if err != nil {
		return SubscriptionConfig{}, err
	}
	return protoToSubscriptionConfig(rpsub, s.c)
}

func (s *Subscription) updateRequest(cfg *SubscriptionConfigToUpdate) *pb.UpdateSubscriptionRequest {
	psub := &pb.Subscription{Name: s.name}
	var paths []string
	if cfg.PushConfig != nil {
		psub.PushConfig = cfg.PushConfig.toProto()
		paths = append(paths, "push_config")
	}
	if cfg.BigQueryConfig != nil {
		psub.BigqueryConfig = cfg.BigQueryConfig.toProto()
		paths = append(paths, "bigquery_config")
	}
	if cfg.AckDeadline != 0 {
		psub.AckDeadlineSeconds = trunc32(int64(cfg.AckDeadline.Seconds()))
		paths = append(paths, "ack_deadline_seconds")
	}
	if cfg.RetainAckedMessages != nil {
		psub.RetainAckedMessages = optional.ToBool(cfg.RetainAckedMessages)
		paths = append(paths, "retain_acked_messages")
	}
	if cfg.RetentionDuration != 0 {
		psub.MessageRetentionDuration = durpb.New(cfg.RetentionDuration)
		paths = append(paths, "message_retention_duration")
	}
	if cfg.ExpirationPolicy != nil {
		psub.ExpirationPolicy = expirationPolicyToProto(cfg.ExpirationPolicy)
		paths = append(paths, "expiration_policy")
	}
	if cfg.DeadLetterPolicy != nil {
		psub.DeadLetterPolicy = cfg.DeadLetterPolicy.toProto()
		paths = append(paths, "dead_letter_policy")
	}
	if cfg.Labels != nil {
		psub.Labels = cfg.Labels
		paths = append(paths, "labels")
	}
	if cfg.RetryPolicy != nil {
		psub.RetryPolicy = cfg.RetryPolicy.toProto()
		paths = append(paths, "retry_policy")
	}
	if cfg.EnableExactlyOnceDelivery != nil {
		psub.EnableExactlyOnceDelivery = optional.ToBool(cfg.EnableExactlyOnceDelivery)
		paths = append(paths, "enable_exactly_once_delivery")
	}
	return &pb.UpdateSubscriptionRequest{
		Subscription: psub,
		UpdateMask:   &fmpb.FieldMask{Paths: paths},
	}
}

const (
	// The minimum expiration policy duration is 1 day as per:
	//    https://github.com/googleapis/googleapis/blob/51145ff7812d2bb44c1219d0b76dac92a8bd94b2/google/pubsub/v1/pubsub.proto#L606-L607
	minExpirationPolicy = 24 * time.Hour

	// If an expiration policy is not specified, the default of 31 days is used as per:
	//    https://github.com/googleapis/googleapis/blob/51145ff7812d2bb44c1219d0b76dac92a8bd94b2/google/pubsub/v1/pubsub.proto#L605-L606
	defaultExpirationPolicy = 31 * 24 * time.Hour
)

func (cfg *SubscriptionConfigToUpdate) validate() error {
	if cfg == nil || cfg.ExpirationPolicy == nil {
		return nil
	}
	expPolicy, min := optional.ToDuration(cfg.ExpirationPolicy), minExpirationPolicy
	if expPolicy != 0 && expPolicy < min {
		return fmt.Errorf("invalid expiration policy(%q) < minimum(%q)", expPolicy, min)
	}
	return nil
}

func expirationPolicyToProto(expirationPolicy optional.Duration) *pb.ExpirationPolicy {
	if expirationPolicy == nil {
		return nil
	}

	dur := optional.ToDuration(expirationPolicy)
	var ttl *durpb.Duration
	// As per:
	//    https://godoc.org/google.golang.org/genproto/googleapis/pubsub/v1#ExpirationPolicy.Ttl
	// if ExpirationPolicy.Ttl is set to nil, the expirationPolicy is toggled to NEVER expire.
	if dur != 0 {
		ttl = durpb.New(dur)
	}
	return &pb.ExpirationPolicy{
		Ttl: ttl,
	}
}

// IAM returns the subscription's IAM handle.
func (s *Subscription) IAM() *iam.Handle {
	return iam.InternalNewHandle(s.c.subc.Connection(), s.name)
}

// CreateSubscription creates a new subscription on a topic.
//
// id is the name of the subscription to create. It must start with a letter,
// and contain only letters ([A-Za-z]), numbers ([0-9]), dashes (-),
// underscores (_), periods (.), tildes (~), plus (+) or percent signs (%). It
// must be between 3 and 255 characters in length, and must not start with
// "goog".
//
// cfg.Topic is the topic from which the subscription should receive messages. It
// need not belong to the same project as the subscription. This field is required.
//
// cfg.AckDeadline is the maximum time after a subscriber receives a message before
// the subscriber should acknowledge the message. It must be between 10 and 600
// seconds (inclusive), and is rounded down to the nearest second. If the
// provided ackDeadline is 0, then the default value of 10 seconds is used.
// Note: messages which are obtained via Subscription.Receive need not be
// acknowledged within this deadline, as the deadline will be automatically
// extended.
//
// cfg.PushConfig may be set to configure this subscription for push delivery.
//
// If the subscription already exists an error will be returned.
func (c *Client) CreateSubscription(ctx context.Context, id string, cfg SubscriptionConfig) (*Subscription, error) {
	if cfg.Topic == nil {
		return nil, errors.New("pubsub: require non-nil Topic")
	}
	if cfg.AckDeadline == 0 {
		cfg.AckDeadline = 10 * time.Second
	}
	if d := cfg.AckDeadline; d < 10*time.Second || d > 600*time.Second {
		return nil, fmt.Errorf("ack deadline must be between 10 and 600 seconds; got: %v", d)
	}

	sub := c.Subscription(id)
	_, err := c.subc.CreateSubscription(ctx, cfg.toProto(sub.name))
	if err != nil {
		return nil, err
	}
	return sub, nil
}

var errReceiveInProgress = errors.New("pubsub: Receive already in progress for this subscription")

// Receive calls f with the outstanding messages from the subscription.
// It blocks until ctx is done, or the service returns a non-retryable error.
//
// The standard way to terminate a Receive is to cancel its context:
//
//	cctx, cancel := context.WithCancel(ctx)
//	err := sub.Receive(cctx, callback)
//	// Call cancel from callback, or another goroutine.
//
// If the service returns a non-retryable error, Receive returns that error after
// all of the outstanding calls to f have returned. If ctx is done, Receive
// returns nil after all of the outstanding calls to f have returned and
// all messages have been acknowledged or have expired.
//
// Receive calls f concurrently from multiple goroutines. It is encouraged to
// process messages synchronously in f, even if that processing is relatively
// time-consuming; Receive will spawn new goroutines for incoming messages,
// limited by MaxOutstandingMessages and MaxOutstandingBytes in ReceiveSettings.
//
// The context passed to f will be canceled when ctx is Done or there is a
// fatal service error.
//
// Receive will send an ack deadline extension on message receipt, then
// automatically extend the ack deadline of all fetched Messages up to the
// period specified by s.ReceiveSettings.MaxExtension.
//
// Each Subscription may have only one invocation of Receive active at a time.
func (s *Subscription) Receive(ctx context.Context, f func(context.Context, *Message)) error {
	s.mu.Lock()
	if s.receiveActive {
		s.mu.Unlock()
		return errReceiveInProgress
	}
	s.receiveActive = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.receiveActive = false; s.mu.Unlock() }()

	s.checkOrdering(ctx)

	// TODO(hongalex): move settings check to a helper function to make it more testable
	maxCount := s.ReceiveSettings.MaxOutstandingMessages
	if maxCount == 0 {
		maxCount = DefaultReceiveSettings.MaxOutstandingMessages
	}
	maxBytes := s.ReceiveSettings.MaxOutstandingBytes
	if maxBytes == 0 {
		maxBytes = DefaultReceiveSettings.MaxOutstandingBytes
	}
	maxExt := s.ReceiveSettings.MaxExtension
	if maxExt == 0 {
		maxExt = DefaultReceiveSettings.MaxExtension
	} else if maxExt < 0 {
		// If MaxExtension is negative, disable automatic extension.
		maxExt = 0
	}
	maxExtPeriod := s.ReceiveSettings.MaxExtensionPeriod
	if maxExtPeriod < 0 {
		maxExtPeriod = DefaultReceiveSettings.MaxExtensionPeriod
	}
	minExtPeriod := s.ReceiveSettings.MinExtensionPeriod
	if minExtPeriod < 0 {
		minExtPeriod = DefaultReceiveSettings.MinExtensionPeriod
	}

	var numGoroutines int
	switch {
	case s.ReceiveSettings.Synchronous:
		numGoroutines = 1
	case s.ReceiveSettings.NumGoroutines >= 1:
		numGoroutines = s.ReceiveSettings.NumGoroutines
	default:
		numGoroutines = DefaultReceiveSettings.NumGoroutines
	}
	// TODO(jba): add tests that verify that ReceiveSettings are correctly processed.
	po := &pullOptions{
		maxExtension:           maxExt,
		maxExtensionPeriod:     maxExtPeriod,
		minExtensionPeriod:     minExtPeriod,
		maxPrefetch:            trunc32(int64(maxCount)),
		synchronous:            s.ReceiveSettings.Synchronous,
		maxOutstandingMessages: maxCount,
		maxOutstandingBytes:    maxBytes,
		useLegacyFlowControl:   s.ReceiveSettings.UseLegacyFlowControl,
	}
	fc := newSubscriptionFlowController(FlowControlSettings{
		MaxOutstandingMessages: maxCount,
		MaxOutstandingBytes:    maxBytes,
		LimitExceededBehavior:  FlowControlBlock,
	})

	sched := scheduler.NewReceiveScheduler(maxCount)

	// Wait for all goroutines started by Receive to return, so instead of an
	// obscure goroutine leak we have an obvious blocked call to Receive.
	group, gctx := errgroup.WithContext(ctx)

	type closeablePair struct {
		wg   *sync.WaitGroup
		iter *messageIterator
	}

	var pairs []closeablePair

	// Cancel a sub-context which, when we finish a single receiver, will kick
	// off the context-aware callbacks and the goroutine below (which stops
	// all receivers, iterators, and the scheduler).
	ctx2, cancel2 := context.WithCancel(gctx)
	defer cancel2()

	for i := 0; i < numGoroutines; i++ {
		// The iterator does not use the context passed to Receive. If it did,
		// canceling that context would immediately stop the iterator without
		// waiting for unacked messages.
		iter := newMessageIterator(s.c.subc, s.name, po)

		// We cannot use errgroup from Receive here. Receive might already be
		// calling group.Wait, and group.Wait cannot be called concurrently with
		// group.Go. We give each receive() its own WaitGroup instead.
		//
		// Since wg.Add is only called from the main goroutine, wg.Wait is
		// guaranteed to be called after all Adds.
		var wg sync.WaitGroup
		wg.Add(1)
		pairs = append(pairs, closeablePair{wg: &wg, iter: iter})

		group.Go(func() error {
			defer wg.Wait()
			defer cancel2()
			for {
				var maxToPull int32 // maximum number of messages to pull
				if po.synchronous {
					if po.maxPrefetch < 0 {
						// If there is no limit on the number of messages to
						// pull, use a reasonable default.
						maxToPull = 1000
					} else {
						// Limit the number of messages in memory to MaxOutstandingMessages
						// (here, po.maxPrefetch). For each message currently in memory, we have
						// called fc.acquire but not fc.release: this is fc.count(). The next
						// call to Pull should fetch no more than the difference between these
						// values.
						maxToPull = po.maxPrefetch - int32(fc.count())
						if maxToPull <= 0 {
							// Wait for some callbacks to finish.
							if err := gax.Sleep(ctx, synchronousWaitTime); err != nil {
								// Return nil if the context is done, not err.
								return nil
							}
							continue
						}
					}
				}
				// If the context is done, don't pull more messages.
				select {
				case <-ctx.Done():
					return nil
				default:
				}
				msgs, err := iter.receive(maxToPull)
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
				// If context is done and messages have been pulled,
				// nack them.
				select {
				case <-ctx.Done():
					for _, m := range msgs {
						m.Nack()
					}
					return nil
				default:
				}
				for i, msg := range msgs {
					msg := msg
					// TODO(jba): call acquire closer to when the message is allocated.
					if err := fc.acquire(ctx, len(msg.Data)); err != nil {
						// TODO(jba): test that these "orphaned" messages are nacked immediately when ctx is done.
						for _, m := range msgs[i:] {
							m.Nack()
						}
						// Return nil if the context is done, not err.
						return nil
					}
					iter.eoMu.RLock()
					ackh, _ := msgAckHandler(msg, iter.enableExactlyOnceDelivery)
					iter.eoMu.RUnlock()
					old := ackh.doneFunc
					msgLen := len(msg.Data)
					ackh.doneFunc = func(ackID string, ack bool, r *ipubsub.AckResult, receiveTime time.Time) {
						defer fc.release(ctx, msgLen)
						old(ackID, ack, r, receiveTime)
					}
					wg.Add(1)
					// Make sure the subscription has ordering enabled before adding to scheduler.
					var key string
					if s.enableOrdering {
						key = msg.OrderingKey
					}
					// TODO(deklerk): Can we have a generic handler at the
					// constructor level?
					if err := sched.Add(key, msg, func(msg interface{}) {
						defer wg.Done()
						f(ctx2, msg.(*Message))
					}); err != nil {
						wg.Done()
						// If there are any errors with scheduling messages,
						// nack them so they can be redelivered.
						msg.Nack()
						// Currently, only this error is returned by the receive scheduler.
						if errors.Is(err, scheduler.ErrReceiveDraining) {
							return nil
						}
						return err
					}
				}
			}
		})
	}

	go func() {
		<-ctx2.Done()

		// Wait for all iterators to stop.
		for _, p := range pairs {
			p.iter.stop()
			p.wg.Done()
		}

		// This _must_ happen after every iterator has stopped, or some
		// iterator will still have undelivered messages but the scheduler will
		// already be shut down.
		sched.Shutdown()
	}()

	return group.Wait()
}

// checkOrdering calls Config to check theEnableMessageOrdering field.
// If this call fails (e.g. because the service account doesn't have
// the roles/viewer or roles/pubsub.viewer role) we will assume
// EnableMessageOrdering to be true.
// See: https://github.com/googleapis/google-cloud-go/issues/3884
func (s *Subscription) checkOrdering(ctx context.Context) {
	cfg, err := s.Config(ctx)
	if err != nil {
		s.enableOrdering = true
	} else {
		s.enableOrdering = cfg.EnableMessageOrdering
	}
}

type pullOptions struct {
	maxExtension       time.Duration // the maximum time to extend a message's ack deadline in total
	maxExtensionPeriod time.Duration // the maximum time to extend a message's ack deadline per modack rpc
	minExtensionPeriod time.Duration // the minimum time to extend a message's lease duration per modack
	maxPrefetch        int32         // the max number of outstanding messages, used to calculate maxToPull
	// If true, use unary Pull instead of StreamingPull, and never pull more
	// than maxPrefetch messages.
	synchronous            bool
	maxOutstandingMessages int
	maxOutstandingBytes    int
	useLegacyFlowControl   bool
}
