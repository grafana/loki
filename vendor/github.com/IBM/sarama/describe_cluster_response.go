package sarama

import "time"

type DescribeClusterResponse struct {
	Version int16

	ThrottleTimeMs int32
	Err            KError
	ErrorMessage   *string
	EndpointType   int8
	ClusterID      string
	ControllerID   int32
	Brokers        []*DescribeClusterBroker

	ClusterAuthorizedOperations int32
}

type DescribeClusterBroker struct {
	BrokerID int32
	Host     string
	Port     int32
	Rack     *string
	IsFenced bool
}

func (r *DescribeClusterResponse) encode(pe packetEncoder) error {
	if r.Version < 0 || r.Version > 2 {
		return PacketEncodingError{"invalid or unsupported DescribeClusterResponse version"}
	}

	pe.putInt32(r.ThrottleTimeMs)
	pe.putInt16(int16(r.Err))
	if err := pe.putNullableString(r.ErrorMessage); err != nil {
		return err
	}
	if r.Version >= 1 {
		pe.putInt8(r.EndpointType)
	}
	if err := pe.putString(r.ClusterID); err != nil {
		return err
	}
	pe.putInt32(r.ControllerID)

	if err := pe.putArrayLength(len(r.Brokers)); err != nil {
		return err
	}
	for _, broker := range r.Brokers {
		if err := broker.encode(pe, r.Version); err != nil {
			return err
		}
	}

	pe.putInt32(r.ClusterAuthorizedOperations)
	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *DescribeClusterResponse) decode(pd packetDecoder, version int16) error {
	r.Version = version
	if r.Version < 0 || r.Version > 2 {
		return PacketDecodingError{"invalid or unsupported DescribeClusterResponse version"}
	}

	var err error
	if r.ThrottleTimeMs, err = pd.getInt32(); err != nil {
		return err
	}
	if r.Err, err = pd.getKError(); err != nil {
		return err
	}
	if r.ErrorMessage, err = pd.getNullableString(); err != nil {
		return err
	}
	if r.Version >= 1 {
		if r.EndpointType, err = pd.getInt8(); err != nil {
			return err
		}
	}
	if r.ClusterID, err = pd.getString(); err != nil {
		return err
	}
	if r.ControllerID, err = pd.getInt32(); err != nil {
		return err
	}

	brokerCount, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	r.Brokers = make([]*DescribeClusterBroker, brokerCount)
	for i := 0; i < brokerCount; i++ {
		broker := &DescribeClusterBroker{}
		if err := broker.decode(pd, r.Version); err != nil {
			return err
		}
		r.Brokers[i] = broker
	}

	if r.ClusterAuthorizedOperations, err = pd.getInt32(); err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *DescribeClusterResponse) key() int16 { return apiKeyDescribeCluster }

func (r *DescribeClusterResponse) version() int16 { return r.Version }

func (r *DescribeClusterResponse) setVersion(v int16) { r.Version = v }

func (r *DescribeClusterResponse) headerVersion() int16 { return 1 }

func (r *DescribeClusterResponse) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 2
}

func (r *DescribeClusterResponse) isFlexible() bool { return true }

func (r *DescribeClusterResponse) isFlexibleVersion(version int16) bool { return version >= 0 }

func (r *DescribeClusterResponse) requiredVersion() KafkaVersion {
	switch r.Version {
	case 2:
		return V4_0_0_0
	case 1:
		return V3_7_0_0
	default:
		return V2_8_0_0
	}
}

func (r *DescribeClusterResponse) throttleTime() time.Duration {
	return time.Duration(r.ThrottleTimeMs) * time.Millisecond
}

func (b *DescribeClusterBroker) encode(pe packetEncoder, version int16) error {
	pe.putInt32(b.BrokerID)
	if err := pe.putString(b.Host); err != nil {
		return err
	}
	pe.putInt32(b.Port)
	if err := pe.putNullableString(b.Rack); err != nil {
		return err
	}
	if version >= 2 {
		pe.putBool(b.IsFenced)
	}
	pe.putEmptyTaggedFieldArray()
	return nil
}

func (b *DescribeClusterBroker) decode(pd packetDecoder, version int16) error {
	var err error
	if b.BrokerID, err = pd.getInt32(); err != nil {
		return err
	}
	if b.Host, err = pd.getString(); err != nil {
		return err
	}
	if b.Port, err = pd.getInt32(); err != nil {
		return err
	}
	if b.Rack, err = pd.getNullableString(); err != nil {
		return err
	}
	if version >= 2 {
		if b.IsFenced, err = pd.getBool(); err != nil {
			return err
		}
	}
	_, err = pd.getEmptyTaggedFieldArray()
	return err
}
