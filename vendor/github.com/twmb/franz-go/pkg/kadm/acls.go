package kadm

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// ACLBuilder is a builder that is used for batch creating / listing / deleting
// ACLS.
//
// An ACL consists of five components:
//
//   - the user (principal)
//   - the host the user runs on
//   - what resource to access (topic name, group id, etc.)
//   - the operation (read, write)
//   - whether to allow or deny the above
//
// This builder allows for adding the above five components in batches and then
// creating, listing, or deleting a batch of ACLs in one go. This builder
// merges the fifth component (allowing or denying) into allowing principals
// and hosts and denying principals and hosts. The builder must always have an
// Allow or Deny. For creating, the host is optional and defaults to the
// wildcard * that allows or denies all hosts. For listing / deleting, the host
// is also required (specifying no hosts matches all hosts, but you must
// specify this).
//
// Building works on a multiplying factor: every user, every host, every
// resource, and every operation is combined (principals * hosts * resources *
// operations).
//
// With the Kafka simple authorizer (and most reimplementations), all
// principals are required to have the "User:" prefix. The PrefixUserExcept
// function can be used to easily add the "User:" prefix if missing.
//
// The full set of operations and which requests require what operations is
// described in a large doc comment on the ACLOperation type.
//
// Lastly, resources to access / deny access to can be created / matched based
// on literal (exact) names, or on prefix names, or more. See the ACLPattern
// docs for more information.
type ACLBuilder struct {
	any         []string
	anyResource bool
	topics      []string
	anyTopic    bool
	groups      []string
	anyGroup    bool
	anyCluster  bool
	txnIDs      []string
	anyTxn      bool
	tokens      []string
	anyToken    bool

	allow         []string
	anyAllow      bool
	allowHosts    []string
	anyAllowHosts bool
	deny          []string
	anyDeny       bool
	denyHosts     []string
	anyDenyHosts  bool

	ops []ACLOperation

	pattern ACLPattern
}

// PrefixUser prefixes all allowed and denied principals with "User:".
func (b *ACLBuilder) PrefixUser() {
	b.PrefixUserExcept()
}

// PrefixUserExcept prefixes all allowed and denied principals with "User:",
// unless they have any of the given except prefixes.
func (b *ACLBuilder) PrefixUserExcept(except ...string) {
	replace := func(u string) string {
		if !strings.HasPrefix(u, "User:") {
			for _, e := range except {
				if strings.HasPrefix(u, e) {
					return u
				}
			}
			return "User:" + u
		}
		return u
	}

	for i, u := range b.allow {
		b.allow[i] = replace(u)
	}
	for i, u := range b.deny {
		b.deny[i] = replace(u)
	}
}

// NewACLs returns a new ACL builder.
func NewACLs() *ACLBuilder {
	return new(ACLBuilder)
}

// AnyResource lists & deletes ACLs of any type matching the given names
// (pending other filters). If no names are given, this matches all names.
//
// This returns the input pointer.
//
// This function does nothing for creating.
func (b *ACLBuilder) AnyResource(name ...string) *ACLBuilder {
	b.any = name
	if len(name) == 0 {
		b.anyResource = true
	}
	return b
}

// Topics lists/deletes/creates ACLs of resource type "topic" for the given
// topics.
//
// This returns the input pointer.
//
// For listing or deleting, if this is provided no topics, all "topic" resource
// type ACLs are matched. For creating, if no topics are provided, this
// function does nothing.
func (b *ACLBuilder) Topics(t ...string) *ACLBuilder {
	b.topics = t
	if len(t) == 0 {
		b.anyTopic = true
	}
	return b
}

// MaybeTopics is the same as Topics, but does not match all topics if none are
// provided.
func (b *ACLBuilder) MaybeTopics(t ...string) *ACLBuilder { b.topics = t; return b }

// Groups lists/deletes/creates ACLs of resource type "group" for the given
// groups.
//
// This returns the input pointer.
//
// For listing or deleting, if this is provided no groups, all "group" resource
// type ACLs are matched. For creating, if no groups are provided, this
// function does nothing.
func (b *ACLBuilder) Groups(g ...string) *ACLBuilder {
	b.groups = g
	if len(g) == 0 {
		b.anyGroup = true
	}
	return b
}

// MaybeGroups is the same as Groups, but does not match all groups if none are
// provided.
func (b *ACLBuilder) MaybeGroups(g ...string) *ACLBuilder { b.groups = g; return b }

// Clusters lists/deletes/creates ACLs of resource type "cluster".
//
// This returns the input pointer.
//
// There is only one type of cluster in Kafka, "kafka-cluster". Opting in to
// listing or deleting by cluster inherently matches all ACLS of resource type
// cluster. For creating, this function allows for creating cluster ACLs.
func (b *ACLBuilder) Clusters() *ACLBuilder {
	b.anyCluster = true
	return b
}

