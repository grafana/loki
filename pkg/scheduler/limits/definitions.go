package limits

// Limits needed for the Query Scheduler - interface used for decoupling.
type Limits interface {
	// MaxQueriersPerUser returns max queriers to use per tenant, or 0 if shuffle sharding is disabled.
	MaxQueriersPerUser(user string) int

	// MaxQueryCapacity returns how much of the available query capacity can be used by this user.
	MaxQueryCapacity(user string) float64
}
