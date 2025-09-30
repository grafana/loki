package sarama

// DescribeLogDirsRequest is a describe request to get partitions' log size
type DescribeLogDirsRequest struct {
	// Version 0 and 1 are equal
	// The version number is bumped to indicate that on quota violation brokers send out responses before throttling.
	Version int16

	// If this is an empty array, all topics will be queried
	DescribeTopics []DescribeLogDirsRequestTopic
}

func (r *DescribeLogDirsRequest) setVersion(v int16) {
	r.Version = v
}

// DescribeLogDirsRequestTopic is a describe request about the log dir of one or more partitions within a Topic
type DescribeLogDirsRequestTopic struct {
	Topic        string
	PartitionIDs []int32
}

func (r *DescribeLogDirsRequest) encode(pe packetEncoder) error {
	isFlexible := r.Version >= 2

	length := len(r.DescribeTopics)
	if length == 0 {
		// In order to query all topics we must send null
		length = -1
	}
	if isFlexible {
		pe.putCompactArrayLength(length)
	} else {
		if err := pe.putArrayLength(length); err != nil {
			return err
		}
	}

	for _, d := range r.DescribeTopics {
		if isFlexible {
			if err := pe.putCompactString(d.Topic); err != nil {
				return err
			}

			if err := pe.putCompactInt32Array(d.PartitionIDs); err != nil {
				return err
			}
			pe.putEmptyTaggedFieldArray()
		} else {
			if err := pe.putString(d.Topic); err != nil {
				return err
			}

			if err := pe.putInt32Array(d.PartitionIDs); err != nil {
				return err
			}
		}
	}
	if isFlexible {
		pe.putEmptyTaggedFieldArray()
	}

	return nil
}

func (r *DescribeLogDirsRequest) decode(pd packetDecoder, version int16) error {
	isFlexible := r.Version >= 2

	var n int
	var err error
	if isFlexible {
		n, err = pd.getCompactArrayLength()
	} else {
		n, err = pd.getArrayLength()
	}
	if err != nil {
		return err
	}

	if n == -1 {
		n = 0
	}

	topics := make([]DescribeLogDirsRequestTopic, n)
	for i := 0; i < n; i++ {
		topics[i] = DescribeLogDirsRequestTopic{}

		var topic string
		if isFlexible {
			topic, err = pd.getCompactString()
		} else {
			topic, err = pd.getString()
		}
		if err != nil {
			return err
		}
		topics[i].Topic = topic

		var pIDs []int32
		if isFlexible {
			pIDs, err = pd.getCompactInt32Array()
		} else {
			pIDs, err = pd.getInt32Array()
		}
		if err != nil {
			return err
		}
		topics[i].PartitionIDs = pIDs
		if isFlexible {
			_, err = pd.getEmptyTaggedFieldArray()
			if err != nil {
				return err
			}
		}
	}
	r.DescribeTopics = topics

	if isFlexible {
		_, err = pd.getEmptyTaggedFieldArray()
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *DescribeLogDirsRequest) key() int16 {
	return apiKeyDescribeLogDirs
}

func (r *DescribeLogDirsRequest) version() int16 {
	return r.Version
}

func (r *DescribeLogDirsRequest) headerVersion() int16 {
	if r.Version >= 2 {
		return 2
	}
	return 1
}

func (r *DescribeLogDirsRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 4
}

func (r *DescribeLogDirsRequest) requiredVersion() KafkaVersion {
	switch r.Version {
	case 4:
		return V3_3_0_0
	case 3:
		return V3_2_0_0
	case 2:
		return V2_6_0_0
	case 1:
		return V2_0_0_0
	default:
		return V1_0_0_0
	}
}