// MaybeClusters is the same as Clusters, but only matches clusters if c is
// true.
func (b *ACLBuilder) MaybeClusters(c bool) *ACLBuilder { b.anyCluster = c; return b }

// TransactionalIDs lists/deletes/creates ACLs of resource type
// "transactional_id" for the given transactional IDs.
//
// This returns the input pointer.
//
// For listing or deleting, if this is provided no IDs, all "transactional_id"
// resource type ACLs matched. For creating, if no IDs are provided, this
// function does nothing.
func (b *ACLBuilder) TransactionalIDs(x ...string) *ACLBuilder {
	b.txnIDs = x
	if len(x) == 0 {
		b.anyTxn = true
	}
	return b
}

// MaybeTransactionalIDs is the same as TransactionalIDs, but does not match
// all transactional ID's if none are provided.
func (b *ACLBuilder) MaybeTransactionalIDs(x ...string) *ACLBuilder { b.txnIDs = x; return b }

// DelegationTokens lists/deletes/creates ACLs of resource type
// "delegation_token" for the given delegation tokens.
//
// This returns the input pointer.
//
// For listing or deleting, if this is provided no tokens, all
// "delegation_token" resource type ACLs are matched. For creating, if no
// tokens are provided, this function does nothing.
func (b *ACLBuilder) DelegationTokens(t ...string) *ACLBuilder {
	b.tokens = t
	if len(t) == 0 {
		b.anyToken = true
	}
	return b
}

// MaybeDelegationTokens is the same as DelegationTokens, but does not match
// all tokens if none are provided.
func (b *ACLBuilder) MaybeDelegationTokens(t ...string) *ACLBuilder { b.tokens = t; return b }

// Allow sets the principals to add allow permissions for. For listing and
// deleting, you must also use AllowHosts.
//
// This returns the input pointer.
//
// For creating, if this is not paired with AllowHosts, the user will have
// access to all hosts (the wildcard *).
//
// For listing & deleting, if the principals are empty, this matches any user.
func (b *ACLBuilder) Allow(principals ...string) *ACLBuilder {
	b.allow = principals
	if len(principals) == 0 {
		b.anyAllow = true
	}
	return b
}

// MaybeAllow is the same as Allow, but does not match all allowed principals
// if none are provided.
func (b *ACLBuilder) MaybeAllow(principals ...string) *ACLBuilder { b.allow = principals; return b }

// AllowHosts sets the hosts to add allow permissions for. If using this, you
// must also use Allow.
//
// This returns the input pointer.
//
// For creating, if this is empty, the user will have access to all hosts (the
// wildcard *) and this function is actually not necessary.
//
// For listing & deleting, if the hosts are empty, this matches any host.
func (b *ACLBuilder) AllowHosts(hosts ...string) *ACLBuilder {
	b.allowHosts = hosts
	if len(hosts) == 0 {
		b.anyAllowHosts = true
	}
	return b
}

// MaybeAllowHosts is the same as AllowHosts, but does not match all allowed
// hosts if none are provided.
func (b *ACLBuilder) MaybeAllowHosts(hosts ...string) *ACLBuilder { b.allowHosts = hosts; return b }

// Deny sets the principals to add deny permissions for. For listing and
// deleting, you must also use DenyHosts.
//
// This returns the input pointer.
//
// For creating, if this is not paired with DenyHosts, the user will be denied
// access to all hosts (the wildcard *).
//
// For listing & deleting, if the principals are empty, this matches any user.
func (b *ACLBuilder) Deny(principals ...string) *ACLBuilder {
	b.deny = principals
	if len(principals) == 0 {
		b.anyDeny = true
	}
	return b
}

// MaybeDeny is the same as Deny, but does not match all denied principals if
// none are provided.
func (b *ACLBuilder) MaybeDeny(principals ...string) *ACLBuilder { b.deny = principals; return b }

// DenyHosts sets the hosts to add deny permissions for. If using this, you
// must also use Deny.
//
// This returns the input pointer.
//
// For creating, if this is empty, the user will be denied access to all hosts
// (the wildcard *) and this function is actually not necessary.
//
// For listing & deleting, if the hosts are empty, this matches any host.
func (b *ACLBuilder) DenyHosts(hosts ...string) *ACLBuilder {
	b.denyHosts = hosts
	if len(hosts) == 0 {
		b.anyDenyHosts = true
	}
	return b
}

// MaybeDenyHosts is the same as DenyHosts, but does not match all denied
// hosts if none are provided.
func (b *ACLBuilder) MaybeDenyHosts(hosts ...string) *ACLBuilder { b.denyHosts = hosts; return b }

