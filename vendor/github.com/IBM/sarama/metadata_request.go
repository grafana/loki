package sarama

import "encoding/base64"

type Uuid [16]byte

func (u Uuid) String() string {
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(u[:])
}

var NullUUID = []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

type MetadataRequest struct {
	// Version defines the protocol version to use for encode and decode
	Version int16
	// Topics contains the topics to fetch metadata for.
	Topics []string
	// AllowAutoTopicCreation contains a If this is true, the broker may auto-create topics that we requested which do not already exist, if it is configured to do so.
	AllowAutoTopicCreation             bool
	IncludeClusterAuthorizedOperations bool // version 8 and up
	IncludeTopicAuthorizedOperations   bool // version 8 and up
}

func NewMetadataRequest(version KafkaVersion, topics []string) *MetadataRequest {
	m := &MetadataRequest{Topics: topics}
	if version.IsAtLeast(V2_8_0_0) {
		m.Version = 10
	} else if version.IsAtLeast(V2_4_0_0) {
		m.Version = 9
	} else if version.IsAtLeast(V2_4_0_0) {
		m.Version = 8
	} else if version.IsAtLeast(V2_1_0_0) {
		m.Version = 7
	} else if version.IsAtLeast(V2_0_0_0) {
		m.Version = 6
	} else if version.IsAtLeast(V1_0_0_0) {
		m.Version = 5
	} else if version.IsAtLeast(V0_11_0_0) {
		m.Version = 4
	} else if version.IsAtLeast(V0_10_1_0) {
		m.Version = 2
	} else if version.IsAtLeast(V0_10_0_0) {
		m.Version = 1
	}
	return m
}

func (r *MetadataRequest) encode(pe packetEncoder) (err error) {
	if r.Version < 0 || r.Version > 10 {
		return PacketEncodingError{"invalid or unsupported MetadataRequest version field"}
	}
	if r.Version == 0 || len(r.Topics) > 0 {
		if r.Version < 9 {
			err := pe.putArrayLength(len(r.Topics))
			if err != nil {
				return err
			}

			for i := range r.Topics {
				err = pe.putString(r.Topics[i])
				if err != nil {
					return err
				}
			}
		} else if r.Version == 9 {
			pe.putCompactArrayLength(len(r.Topics))
			for _, topicName := range r.Topics {
				if err := pe.putCompactString(topicName); err != nil {
					return err
				}
				pe.putEmptyTaggedFieldArray()
			}
		} else { // r.Version = 10
			pe.putCompactArrayLength(len(r.Topics))
			for _, topicName := range r.Topics {
				if err := pe.putRawBytes(NullUUID); err != nil {
					return err
				}
				// Avoid implicit memory aliasing in for loop
				tn := topicName
				if err := pe.putNullableCompactString(&tn); err != nil {
					return err
				}
				pe.putEmptyTaggedFieldArray()
			}
		}
	} else {
		if r.Version < 9 {
			pe.putInt32(-1)
		} else {
			pe.putCompactArrayLength(-1)
		}
	}

	if r.Version > 3 {
		pe.putBool(r.AllowAutoTopicCreation)
	}
	if r.Version > 7 {
		pe.putBool(r.IncludeClusterAuthorizedOperations)
		pe.putBool(r.IncludeTopicAuthorizedOperations)
	}
	if r.Version > 8 {
		pe.putEmptyTaggedFieldArray()
	}
	return nil
}

func (r *MetadataRequest) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version
	if r.Version < 9 {
		size, err := pd.getInt32()
		if err != nil {
			return err
		}
		if size > 0 {
			r.Topics = make([]string, size)
			for i := range r.Topics {
				topic, err := pd.getString()
				if err != nil {
					return err
				}
				r.Topics[i] = topic
			}
		}
	} else if r.Version == 9 {
		size, err := pd.getCompactArrayLength()
		if err != nil {
			return err
		}
		if size > 0 {
			r.Topics = make([]string, size)
		}
		for i := range r.Topics {
			topic, err := pd.getCompactString()
			if err != nil {
				return err
			}
			r.Topics[i] = topic
			if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
				return err
			}
		}
	} else { // version 10+
		size, err := pd.getCompactArrayLength()
		if err != nil {
			return err
		}

		if size > 0 {
			r.Topics = make([]string, size)
		}
		for i := range r.Topics {
			if _, err = pd.getRawBytes(16); err != nil { // skip UUID
				return err
			}
			topic, err := pd.getCompactNullableString()
			if err != nil {
				return err
			}
			if topic != nil {
				r.Topics[i] = *topic
			}

			if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
				return err
			}
		}
	}

	if r.Version >= 4 {
		if r.AllowAutoTopicCreation, err = pd.getBool(); err != nil {
			return err
		}
	}

	if r.Version > 7 {
		includeClusterAuthz, err := pd.getBool()
		if err != nil {
			return err
		}
		r.IncludeClusterAuthorizedOperations = includeClusterAuthz
		includeTopicAuthz, err := pd.getBool()
		if err != nil {
			return err
		}
		r.IncludeTopicAuthorizedOperations = includeTopicAuthz
	}
	if r.Version > 8 {
		if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
			return err
		}
	}
	return nil
}

func (r *MetadataRequest) key() int16 {
	return 3
}

func (r *MetadataRequest) version() int16 {
	return r.Version
}

func (r *MetadataRequest) headerVersion() int16 {
	if r.Version >= 9 {
		return 2
	}
	return 1
}

func (r *MetadataRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 10
}

func (r *MetadataRequest) requiredVersion() KafkaVersion {
	switch r.Version {
	case 10:
		return V2_8_0_0
	case 9:
		return V2_4_0_0
	case 8:
		return V2_3_0_0
	case 7:
		return V2_1_0_0
	case 6:
		return V2_0_0_0
	case 5:
		return V1_0_0_0
	case 3, 4:
		return V0_11_0_0
	case 2:
		return V0_10_1_0
	case 1:
		return V0_10_0_0
	case 0:
		return V0_8_2_0
	default:
		return V2_8_0_0
	}
}
