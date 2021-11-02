package sarama

import (
	"time"
)

type DeleteOffsetsResponse struct {
	//The top-level error code, or 0 if there was no error.
	ErrorCode    KError
	ThrottleTime time.Duration
	//The responses for each partition of the topics.
	Errors map[string]map[int32]KError
}

func (r *DeleteOffsetsResponse) AddError(topic string, partition int32, errorCode KError) {
	if r.Errors == nil {
		r.Errors = make(map[string]map[int32]KError)
	}
	partitions := r.Errors[topic]
	if partitions == nil {
		partitions = make(map[int32]KError)
		r.Errors[topic] = partitions
	}
	partitions[partition] = errorCode
}

func (r *DeleteOffsetsResponse) encode(pe packetEncoder) error {
	pe.putInt16(int16(r.ErrorCode))
	pe.putInt32(int32(r.ThrottleTime / time.Millisecond))

	if err := pe.putArrayLength(len(r.Errors)); err != nil {
		return err
	}
	for topic, partitions := range r.Errors {
		if err := pe.putString(topic); err != nil {
			return err
		}
		if err := pe.putArrayLength(len(partitions)); err != nil {
			return err
		}
		for partition, errorCode := range partitions {
			pe.putInt32(partition)
			pe.putInt16(int16(errorCode))
		}
	}
	return nil
}

func (r *DeleteOffsetsResponse) decode(pd packetDecoder, version int16) error {
	tmpErr, err := pd.getInt16()
	if err != nil {
		return err
	}
	r.ErrorCode = KError(tmpErr)

	throttleTime, err := pd.getInt32()
	if err != nil {
		return err
	}
	r.ThrottleTime = time.Duration(throttleTime) * time.Millisecond

	numTopics, err := pd.getArrayLength()
	if err != nil || numTopics == 0 {
		return err
	}

	r.Errors = make(map[string]map[int32]KError, numTopics)
	for i := 0; i < numTopics; i++ {
		name, err := pd.getString()
		if err != nil {
			return err
		}

		numErrors, err := pd.getArrayLength()
		if err != nil {
			return err
		}

		r.Errors[name] = make(map[int32]KError, numErrors)

		for j := 0; j < numErrors; j++ {
			id, err := pd.getInt32()
			if err != nil {
				return err
			}

			tmp, err := pd.getInt16()
			if err != nil {
				return err
			}
			r.Errors[name][id] = KError(tmp)
		}
	}

	return nil
}

func (r *DeleteOffsetsResponse) key() int16 {
	return 47
}

func (r *DeleteOffsetsResponse) version() int16 {
	return 0
}

func (r *DeleteOffsetsResponse) headerVersion() int16 {
	return 0
}

func (r *DeleteOffsetsResponse) requiredVersion() KafkaVersion {
	return V2_4_0_0
}
