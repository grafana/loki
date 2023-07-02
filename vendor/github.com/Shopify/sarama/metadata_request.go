package sarama

type MetadataRequest struct {
	// Version defines the protocol version to use for encode and decode
	Version int16
	// Topics contains the topics to fetch metadata for.
	Topics []string
	// AllowAutoTopicCreation contains a If this is true, the broker may auto-create topics that we requested which do not already exist, if it is configured to do so.
	AllowAutoTopicCreation bool
}

func NewMetadataRequest(version KafkaVersion, topics []string) *MetadataRequest {
	m := &MetadataRequest{Topics: topics}
	if version.IsAtLeast(V2_1_0_0) {
		m.Version = 7
	} else if version.IsAtLeast(V2_0_0_0) {
		m.Version = 6
	} else if version.IsAtLeast(V1_0_0_0) {
		m.Version = 5
	} else if version.IsAtLeast(V0_10_0_0) {
		m.Version = 1
	}
	return m
}

func (r *MetadataRequest) encode(pe packetEncoder) (err error) {
	if r.Version < 0 || r.Version > 12 {
		return PacketEncodingError{"invalid or unsupported MetadataRequest version field"}
	}
	if r.Version == 0 || len(r.Topics) > 0 {
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
	} else {
		pe.putInt32(-1)
	}

	if r.Version >= 4 {
		pe.putBool(r.AllowAutoTopicCreation)
	}

	return nil
}

func (r *MetadataRequest) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version
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

	if r.Version >= 4 {
		if r.AllowAutoTopicCreation, err = pd.getBool(); err != nil {
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
	return 1
}

func (r *MetadataRequest) requiredVersion() KafkaVersion {
	switch r.Version {
	case 1:
		return V0_10_0_0
	case 2:
		return V0_10_1_0
	case 3, 4:
		return V0_11_0_0
	case 5:
		return V1_0_0_0
	case 6:
		return V2_0_0_0
	case 7:
		return V2_1_0_0
	default:
		return MinVersion
	}
}
