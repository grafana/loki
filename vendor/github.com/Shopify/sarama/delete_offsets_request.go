package sarama

type DeleteOffsetsRequest struct {
	Group      string
	partitions map[string][]int32
}

func (r *DeleteOffsetsRequest) encode(pe packetEncoder) (err error) {
	err = pe.putString(r.Group)
	if err != nil {
		return err
	}

	if r.partitions == nil {
		pe.putInt32(0)
	} else {
		if err = pe.putArrayLength(len(r.partitions)); err != nil {
			return err
		}
	}
	for topic, partitions := range r.partitions {
		err = pe.putString(topic)
		if err != nil {
			return err
		}
		err = pe.putInt32Array(partitions)
		if err != nil {
			return err
		}
	}
	return
}

func (r *DeleteOffsetsRequest) decode(pd packetDecoder, version int16) (err error) {
	r.Group, err = pd.getString()
	if err != nil {
		return err
	}
	var partitionCount int

	partitionCount, err = pd.getArrayLength()
	if err != nil {
		return err
	}

	if (partitionCount == 0 && version < 2) || partitionCount < 0 {
		return nil
	}

	r.partitions = make(map[string][]int32, partitionCount)
	for i := 0; i < partitionCount; i++ {
		var topic string
		topic, err = pd.getString()
		if err != nil {
			return err
		}

		var partitions []int32
		partitions, err = pd.getInt32Array()
		if err != nil {
			return err
		}

		r.partitions[topic] = partitions
	}

	return nil
}

func (r *DeleteOffsetsRequest) key() int16 {
	return 47
}

func (r *DeleteOffsetsRequest) version() int16 {
	return 0
}

func (r *DeleteOffsetsRequest) headerVersion() int16 {
	return 1
}

func (r *DeleteOffsetsRequest) requiredVersion() KafkaVersion {
	return V2_4_0_0
}

func (r *DeleteOffsetsRequest) AddPartition(topic string, partitionID int32) {
	if r.partitions == nil {
		r.partitions = make(map[string][]int32)
	}

	r.partitions[topic] = append(r.partitions[topic], partitionID)
}
