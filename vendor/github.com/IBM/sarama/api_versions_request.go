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

func (r *ApiVersionsRequest) encode(pe packetEncoder) (err error) {
	if r.Version >= 3 {
		if err := pe.putCompactString(r.ClientSoftwareName); err != nil {
			return err
		}
		if err := pe.putCompactString(r.ClientSoftwareVersion); err != nil {
			return err
		}
		pe.putEmptyTaggedFieldArray()
	}

	return nil
}

func (r *ApiVersionsRequest) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version
	if r.Version >= 3 {
		if r.ClientSoftwareName, err = pd.getCompactString(); err != nil {
			return err
		}
		if r.ClientSoftwareVersion, err = pd.getCompactString(); err != nil {
			return err
		}
		if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
			return err
		}
	}

	return nil
}

func (r *ApiVersionsRequest) key() int16 {
	return 18
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
