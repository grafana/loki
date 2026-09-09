package kafka

import "strconv"

// PartitionID is a Kafka partition identifier.
type PartitionID int32

// String returns the decimal representation of the partition id.
func (p PartitionID) String() string {
	return strconv.Itoa(int(p))
}

// Offset is a Kafka record offset.
type Offset int64

// String returns the decimal representation of the offset.
func (o Offset) String() string {
	return strconv.FormatInt(int64(o), 10)
}
