package sarama

type SaslAuthenticateRequest struct {
	// Version defines the protocol version to use for encode and decode
	Version       int16
	SaslAuthBytes []byte
}

func (r *SaslAuthenticateRequest) setVersion(v int16) {
	r.Version = v
}

func (r *SaslAuthenticateRequest) encode(pe packetEncoder) error {
	if err := pe.putBytes(r.SaslAuthBytes); err != nil {
		return err
	}
	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *SaslAuthenticateRequest) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version
	if r.SaslAuthBytes, err = pd.getBytes(); err != nil {
		return err
	}
	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *SaslAuthenticateRequest) key() int16 {
	return apiKeySASLAuth
}

func (r *SaslAuthenticateRequest) version() int16 {
	return r.Version
}

func (r *SaslAuthenticateRequest) headerVersion() int16 {
	if r.Version >= 2 {
		return 2
	}
	return 1
}

func (r *SaslAuthenticateRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 2
}

func (r *SaslAuthenticateRequest) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *SaslAuthenticateRequest) isFlexibleVersion(version int16) bool {
	return version >= 2
}

func (r *SaslAuthenticateRequest) requiredVersion() KafkaVersion {
	switch r.Version {
	case 2:
		return V2_5_0_0
	case 1:
		return V2_2_0_0
	default:
		return V1_0_0_0
	}
}
