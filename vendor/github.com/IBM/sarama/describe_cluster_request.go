package sarama

// DescribeClusterEndpointType enumerates the DescribeCluster endpoint types
// defined by the Kafka protocol.
const (
	DescribeClusterEndpointTypeBrokers     int8 = 1
	DescribeClusterEndpointTypeControllers int8 = 2
)

type DescribeClusterRequest struct {
	Version int16

	IncludeClusterAuthorizedOperations bool
	EndpointType                       int8
	IncludeFencedBrokers               bool
}

func NewDescribeClusterRequest(version KafkaVersion) *DescribeClusterRequest {
	req := &DescribeClusterRequest{}

	switch {
	case version.IsAtLeast(V4_0_0_0):
		req.Version = 2
	case version.IsAtLeast(V3_7_0_0):
		req.Version = 1
	default:
		req.Version = 0
	}

	if req.Version >= 1 {
		req.EndpointType = DescribeClusterEndpointTypeBrokers
	}
	if req.Version >= 2 {
		req.IncludeFencedBrokers = true
	}

	return req
}

func (r *DescribeClusterRequest) encode(pe packetEncoder) error {
	if r.Version < 0 || r.Version > 2 {
		return PacketEncodingError{"invalid or unsupported DescribeClusterRequest version"}
	}

	pe.putBool(r.IncludeClusterAuthorizedOperations)
	if r.Version >= 1 {
		pe.putInt8(r.EndpointType)
	}
	if r.Version >= 2 {
		pe.putBool(r.IncludeFencedBrokers)
	}
	pe.putEmptyTaggedFieldArray()

	return nil
}

func (r *DescribeClusterRequest) decode(pd packetDecoder, version int16) error {
	r.Version = version
	if r.Version < 0 || r.Version > 2 {
		return PacketDecodingError{"invalid or unsupported DescribeClusterRequest version"}
	}

	var err error
	if r.IncludeClusterAuthorizedOperations, err = pd.getBool(); err != nil {
		return err
	}
	if r.Version >= 1 {
		if r.EndpointType, err = pd.getInt8(); err != nil {
			return err
		}
	}
	if r.Version >= 2 {
		if r.IncludeFencedBrokers, err = pd.getBool(); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *DescribeClusterRequest) key() int16 { return apiKeyDescribeCluster }

func (r *DescribeClusterRequest) version() int16 { return r.Version }

func (r *DescribeClusterRequest) setVersion(v int16) { r.Version = v }

func (r *DescribeClusterRequest) headerVersion() int16 { return 2 }

func (r *DescribeClusterRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 2
}

func (r *DescribeClusterRequest) isFlexible() bool { return true }

func (r *DescribeClusterRequest) isFlexibleVersion(version int16) bool { return version >= 0 }

func (r *DescribeClusterRequest) requiredVersion() KafkaVersion {
	switch r.Version {
	case 2:
		return V4_0_0_0
	case 1:
		return V3_7_0_0
	default:
		return V2_8_0_0
	}
}
