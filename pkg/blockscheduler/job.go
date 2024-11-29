package blockscheduler

type Job struct {
	ID          string
	Partition   int32
	StartOffset int64 // inclusive
	EndOffset   int64 // exclusive
}
