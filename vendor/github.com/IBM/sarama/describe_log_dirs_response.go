package sarama

import "time"

type DescribeLogDirsResponse struct {
	ThrottleTime time.Duration

	// Version 0 and 1 are equal
	// The version number is bumped to indicate that on quota violation brokers send out responses before throttling.
	Version int16

	LogDirs []DescribeLogDirsResponseDirMetadata

	ErrorCode KError
}

func (r *DescribeLogDirsResponse) setVersion(v int16) {
	r.Version = v
}

func (r *DescribeLogDirsResponse) encode(pe packetEncoder) error {
	isFlexible := r.Version >= 2

	pe.putInt32(int32(r.ThrottleTime / time.Millisecond))

	if r.Version >= 3 {
		pe.putInt16(int16(r.ErrorCode))
	}

	if isFlexible {
		pe.putCompactArrayLength(len(r.LogDirs))
	} else {
		if err := pe.putArrayLength(len(r.LogDirs)); err != nil {
			return err
		}
	}

	for _, dir := range r.LogDirs {
		if err := dir.encode(pe, r.Version); err != nil {
			return err
		}
	}

	if isFlexible {
		pe.putEmptyTaggedFieldArray()
	}

	return nil
}

func (r *DescribeLogDirsResponse) decode(pd packetDecoder, version int16) error {
	isFlexible := version >= 2
	throttleTime, err := pd.getInt32()
	if err != nil {
		return err
	}
	r.ThrottleTime = time.Duration(throttleTime) * time.Millisecond

	if version >= 3 {
		errCode, err := pd.getInt16()
		if err != nil {
			return err
		}
		r.ErrorCode = KError(errCode)
	}

	// Decode array of DescribeLogDirsResponseDirMetadata
	var n int
	if isFlexible {
		n, err = pd.getCompactArrayLength()
	} else {
		n, err = pd.getArrayLength()
	}
	if err != nil {
		return err
	}

	r.LogDirs = make([]DescribeLogDirsResponseDirMetadata, n)
	for i := 0; i < n; i++ {
		dir := DescribeLogDirsResponseDirMetadata{}
		if err := dir.decode(pd, version); err != nil {
			return err
		}
		r.LogDirs[i] = dir
	}

	if isFlexible {
		if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
			return err
		}
	}

	return nil
}

func (r *DescribeLogDirsResponse) key() int16 {
	return apiKeyDescribeLogDirs
}

func (r *DescribeLogDirsResponse) version() int16 {
	return r.Version
}

func (r *DescribeLogDirsResponse) headerVersion() int16 {
	if r.Version >= 2 {
		return 1
	}
	return 0
}

func (r *DescribeLogDirsResponse) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 4
}

func (r *DescribeLogDirsResponse) requiredVersion() KafkaVersion {
	if r.Version > 0 {
		return V2_0_0_0
	}
	return V1_0_0_0
}

func (r *DescribeLogDirsResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}

type DescribeLogDirsResponseDirMetadata struct {
	ErrorCode KError

	// The absolute log directory path
	Path   string
	Topics []DescribeLogDirsResponseTopic

	TotalBytes  int64
	UsableBytes int64
}

func (r *DescribeLogDirsResponseDirMetadata) encode(pe packetEncoder, version int16) error {
	isFlexible := version >= 2

	pe.putInt16(int16(r.ErrorCode))

	var err error
	if isFlexible {
		err = pe.putCompactString(r.Path)
	} else {
		err = pe.putString(r.Path)
	}
	if err != nil {
		return err
	}

	if isFlexible {
		pe.putCompactArrayLength(len(r.Topics))
	} else {
		if err := pe.putArrayLength(len(r.Topics)); err != nil {
			return err
		}
	}
	for _, topic := range r.Topics {
		if err := topic.encode(pe, version); err != nil {
			return err
		}
	}

	if version >= 4 {
		pe.putInt64(r.TotalBytes)
		pe.putInt64(r.UsableBytes)
	}

	if isFlexible {
		pe.putEmptyTaggedFieldArray()
	}

	return nil
}

