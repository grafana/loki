package distributor

// Limits controls the max line size
type Limits interface {
	MaxLineSize(userID string) int
}

type constLimits int

func (c constLimits) MaxLineSize(userID string) int {
	return int(c)
}

// PriorityLimits returns the first non-zero result from a set of []Limits
type PriorityLimits []Limits

func (ls PriorityLimits) MaxLineSize(userID string) (res int) {
	for _, l := range ls {
		if res = l.MaxLineSize(userID); res != 0 {
			return res
		}
	}
	return res
}
