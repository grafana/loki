package grpc

// Humanize returns a more human-friendly string form of job types.
func (x JobType) Humanize() string {
	switch x {
	case JOB_TYPE_DELETION:
		return "deletion"
	default:
		return x.String()
	}
}