// ACLOperation is a type alias for kmsg.ACLOperation, which is an enum
// containing all Kafka ACL operations and has helper functions.
//
// Kafka requests require the following operations (broker <=> broker ACLs
// elided):
//
//	PRODUCING/CONSUMING
//	===================
//	Produce      WRITE on TOPIC for topics
//	             WRITE on TRANSACTIONAL_ID for txn id (if transactionally producing)
//
//	Fetch        READ on TOPIC for topics
//
//	ListOffsets  DESCRIBE on TOPIC for topics
//
//	Metadata     DESCRIBE on TOPIC for topics
//	             CREATE on CLUSTER for kafka-cluster (if automatically creating new topics)
//	             CREATE on TOPIC for topics (if automatically creating new topics)
//
//	OffsetForLeaderEpoch  DESCRIBE on TOPIC for topics
//
//	GROUPS
//	======
//	FindCoordinator  DESCRIBE on GROUP for group (if finding group coordinator)
//	                 DESCRIBE on TRANSACTIONAL_ID for id (if finding transactiona coordinator)
//
//	OffsetCommit     READ on GROUP for group
//	                 READ on TOPIC for topics
//
//	OffsetFetch      DESCRIBE on GROUP for group
//	                 DESCRIBE on TOPIC for topics
//
//	OffsetDelete     DELETE on GROUP For group
//	                 READ on TOPIC for topics
//
//	JoinGroup        READ on GROUP for group
//	Heartbeat        READ on GROUP for group
//	LeaveGroup       READ on GROUP for group
//	SyncGroup        READ on GROUP for group
//
//	DescribeGroup    DESCRIBE on GROUP for groups
//
//	ListGroups       DESCRIBE on GROUP for groups
//	                 or, DESCRIBE on CLUSTER for kafka-cluster
//
//	DeleteGroups     DELETE on GROUP for groups
//
//	TRANSACTIONS (including FindCoordinator above)
//	============
//	InitProducerID      WRITE on TRANSACTIONAL_ID for id, if using transactions
//	                    or, IDEMPOTENT_WRITE on CLUSTER for kafka-cluster, if pre Kafka 3.0
//	                    or, WRITE on TOPIC for any topic, if Kafka 3.0+
//
//	AddPartitionsToTxn  WRITE on TRANSACTIONAL_ID for id
//	                    WRITE on TOPIC for topics
//
//	AddOffsetsToTxn     WRITE on TRANSACTIONAL_ID for id
//	                    READ on GROUP for group
//
//	EndTxn              WRITE on TRANSACTIONAL_ID for id
//
//	TxnOffsetCommit     WRITE on TRANSACTIONAL_ID for id
//	                    READ on GROUP for group
//	                    READ on TOPIC for topics
//
//	TOPIC ADMIN
//	===========
//	CreateTopics      CREATE on CLUSTER for kafka-cluster
//	                  CREATE on TOPIC for topics
//	                  DESCRIBE_CONFIGS on TOPIC for topics, for returning topic configs on create
//
//	CreatePartitions  ALTER on TOPIC for topics
//
//	DeleteTopics      DELETE on TOPIC for topics
//	                  DESCRIBE on TOPIC for topics, if deleting by topic id (in addition to prior ACL)
//
//	DeleteRecords     DELETE on TOPIC for topics
//
//	CONFIG ADMIN
//	============
//	DescribeConfigs          DESCRIBE_CONFIGS on CLUSTER for kafka-cluster, for broker or broker-logger describing
//	                         DESCRIBE_CONFIGS on TOPIC for topics, for topic describing
//
//	AlterConfigs             ALTER_CONFIGS on CLUSTER for kafka-cluster, for broker altering
//	                         ALTER_CONFIGS on TOPIC for topics, for topic altering
//
//	IncrementalAlterConfigs  ALTER_CONFIGS on CLUSTER for kafka-cluster, for broker or broker-logger altering
//	                         ALTER_CONFIGS on TOPIC for topics, for topic altering
//
//
//	MISC ADMIN
//	==========
//	AlterReplicaLogDirs  ALTER on CLUSTER for kafka-cluster
//	DescribeLogDirs      DESCRIBE on CLUSTER for kafka-cluster
//
//	AlterPartitionAssignments   ALTER on CLUSTER for kafka-cluster
//	ListPartitionReassignments  DESCRIBE on CLUSTER for kafka-cluster
//
//	DescribeDelegationTokens    DESCRIBE on DELEGATION_TOKEN for id
//
//	ElectLeaders          ALTER on CLUSTER for kafka-cluster
//
//	DescribeClientQuotas  DESCRIBE_CONFIGS on CLUSTER for kafka-cluster
//	AlterClientQuotas     ALTER_CONFIGS on CLUSTER for kafka-cluster
//
//	DescribeUserScramCredentials  DESCRIBE on CLUSTER for kafka-cluster
//	AlterUserScramCredentials     ALTER on CLUSTER for kafka-cluster
//
//	UpdateFeatures        ALTER on CLUSTER for kafka-cluster
//
//	DescribeCluster       DESCRIBE on CLUSTER for kafka-cluster
//
//	DescribeProducerIDs   READ on TOPIC for topics
//	DescribeTransactions  DESCRIBE on TRANSACTIONAL_ID for ids
//	                      DESCRIBE on TOPIC for topics
//	ListTransactions      DESCRIBE on TRANSACTIONAL_ID for ids
type ACLOperation = kmsg.ACLOperation

