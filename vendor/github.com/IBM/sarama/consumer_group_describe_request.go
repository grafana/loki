package sarama

// ConsumerGroupDescribeRequest describes consumer groups using the KIP-848 protocol.
type ConsumerGroupDescribeRequest struct {
	Version                     int16
	GroupIDs                    []string
	IncludeAuthorizedOperations bool
}

func NewConsumerGroupDescribeRequest(version KafkaVersion) *ConsumerGroupDescribeRequest {
	req := &ConsumerGroupDescribeRequest{}

	switch {
	case version.IsAtLeast(V4_0_0_0):
		req.Version = 1
	default:
		req.Version = 0
	}

	return req
}

func (r *ConsumerGroupDescribeRequest) encode(pe packetEncoder) error {
	if !r.isValidVersion() {
		return PacketEncodingError{"invalid or unsupported ConsumerGroupDescribeRequest version"}
	}

	if err := pe.putStringArray(r.GroupIDs); err != nil {
		return err
	}

	pe.putBool(r.IncludeAuthorizedOperations)

	pe.putEmptyTaggedFieldArray()

	return nil
}

func (r *ConsumerGroupDescribeRequest) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version

	if !r.isValidVersion() {
		return PacketDecodingError{"invalid or unsupported ConsumerGroupDescribeRequest version"}
	}

	if r.GroupIDs, err = pd.getStringArray(); err != nil {
		return err
	}

	if r.IncludeAuthorizedOperations, err = pd.getBool(); err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()

	return err
}

func (r *ConsumerGroupDescribeRequest) key() int16 { return apiKeyConsumerGroupDescribe }

func (r *ConsumerGroupDescribeRequest) version() int16 { return r.Version }

func (r *ConsumerGroupDescribeRequest) setVersion(v int16) { r.Version = v }

func (r *ConsumerGroupDescribeRequest) headerVersion() int16 { return 2 }

func (r *ConsumerGroupDescribeRequest) isValidVersion() bool { return r.Version >= 0 && r.Version <= 1 }

func (r *ConsumerGroupDescribeRequest) isFlexible() bool { return true }

func (r *ConsumerGroupDescribeRequest) isFlexibleVersion(version int16) bool { return version >= 0 }

func (r *ConsumerGroupDescribeRequest) requiredVersion() KafkaVersion {
	switch r.Version {
	case 1:
		return V4_0_0_0
	default:
		return V3_7_0_0
	}
}