func (r *DescribeLogDirsResponseDirMetadata) decode(pd packetDecoder, version int16) error {
	isFlexible := version >= 2
	errCode, err := pd.getInt16()
	if err != nil {
		return err
	}
	r.ErrorCode = KError(errCode)

	var path string
	if isFlexible {
		path, err = pd.getCompactString()
	} else {
		path, err = pd.getString()
	}
	if err != nil {
		return err
	}
	r.Path = path

	// Decode array of DescribeLogDirsResponseTopic
	var n int
	if isFlexible {
		n, err = pd.getCompactArrayLength()
	} else {
		n, err = pd.getArrayLength()
	}
	if err != nil {
		return err
	}

	r.Topics = make([]DescribeLogDirsResponseTopic, n)
	for i := 0; i < n; i++ {
		t := DescribeLogDirsResponseTopic{}

		if err := t.decode(pd, version); err != nil {
			return err
		}

		r.Topics[i] = t
	}

	if version >= 4 {
		totalBytes, err := pd.getInt64()
		if err != nil {
			return err
		}
		r.TotalBytes = totalBytes
		usableBytes, err := pd.getInt64()
		if err != nil {
			return err
		}
		r.UsableBytes = usableBytes
	}

	if isFlexible {
		if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
			return err
		}
	}

	return nil
}

// DescribeLogDirsResponseTopic contains a topic's partitions descriptions
type DescribeLogDirsResponseTopic struct {
	Topic      string
	Partitions []DescribeLogDirsResponsePartition
}

func (r *DescribeLogDirsResponseTopic) encode(pe packetEncoder, version int16) error {
	isFlexible := version >= 2
	var err error
	if isFlexible {
		err = pe.putCompactString(r.Topic)
	} else {
		err = pe.putString(r.Topic)
	}
	if err != nil {
		return err
	}

	if isFlexible {
		pe.putCompactArrayLength(len(r.Partitions))
	} else {
		if err := pe.putArrayLength(len(r.Partitions)); err != nil {
			return err
		}
	}
	for _, partition := range r.Partitions {
		if err := partition.encode(pe, version); err != nil {
			return err
		}
	}

	if isFlexible {
		pe.putEmptyTaggedFieldArray()
	}

	return nil
}

func (r *DescribeLogDirsResponseTopic) decode(pd packetDecoder, version int16) error {
	isFlexible := version >= 2
	var t string
	var err error
	if isFlexible {
		t, err = pd.getCompactString()
	} else {
		t, err = pd.getString()
	}
	if err != nil {
		return err
	}
	r.Topic = t

	var n int
	if isFlexible {
		n, err = pd.getCompactArrayLength()
	} else {
		n, err = pd.getArrayLength()
	}
	if err != nil {
		return err
	}

	r.Partitions = make([]DescribeLogDirsResponsePartition, n)
	for i := 0; i < n; i++ {
		p := DescribeLogDirsResponsePartition{}
		if err := p.decode(pd, version); err != nil {
			return err
		}
		r.Partitions[i] = p
	}

	if isFlexible {
		if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
			return err
		}
	}

	return nil
}

// DescribeLogDirsResponsePartition describes a partition's log directory
type DescribeLogDirsResponsePartition struct {
	PartitionID int32

	// The size of the log segments of the partition in bytes.
	Size int64

	// The lag of the log's LEO w.r.t. partition's HW (if it is the current log for the partition) or
	// current replica's LEO (if it is the future log for the partition)
	OffsetLag int64

	// True if this log is created by AlterReplicaLogDirsRequest and will replace the current log of
	// the replica in the future.
	IsTemporary bool
}

func (r *DescribeLogDirsResponsePartition) encode(pe packetEncoder, version int16) error {
	isFlexible := version >= 2
	pe.putInt32(r.PartitionID)
	pe.putInt64(r.Size)
	pe.putInt64(r.OffsetLag)
	pe.putBool(r.IsTemporary)
	if isFlexible {
		pe.putEmptyTaggedFieldArray()
	}

	return nil
}

func (r *DescribeLogDirsResponsePartition) decode(pd packetDecoder, version int16) error {
	isFlexible := version >= 2
	pID, err := pd.getInt32()
	if err != nil {
		return err
	}
	r.PartitionID = pID

	size, err := pd.getInt64()
	if err != nil {
		return err
	}
	r.Size = size

	lag, err := pd.getInt64()
	if err != nil {
		return err
	}
	r.OffsetLag = lag

	isTemp, err := pd.getBool()
	if err != nil {
		return err
	}
	r.IsTemporary = isTemp

	if isFlexible {
		if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
			return err
		}
	}

	return nil
}
