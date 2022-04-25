package sarama

// ConsumerGroupMemberMetadata holds the metadata for consumer group
// https://github.com/apache/kafka/blob/trunk/clients/src/main/resources/common/message/ConsumerProtocolSubscription.json
type ConsumerGroupMemberMetadata struct {
	Version         int16
	Topics          []string
	UserData        []byte
	OwnedPartitions []*OwnedPartition
}

func (m *ConsumerGroupMemberMetadata) encode(pe packetEncoder) error {
	pe.putInt16(m.Version)

	if err := pe.putStringArray(m.Topics); err != nil {
		return err
	}

	if err := pe.putBytes(m.UserData); err != nil {
		return err
	}

	return nil
}

func (m *ConsumerGroupMemberMetadata) decode(pd packetDecoder) (err error) {
	if m.Version, err = pd.getInt16(); err != nil {
		return
	}

	if m.Topics, err = pd.getStringArray(); err != nil {
		return
	}

	if m.UserData, err = pd.getBytes(); err != nil {
		return
	}
	if m.Version >= 1 {
		n, err := pd.getArrayLength()
		if err != nil {
			// permit missing data here in case of misbehaving 3rd party
			// clients who incorrectly marked the member metadata as V1 in
			// their JoinGroup request
			if err == ErrInsufficientData {
				return nil
			}
			return err
		}
		if n == 0 {
			return nil
		}
		m.OwnedPartitions = make([]*OwnedPartition, n)
		for i := 0; i < n; i++ {
			m.OwnedPartitions[i] = &OwnedPartition{}
			if err := m.OwnedPartitions[i].decode(pd); err != nil {
				return err
			}
		}
	}

	return nil
}

type OwnedPartition struct {
	Topic      string
	Partitions []int32
}

func (m *OwnedPartition) decode(pd packetDecoder) (err error) {
	if m.Topic, err = pd.getString(); err != nil {
		return err
	}
	if m.Partitions, err = pd.getInt32Array(); err != nil {
		return err
	}

	return nil
}

// ConsumerGroupMemberAssignment holds the member assignment for a consume group
// https://github.com/apache/kafka/blob/trunk/clients/src/main/resources/common/message/ConsumerProtocolAssignment.json
type ConsumerGroupMemberAssignment struct {
	Version  int16
	Topics   map[string][]int32
	UserData []byte
}

func (m *ConsumerGroupMemberAssignment) encode(pe packetEncoder) error {
	pe.putInt16(m.Version)

	if err := pe.putArrayLength(len(m.Topics)); err != nil {
		return err
	}

	for topic, partitions := range m.Topics {
		if err := pe.putString(topic); err != nil {
			return err
		}
		if err := pe.putInt32Array(partitions); err != nil {
			return err
		}
	}

	if err := pe.putBytes(m.UserData); err != nil {
		return err
	}

	return nil
}

func (m *ConsumerGroupMemberAssignment) decode(pd packetDecoder) (err error) {
	if m.Version, err = pd.getInt16(); err != nil {
		return
	}

	var topicLen int
	if topicLen, err = pd.getArrayLength(); err != nil {
		return
	}

	m.Topics = make(map[string][]int32, topicLen)
	for i := 0; i < topicLen; i++ {
		var topic string
		if topic, err = pd.getString(); err != nil {
			return
		}
		if m.Topics[topic], err = pd.getInt32Array(); err != nil {
			return
		}
	}

	if m.UserData, err = pd.getBytes(); err != nil {
		return
	}

	return nil
}