const (
	// OpUnknown is returned for unknown operations.
	OpUnknown ACLOperation = kmsg.ACLOperationUnknown

	// OpAny, used for listing and deleting, matches any operation.
	OpAny ACLOperation = kmsg.ACLOperationAny

	// OpAll is a shortcut for allowing / denying all operations.
	OpAll ACLOperation = kmsg.ACLOperationAll

	// OpRead is the READ operation.
	OpRead ACLOperation = kmsg.ACLOperationRead

	// OpWrite is the WRITE operation.
	OpWrite ACLOperation = kmsg.ACLOperationWrite

	// OpCreate is the CREATE operation.
	OpCreate ACLOperation = kmsg.ACLOperationCreate

	// OpDelete is the DELETE operation.
	OpDelete ACLOperation = kmsg.ACLOperationDelete

	// OpAlter is the ALTER operation.
	OpAlter ACLOperation = kmsg.ACLOperationAlter

	// OpDescribe is the DESCRIBE operation.
	OpDescribe ACLOperation = kmsg.ACLOperationDescribe

	// OpClusterAction is the CLUSTER_ACTION operation. This operation is
	// used for any broker<=>broker communication and is not needed by
	// clients.
	OpClusterAction ACLOperation = kmsg.ACLOperationClusterAction

	// OpDescribeConfigs is the DESCRIBE_CONFIGS operation.
	OpDescribeConfigs ACLOperation = kmsg.ACLOperationDescribeConfigs

	// OpAlterConfigs is the ALTER_CONFIGS operation.
	OpAlterConfigs ACLOperation = kmsg.ACLOperationAlterConfigs

	// OpIdempotentWrite is the IDEMPOTENT_WRITE operation. As of Kafka
	// 3.0+, this has been deprecated and replaced by the ability to WRITE
	// on any topic.
	OpIdempotentWrite ACLOperation = kmsg.ACLOperationIdempotentWrite
)

// Operations sets operations to allow or deny. Passing no operations defaults
// to OpAny.
//
// This returns the input pointer.
//
// For creating, OpAny returns an error, for it is strictly used for filters
// (listing & deleting).
func (b *ACLBuilder) Operations(operations ...ACLOperation) *ACLBuilder {
	b.ops = operations
	if len(operations) == 0 {
		b.ops = []ACLOperation{OpAny}
	}
	return b
}

// MaybeOperations is the same as Operations, but does not match all operations
// if none are provided.
func (b *ACLBuilder) MaybeOperations(operations ...ACLOperation) *ACLBuilder {
	if len(operations) > 0 {
		b.Operations(operations...)
	}
	return b
}

// ACLPattern is a type alias for kmsg.ACLResourcePatternType, which is an enum
// containing all Kafka ACL resource pattern options.
//
// Creating/listing/deleting ACLs works on a resource name basis: every ACL
// created has a name, and every ACL filtered for listing / deleting matches by
// name. The name by default is "literal", meaning created ACLs will have the
// exact name, and matched ACLs must match completely.
//
// Prefixed names allow for creating an ACL that matches any prefix: principals
// foo-bar and foo-baz both have the prefix "foo-", meaning a READ on TOPIC for
// User:foo- with prefix pattern will allow both of those principals to read
// the topic.
//
// Any and match are used for listing and deleting. Any will match any name, be
// it literal or prefix or a wildcard name. There is no need for specifying
// topics, groups, etc. when using any resource pattern.
//
// Alternatively, match requires a name, but it matches any literal name (exact
// match), any prefix, and any wildcard.
type ACLPattern = kmsg.ACLResourcePatternType

const (
	// ACLPatternUnknown is returned for unknown patterns.
	ACLPatternUnknown ACLPattern = kmsg.ACLResourcePatternTypeUnknown

	// ACLPatternAny is the ANY resource pattern.
	ACLPatternAny ACLPattern = kmsg.ACLResourcePatternTypeAny

	// ACLPatternMatch is the MATCH resource pattern.
	ACLPatternMatch ACLPattern = kmsg.ACLResourcePatternTypeMatch

	// ACLPatternLiteral is the LITERAL resource pattern, the default.
	ACLPatternLiteral ACLPattern = kmsg.ACLResourcePatternTypeLiteral

	// ACLPatternPrefixed is the PREFIXED resource pattern.
	ACLPatternPrefixed ACLPattern = kmsg.ACLResourcePatternTypePrefixed
)

// ResourcePatternType sets the pattern type to use when creating or filtering
// ACL resource names, overriding the default of LITERAL.
//
// This returns the input pointer.
//
// For creating, only LITERAL and PREFIXED are supported.
func (b *ACLBuilder) ResourcePatternType(pattern ACLPattern) *ACLBuilder {
	b.pattern = pattern
	return b
}

