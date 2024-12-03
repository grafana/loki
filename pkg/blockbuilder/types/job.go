package types

import "fmt"

// Job represents a block building task.
type Job struct {
	ID string
	// Partition and offset information
	Partition int
	Offsets   Offsets
}

// JobStatus represents the current state of a job
type JobStatus int

const (
	JobStatusPending JobStatus = iota
	JobStatusInProgress
	JobStatusComplete
)

// Offsets represents the range of offsets to process
type Offsets struct {
	Min int64
	Max int64
}

// NewJob creates a new job with the given partition and offsets
func NewJob(partition int, offsets Offsets) *Job {
	return &Job{
		ID:        GenerateJobID(partition, offsets),
		Partition: partition,
		Offsets:   offsets,
	}
}

// GenerateJobID creates a deterministic job ID from partition and offsets
func GenerateJobID(partition int, offsets Offsets) string {
	return fmt.Sprintf("job-%d-%d-%d", partition, offsets.Min, offsets.Max)
}
