package blockbuilder

import "fmt"

// Job represents a unit of work with a specific Kafka partition and offset range.
type Job struct {
	ID        string
	Partition int32
	Offsets   Offsets
	Status    JobStatus
}

// NewJob creates a new Job with a deterministic ID based on partition and offsets
func NewJob(partition int32, offsets Offsets) *Job {
	return &Job{
		ID:        GenerateJobID(partition, offsets),
		Partition: partition,
		Offsets:   offsets,
		Status:    JobStatusPending,
	}
}

// GenerateJobID creates a deterministic ID from partition and offsets
func GenerateJobID(partition int32, offsets Offsets) string {
	return fmt.Sprintf("job-%d-%d-%d", partition, offsets.Min, offsets.Max)
}

// JobStatus represents the current state of a job
type JobStatus int

const (
	JobStatusPending JobStatus = iota
	JobStatusInProgress
	JobStatusComplete
	JobStatusFailed
)

// Offsets defines a range of offsets with Min and Max values.
// [min,max)
type Offsets struct {
	Min, Max int64
}