// ValidateCreate returns an error if the builder is invalid for creating ACLs.
func (b *ACLBuilder) ValidateCreate() error {
	for _, op := range b.ops {
		switch op {
		case OpAny, OpUnknown:
			return fmt.Errorf("invalid operation %s for creating ACLs", op)
		}
	}

	switch b.pattern {
	case ACLPatternLiteral, ACLPatternPrefixed:
	default:
		return fmt.Errorf("invalid acl resource pattern %s for creating ACLs", b.pattern)
	}

	if len(b.allowHosts) != 0 && len(b.allow) == 0 {
		return fmt.Errorf("invalid allow hosts with no allow principals")
	}
	if len(b.denyHosts) != 0 && len(b.deny) == 0 {
		return fmt.Errorf("invalid deny hosts with no deny principals")
	}
	return nil
}

// ValidateDelete is an alias for ValidateFilter.
func (b *ACLBuilder) ValidateDelete() error { return b.ValidateFilter() }

// ValidateDescribe is an alias for ValidateFilter.
func (b *ACLBuilder) ValidateDescribe() error { return b.ValidateFilter() }

// ValidateFilter returns an error if the builder is invalid for deleting or
// describing ACLs (which both operate on a filter basis).
func (b *ACLBuilder) ValidateFilter() error {
	if len(b.allowHosts) != 0 && len(b.allow) == 0 && !b.anyAllow {
		return fmt.Errorf("invalid allow hosts with no allow principals")
	}
	if len(b.allow) != 0 && len(b.allowHosts) == 0 && !b.anyAllowHosts {
		return fmt.Errorf("invalid allow principals with no allow hosts")
	}
	if len(b.denyHosts) != 0 && len(b.deny) == 0 && !b.anyDeny {
		return fmt.Errorf("invalid deny hosts with no deny principals")
	}
	if len(b.deny) != 0 && len(b.denyHosts) == 0 && !b.anyDenyHosts {
		return fmt.Errorf("invalid deny principals with no deny hosts")
	}
	return nil
}

// HasAnyFilter returns whether any field in this builder is opted into "any",
// meaning a wide glob. This would be if you used Topics with no topics, and so
// on. This function can be used to detect if you accidentally opted into a
// non-specific ACL.
//
// The evaluated fields are: resources, principals/hosts, a single OpAny
// operation, and an Any pattern.
func (b *ACLBuilder) HasAnyFilter() bool {
	return b.anyResource ||
		b.anyTopic ||
		b.anyGroup ||
		b.anyTxn ||
		b.anyToken ||
		b.anyAllow ||
		b.anyAllowHosts ||
		b.anyDeny ||
		b.anyDenyHosts ||
		b.hasOpAny() ||
		b.pattern == ACLPatternAny
}

func (b *ACLBuilder) hasOpAny() bool {
	for _, op := range b.ops {
		if op == OpAny {
			return true
		}
	}
	return false
}

// HasResource returns true if the builder has a non-empty resource (topic,
// group, ...), or if any resource has "any" set to true.
func (b *ACLBuilder) HasResource() bool {
	l := len(b.any) +
		len(b.topics) +
		len(b.groups) +
		len(b.txnIDs) +
		len(b.tokens)
	return l > 0 ||
		b.anyResource ||
		b.anyTopic ||
		b.anyGroup ||
		b.anyCluster ||
		b.anyTxn ||
		b.anyToken
}

// HasPrincipals returns if any allow or deny principals have been set, or if
// their "any" field is true.
func (b *ACLBuilder) HasPrincipals() bool {
	return len(b.allow) > 0 ||
		b.anyAllow ||
		len(b.deny) > 0 ||
		b.anyDeny
}

// HasHosts returns if any allow or deny hosts have been set, or if their "any"
// field is true.
func (b *ACLBuilder) HasHosts() bool {
	return len(b.allowHosts) > 0 ||
		b.anyAllowHosts ||
		len(b.denyHosts) > 0 ||
		b.anyDenyHosts
}

func (b *ACLBuilder) dup() *ACLBuilder { // shallow copy
	d := *b
	return &d
}

// CreateACLsResult is a result for an individual ACL creation.
type CreateACLsResult struct {
	Principal string
	Host      string

	Type       kmsg.ACLResourceType   // Type is the type of resource this is.
	Name       string                 // Name is the name of the resource allowed / denied.
	Pattern    ACLPattern             // Pattern is the name pattern.
	Operation  ACLOperation           // Operation is the operation allowed / denied.
	Permission kmsg.ACLPermissionType // Permission is whether this is allowed / denied.

	Err        error  // Err is the error for this ACL creation.
	ErrMessage string // ErrMessage a potential extra message describing any error.
}

// CreateACLsResults contains all results to created ACLs.
type CreateACLsResults []CreateACLsResult

