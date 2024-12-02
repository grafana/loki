package blockbuilder

// Job represents a unit of work with a specific Kafka partition and offset range.
type Job struct {
	ID        string
	Partition int32
	Offsets   Offsets
	Status    JobStatus
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
