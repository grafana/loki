package sarama

// ConsumerGroupMetadata identifies the consumer group, and optionally the
// group member, on whose behalf offsets are committed within a transaction.
//
// When a group member supplies its MemberID, GenerationID and (for static
// membership) GroupInstanceID, the broker can fence zombie or stale members
// while committing transactional offsets (KIP-447). The zero value, or a
// metadata built with NewConsumerGroupMetadata, leaves GenerationID at
// GroupGenerationUndefined which tells the broker to skip that fencing.
type ConsumerGroupMetadata struct {
	GroupID         string
	GenerationID    int32
	MemberID        string
	GroupInstanceID *string
}

// NewConsumerGroupMetadata returns metadata carrying only the group ID, with
// GenerationID set to GroupGenerationUndefined so the broker performs no member
// fencing. This reproduces the behavior of the group-ID-only transactional
// offset commit APIs.
func NewConsumerGroupMetadata(groupID string) *ConsumerGroupMetadata {
	return &ConsumerGroupMetadata{
		GroupID:      groupID,
		GenerationID: GroupGenerationUndefined,
	}
}

// NewConsumerGroupMetadataFromSession builds metadata from a live consumer
// group session, copying the member ID and generation ID so the broker can
// fence stale members. Pass the group instance ID when using static membership
// (Config.Consumer.Group.InstanceId), otherwise nil.
func NewConsumerGroupMetadataFromSession(session ConsumerGroupSession, groupID string, groupInstanceID *string) *ConsumerGroupMetadata {
	return &ConsumerGroupMetadata{
		GroupID:         groupID,
		GenerationID:    session.GenerationID(),
		MemberID:        session.MemberID(),
		GroupInstanceID: groupInstanceID,
	}
}