// CreateACLs creates a batch of ACLs using the ACL builder, validating the
// input before issuing the CreateACLs request.
//
// If the input is invalid, or if the response fails, or if the response does
// not contain as many ACLs as we issued in our create request, this returns an
// error.
func (cl *Client) CreateACLs(ctx context.Context, b *ACLBuilder) (CreateACLsResults, error) {
	if err := b.ValidateCreate(); err != nil {
		return nil, err
	}
	if len(b.allow) != 0 && len(b.allowHosts) == 0 {
		b.allowHosts = []string{"*"}
	}
	if len(b.deny) != 0 && len(b.denyHosts) == 0 {
		b.denyHosts = []string{"*"}
	}

	var clusters []string
	if b.anyCluster {
		clusters = []string{"kafka-cluster"}
	}

	req := kmsg.NewPtrCreateACLsRequest()
	for _, typeNames := range []struct {
		t     kmsg.ACLResourceType
		names []string
	}{
		{kmsg.ACLResourceTypeTopic, b.topics},
		{kmsg.ACLResourceTypeGroup, b.groups},
		{kmsg.ACLResourceTypeCluster, clusters},
		{kmsg.ACLResourceTypeTransactionalId, b.txnIDs},
		{kmsg.ACLResourceTypeDelegationToken, b.tokens},
	} {
		for _, name := range typeNames.names {
			for _, op := range b.ops {
				for _, perm := range []struct {
					principals []string
					hosts      []string
					permType   kmsg.ACLPermissionType
				}{
					{b.allow, b.allowHosts, kmsg.ACLPermissionTypeAllow},
					{b.deny, b.denyHosts, kmsg.ACLPermissionTypeDeny},
				} {
					for _, principal := range perm.principals {
						for _, host := range perm.hosts {
							c := kmsg.NewCreateACLsRequestCreation()
							c.ResourceType = typeNames.t
							c.ResourceName = name
							c.ResourcePatternType = b.pattern
							c.Operation = op
							c.Principal = principal
							c.Host = host
							c.PermissionType = perm.permType
							req.Creations = append(req.Creations, c)
						}
					}
				}
			}
		}
	}

	resp, err := req.RequestWith(ctx, cl.cl)
	if err != nil {
		return nil, err
	}

	if len(resp.Results) != len(req.Creations) {
		return nil, fmt.Errorf("received %d results to %d creations", len(resp.Results), len(req.Creations))
	}

	var rs CreateACLsResults
	for i, r := range resp.Results {
		c := &req.Creations[i]
		rs = append(rs, CreateACLsResult{
			Principal: c.Principal,
			Host:      c.Host,

			Type:       c.ResourceType,
			Name:       c.ResourceName,
			Pattern:    c.ResourcePatternType,
			Operation:  c.Operation,
			Permission: c.PermissionType,

			Err:        kerr.ErrorForCode(r.ErrorCode),
			ErrMessage: unptrStr(r.ErrorMessage),
		})
	}

	return rs, nil
}

// DeletedACL an ACL that was deleted.
type DeletedACL struct {
	Principal string // Principal is this deleted ACL's principal.
	Host      string // Host is this deleted ACL's host.

	Type       kmsg.ACLResourceType   // Type is this deleted ACL's resource type.
	Name       string                 // Name is this deleted ACL's resource name.
	Pattern    ACLPattern             // Pattern is this deleted ACL's resource name pattern.
	Operation  ACLOperation           // Operation is this deleted ACL's operation.
	Permission kmsg.ACLPermissionType // Permission this deleted ACLs permission.

	Err        error  // Err is non-nil if this match has an error.
	ErrMessage string // ErrMessage a potential extra message describing any error.
}

// DeletedACLs contains ACLs that were deleted from a single delete filter.
type DeletedACLs []DeletedACL

// DeleteACLsResult contains the input used for a delete ACL filter, and the
// deletes that the filter matched or the error for this filter.
//
// All fields but Deleted and Err are set from the request input. The response
// sets either Deleted (potentially to nothing if the filter matched nothing)
// or Err.
type DeleteACLsResult struct {
	Principal *string // Principal is the optional user that was used in this filter.
	Host      *string // Host is the optional host that was used in this filter.

	Type       kmsg.ACLResourceType   // Type is the type of resource used for this filter.
	Name       *string                // Name is the name of the resource used for this filter.
	Pattern    ACLPattern             // Pattern is the name pattern used for this filter.
	Operation  ACLOperation           // Operation is the operation used for this filter.
	Permission kmsg.ACLPermissionType // Permission is permission used for this filter.

	Deleted DeletedACLs // Deleted contains all ACLs this delete filter matched.

	Err        error  // Err is non-nil if this filter has an error.
	ErrMessage string // ErrMessage a potential extra message describing any error.
}

// DeleteACLsResults contains all results to deleted ACLs.
type DeleteACLsResults []DeleteACLsResult

