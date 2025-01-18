package sarama

type ElectionType int8

const (
	// PreferredElection constant type
	PreferredElection ElectionType = 0
	// UncleanElection constant type
	UncleanElection ElectionType = 1
)
