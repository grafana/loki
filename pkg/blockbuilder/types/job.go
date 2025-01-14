package types

import "fmt"

// Job represents a block building task.
type Job struct {
	id string
	// Partition and offset information
	partition int32
	offsets   Offsets
}

func (j *Job) ID() string {
	return j.id
}

func (j *Job) Partition() int32 {
	return j.partition
}

func (j *Job) Offsets() Offsets {
	return j.offsets
}

// JobStatus represents the current state of a job
type JobStatus int

const (
	JobStatusUnknown JobStatus = iota // zero value, largely unused
	JobStatusPending
	JobStatusInProgress
	JobStatusComplete
	JobStatusFailed  // Job failed and may be retried
	JobStatusExpired // Job failed too many times or is too old
)

func (s JobStatus) String() string {
	switch s {
	case JobStatusPending:
		return "pending"
	case JobStatusInProgress:
		return "in_progress"
	case JobStatusComplete:
		return "complete"
	case JobStatusFailed:
		return "failed"
	case JobStatusExpired:
		return "expired"
	default:
		return "unknown"
	}
}

func (s JobStatus) IsFinished() bool {
	return s == JobStatusComplete || s == JobStatusFailed || s == JobStatusExpired
}

// Offsets represents the range of offsets to process
// [Min,Max) exclusivity
type Offsets struct {
	Min int64
	Max int64
}

// NewJob creates a new job with the given partition and offsets
func NewJob(partition int32, offsets Offsets) *Job {
	return &Job{
		id:        GenerateJobID(partition, offsets),
		partition: partition,
		offsets:   offsets,
	}
}

// GenerateJobID creates a deterministic job ID from partition and offsets
func GenerateJobID(partition int32, offsets Offsets) string {
	return fmt.Sprintf("job-%d-%d-%d", partition, offsets.Min, offsets.Max)
}