// DeleteACLs deletes a batch of ACLs using the ACL builder, validating the
// input before issuing the DeleteACLs request.
//
// If the input is invalid, or if the response fails, or if the response does
// not contain as many ACL results as we issued in our delete request, this
// returns an error.
//
// Deleting ACLs works on a filter basis: a single filter can match many ACLs.
// For example, deleting with operation ANY matches any operation. For safety /
// verification purposes, you an DescribeACLs with the same builder first to
// see what would be deleted.
func (cl *Client) DeleteACLs(ctx context.Context, b *ACLBuilder) (DeleteACLsResults, error) {
	dels, _, err := createDelDescACL(b)
	if err != nil {
		return nil, err
	}

	req := kmsg.NewPtrDeleteACLsRequest()
	req.Filters = dels
	resp, err := req.RequestWith(ctx, cl.cl)
	if err != nil {
		return nil, err
	}
	if len(resp.Results) != len(req.Filters) {
		return nil, fmt.Errorf("received %d results to %d filters", len(resp.Results), len(req.Filters))
	}

	var rs DeleteACLsResults
	for i, r := range resp.Results {
		f := &req.Filters[i]
		var ms DeletedACLs
		for _, m := range r.MatchingACLs {
			ms = append(ms, DeletedACL{
				Principal:  m.Principal,
				Host:       m.Host,
				Type:       m.ResourceType,
				Name:       m.ResourceName,
				Pattern:    m.ResourcePatternType,
				Operation:  m.Operation,
				Permission: m.PermissionType,
				Err:        kerr.ErrorForCode(m.ErrorCode),
				ErrMessage: unptrStr(m.ErrorMessage),
			})
		}
		rs = append(rs, DeleteACLsResult{
			Principal:  f.Principal,
			Host:       f.Host,
			Type:       f.ResourceType,
			Name:       f.ResourceName,
			Pattern:    f.ResourcePatternType,
			Operation:  f.Operation,
			Permission: f.PermissionType,
			Deleted:    ms,
			Err:        kerr.ErrorForCode(r.ErrorCode),
			ErrMessage: unptrStr(r.ErrorMessage),
		})
	}
	return rs, nil
}

// DescribedACL is an ACL that was described.
type DescribedACL struct {
	Principal string // Principal is this described ACL's principal.
	Host      string // Host is this described ACL's host.

	Type       kmsg.ACLResourceType   // Type is this described ACL's resource type.
	Name       string                 // Name is this described ACL's resource name.
	Pattern    ACLPattern             // Pattern is this described ACL's resource name pattern.
	Operation  ACLOperation           // Operation is this described ACL's operation.
	Permission kmsg.ACLPermissionType // Permission this described ACLs permission.
}

// DescribedACLs contains ACLs that were described from a single describe
// filter.
type DescribedACLs []DescribedACL

// DescribeACLsResults contains the input used for a describe ACL filter, and
// the describes that the filter matched or the error for this filter.
//
// All fields but Described and Err are set from the request input. The
// response sets either Described (potentially to nothing if the filter matched
// nothing) or Err.
type DescribeACLsResult struct {
	Principal *string // Principal is the optional user that was used in this filter.
	Host      *string // Host is the optional host that was used in this filter.

	Type       kmsg.ACLResourceType   // Type is the type of resource used for this filter.
	Name       *string                // Name is the name of the resource used for this filter.
	Pattern    ACLPattern             // Pattern is the name pattern used for this filter.
	Operation  ACLOperation           // Operation is the operation used for this filter.
	Permission kmsg.ACLPermissionType // Permission is permission used for this filter.

	Described DescribedACLs // Described contains all ACLs this describe filter matched.

	Err        error  // Err is non-nil if this filter has an error.
	ErrMessage string // ErrMessage a potential extra message describing any error.
}

// DescribeACLsResults contains all results to described ACLs.
type DescribeACLsResults []DescribeACLsResult

