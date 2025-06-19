package sarama

type SaslAuthenticateRequest struct {
	// Version defines the protocol version to use for encode and decode
	Version       int16
	SaslAuthBytes []byte
}

// APIKeySASLAuth is the API key for the SaslAuthenticate Kafka API
const APIKeySASLAuth = 36

func (r *SaslAuthenticateRequest) encode(pe packetEncoder) error {
	return pe.putBytes(r.SaslAuthBytes)
}

func (r *SaslAuthenticateRequest) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version
	r.SaslAuthBytes, err = pd.getBytes()
	return err
}

func (r *SaslAuthenticateRequest) key() int16 {
	return APIKeySASLAuth
}

func (r *SaslAuthenticateRequest) version() int16 {
	return r.Version
}

func (r *SaslAuthenticateRequest) headerVersion() int16 {
	return 1
}

func (r *SaslAuthenticateRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 1
}

func (r *SaslAuthenticateRequest) requiredVersion() KafkaVersion {
	switch r.Version {
	case 1:
		return V2_2_0_0
	default:
		return V1_0_0_0
	}
}
