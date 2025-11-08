package sarama

const defaultClientSoftwareName = "sarama"

type ApiVersionsRequest struct {
	// Version defines the protocol version to use for encode and decode
	Version int16
	// ClientSoftwareName contains the name of the client.
	ClientSoftwareName string
	// ClientSoftwareVersion contains the version of the client.
	ClientSoftwareVersion string
}

func (r *ApiVersionsRequest) setVersion(v int16) {
	r.Version = v
}

func (r *ApiVersionsRequest) encode(pe packetEncoder) (err error) {
	if r.Version >= 3 {
		if err := pe.putString(r.ClientSoftwareName); err != nil {
			return err
		}
		if err := pe.putString(r.ClientSoftwareVersion); err != nil {
			return err
		}
		pe.putEmptyTaggedFieldArray()
	}

	return nil
}

func (r *ApiVersionsRequest) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version
	if r.Version >= 3 {
		if r.ClientSoftwareName, err = pd.getString(); err != nil {
			return err
		}
		if r.ClientSoftwareVersion, err = pd.getString(); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *ApiVersionsRequest) key() int16 {
	return apiKeyApiVersions
}

func (r *ApiVersionsRequest) version() int16 {
	return r.Version
}

func (r *ApiVersionsRequest) headerVersion() int16 {
	if r.Version >= 3 {
		return 2
	}
	return 1
}

func (r *ApiVersionsRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 3
}

func (r *ApiVersionsRequest) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *ApiVersionsRequest) isFlexibleVersion(version int16) bool {
	return version >= 3
}

func (r *ApiVersionsRequest) requiredVersion() KafkaVersion {
	switch r.Version {
	case 3:
		return V2_4_0_0
	case 2:
		return V2_0_0_0
	case 1:
		return V0_11_0_0
	case 0:
		return V0_10_0_0
	default:
		return V2_4_0_0
	}
}
