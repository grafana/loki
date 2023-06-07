package writefailures

type strategy struct {
	burst int
	rate  float64
}

func newStrategy(burst int, rate float64) *strategy {
	return &strategy{
		burst: burst,
		rate:  rate,
	}
}

func (s *strategy) Burst(tenantID string) int {
	return s.burst
}

func (s *strategy) Limit(tenantID string) float64 {
	return s.rate
}
