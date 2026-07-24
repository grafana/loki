package sarama

import "time"

type DeleteTopicsRequest struct {
	Version int16
	Topics  []string
	Timeout time.Duration
}

func (d *DeleteTopicsRequest) setVersion(v int16) {
	d.Version = v
}

func NewDeleteTopicsRequest(version KafkaVersion, topics []string, timeout time.Duration) *DeleteTopicsRequest {
	d := &DeleteTopicsRequest{
		Topics:  topics,
		Timeout: timeout,
	}
	// Versions 0, 1, 2, and 3 are the same.
	if version.IsAtLeast(V2_8_0_0) {
		// Version 6 reorganizes topics, adds topic IDs and allows topic names to be null (KIP-516)
		d.Version = 6
	} else if version.IsAtLeast(V2_7_0_0) {
		// version 5 may return THROTTLING_QUOTA_EXCEEDED
		d.Version = 5
	} else if version.IsAtLeast(V2_4_0_0) {
		// Version 4 is first flexible version.
		d.Version = 4
	} else if version.IsAtLeast(V2_1_0_0) {
		d.Version = 3
	} else if version.IsAtLeast(V2_0_0_0) {
		d.Version = 2
	} else if version.IsAtLeast(V0_11_0_0) {
		d.Version = 1
	}
	return d
}

func (d *DeleteTopicsRequest) encode(pe packetEncoder) error {
	if d.Version >= 6 {
		if err := pe.putArrayLength(len(d.Topics)); err != nil {
			return err
		}
		for _, topic := range d.Topics {
			// deletion by topic ID is not supported, so send the name with a null topic ID
			if err := pe.putNullableString(&topic); err != nil {
				return err
			}
			if err := pe.putUuid(Uuid{}); err != nil {
				return err
			}
			pe.putEmptyTaggedFieldArray()
		}
	} else {
		if err := pe.putStringArray(d.Topics); err != nil {
			return err
		}
	}
	pe.putInt32(int32(d.Timeout / time.Millisecond))
	pe.putEmptyTaggedFieldArray()
	return nil
}

func (d *DeleteTopicsRequest) decode(pd packetDecoder, version int16) (err error) {
	d.Version = version
	if version >= 6 {
		n, err := pd.getArrayLength()
		if err != nil {
			return err
		}
		if n < 0 {
			return errInvalidArrayLength
		}
		for range n {
			name, err := pd.getNullableString()
			if err != nil {
				return err
			}
			if _, err := pd.getUuid(); err != nil {
				return err
			}
			if name != nil {
				d.Topics = append(d.Topics, *name)
			}
			if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
				return err
			}
		}
	} else {
		if d.Topics, err = pd.getStringArray(); err != nil {
			return err
		}
	}
	timeout, err := pd.getInt32()
	if err != nil {
		return err
	}
	d.Timeout = time.Duration(timeout) * time.Millisecond

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (d *DeleteTopicsRequest) key() int16 {
	return apiKeyDeleteTopics
}

func (d *DeleteTopicsRequest) version() int16 {
	return d.Version
}

func (d *DeleteTopicsRequest) headerVersion() int16 {
	if d.Version >= 4 {
		return 2
	}
	return 1
}

func (d *DeleteTopicsRequest) isFlexible() bool {
	return d.isFlexibleVersion(d.Version)
}

func (d *DeleteTopicsRequest) isFlexibleVersion(version int16) bool {
	return version >= 4
}

func (d *DeleteTopicsRequest) isValidVersion() bool {
	return d.Version >= 0 && d.Version <= 6
}

func (d *DeleteTopicsRequest) requiredVersion() KafkaVersion {
	switch d.Version {
	case 6:
		return V2_8_0_0
	case 5:
		return V2_7_0_0
	case 4:
		return V2_4_0_0
	case 3:
		return V2_1_0_0
	case 2:
		return V2_0_0_0
	case 1:
		return V0_11_0_0
	case 0:
		return V0_10_1_0
	default:
		return V2_2_0_0
	}
}
