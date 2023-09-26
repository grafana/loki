package logql

type MapperStats struct {
	splitQueries int
}

func NewMapperStats() *MapperStats {
	return &MapperStats{}
}

// AddSplitQueries add num split queries to the counter
func (s *MapperStats) AddSplitQueries(num int) {
	s.splitQueries += num
}

// GetSplitQueries returns the number of split queries
func (s *MapperStats) GetSplitQueries() int {
	return s.splitQueries
}

// resetSplitQueries resets the number of split queries
func (s *MapperStats) resetSplitQueries() {
	s.splitQueries = 0
}