// DescribeACLs describes a batch of ACLs using the ACL builder, validating the
// input before issuing DescribeACLs requests.
//
// If the input is invalid, or if any response fails, this returns an error.
//
// Listing ACLs works on a filter basis: a single filter can match many ACLs.
// For example, describing with operation ANY matches any operation. Under the
// hood, this method issues one describe request per filter, because describing
// ACLs does not work on a batch basis (unlike creating & deleting). The return
// of this function can be used to see what would be deleted given the same
// builder input.
func (cl *Client) DescribeACLs(ctx context.Context, b *ACLBuilder) (DescribeACLsResults, error) {
	_, descs, err := createDelDescACL(b)
	if err != nil {
		return nil, err
	}

	var (
		ictx, cancel = context.WithCancel(ctx)
		mu           sync.Mutex
		wg           sync.WaitGroup
		firstErr     error
		resps        = make([]*kmsg.DescribeACLsResponse, len(descs))
	)
	defer cancel()
	for i := range descs {
		req := descs[i] // each req is unique per loop, we are not reusing req, this is safe
		myIdx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := req.RequestWith(ictx, cl.cl)
			resps[myIdx] = resp
			if err == nil {
				return
			}
			cancel()
			mu.Lock()
			defer mu.Unlock()
			if firstErr == nil { // keep the first err
				firstErr = err
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}

	var rs DescribeACLsResults
	for i, r := range resps {
		f := descs[i]
		var ds DescribedACLs
		for _, resource := range r.Resources {
			for _, acl := range resource.ACLs {
				ds = append(ds, DescribedACL{
					Principal:  acl.Principal,
					Host:       acl.Host,
					Type:       resource.ResourceType,
					Name:       resource.ResourceName,
					Pattern:    resource.ResourcePatternType,
					Operation:  acl.Operation,
					Permission: acl.PermissionType,
				})
			}
		}
		rs = append(rs, DescribeACLsResult{
			Principal:  f.Principal,
			Host:       f.Host,
			Type:       f.ResourceType,
			Name:       f.ResourceName,
			Pattern:    f.ResourcePatternType,
			Operation:  f.Operation,
			Permission: f.PermissionType,
			Described:  ds,
			Err:        kerr.ErrorForCode(r.ErrorCode),
			ErrMessage: unptrStr(r.ErrorMessage),
		})
	}
	return rs, nil
}

var sliceAny = []string{"any"}

func createDelDescACL(b *ACLBuilder) ([]kmsg.DeleteACLsRequestFilter, []*kmsg.DescribeACLsRequest, error) {
	if err := b.ValidateFilter(); err != nil {
		return nil, nil, err
	}

	// As a special shortcut, if we have any allow and deny principals and
	// hosts, we collapse these into one "any" group. The anyAny and
	// anyAnyHosts vars are used in our looping below, and if we do this,
	// we dup and set all the relevant fields to false to not expand them
	// in our loops.
	var anyAny, anyAnyHosts bool
	if b.anyAllow && b.anyDeny && b.anyAllowHosts && b.anyDenyHosts {
		anyAny = true
		anyAnyHosts = true

		b = b.dup()
		b.allow = nil
		b.allowHosts = nil
		b.deny = nil
		b.denyHosts = nil
		b.anyAllow = false
		b.anyAllowHosts = false
		b.anyDeny = false
		b.anyDenyHosts = false
	}

	var clusters []string
	if b.anyCluster {
		clusters = []string{"kafka-cluster"}
	}
	var deletions []kmsg.DeleteACLsRequestFilter
	var describes []*kmsg.DescribeACLsRequest
	for _, typeNames := range []struct {
		t     kmsg.ACLResourceType
		names []string
		any   bool
	}{
		{kmsg.ACLResourceTypeAny, b.any, b.anyResource},
		{kmsg.ACLResourceTypeTopic, b.topics, b.anyTopic},
		{kmsg.ACLResourceTypeGroup, b.groups, b.anyGroup},
		{kmsg.ACLResourceTypeCluster, clusters, b.anyCluster},
		{kmsg.ACLResourceTypeTransactionalId, b.txnIDs, b.anyTxn},
		{kmsg.ACLResourceTypeDelegationToken, b.tokens, b.anyToken},
	} {
		if typeNames.any {
			typeNames.names = sliceAny
		}
		for _, name := range typeNames.names {
			for _, op := range b.ops {
				for _, perm := range []struct {
					principals   []string
					anyPrincipal bool
					hosts        []string
					anyHost      bool
					permType     kmsg.ACLPermissionType
				}{
					{
						b.allow,
						b.anyAllow,
						b.allowHosts,
						b.anyAllowHosts,
						kmsg.ACLPermissionTypeAllow,
					},
					{
						b.deny,
						b.anyDeny,
						b.denyHosts,
						b.anyDenyHosts,
						kmsg.ACLPermissionTypeDeny,
					},
					{
						nil,
						anyAny,
						nil,
						anyAnyHosts,
						kmsg.ACLPermissionTypeAny,
					},
				} {
					if perm.anyPrincipal {
						perm.principals = sliceAny
					}
					if perm.anyHost {
						perm.hosts = sliceAny
					}
					for _, principal := range perm.principals {
						for _, host := range perm.hosts {
							deletion := kmsg.NewDeleteACLsRequestFilter()
							describe := kmsg.NewPtrDescribeACLsRequest()

							deletion.ResourceType = typeNames.t
							describe.ResourceType = typeNames.t

							if !typeNames.any {
								deletion.ResourceName = kmsg.StringPtr(name)
								describe.ResourceName = kmsg.StringPtr(name)
							}

							deletion.ResourcePatternType = b.pattern
							describe.ResourcePatternType = b.pattern

							deletion.Operation = op
							describe.Operation = op

							if !perm.anyPrincipal {
								deletion.Principal = kmsg.StringPtr(principal)
								describe.Principal = kmsg.StringPtr(principal)
							}

							if !perm.anyHost {
								deletion.Host = kmsg.StringPtr(host)
								describe.Host = kmsg.StringPtr(host)
							}

							deletion.PermissionType = perm.permType
							describe.PermissionType = perm.permType

							deletions = append(deletions, deletion)
							describes = append(describes, describe)
						}
					}
				}
			}
		}
	}
	return deletions, describes, nil
}
