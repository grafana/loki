package log

type distinctFilter struct {
	datas  map[string]map[string]int
	labels []string
}

func NewDistinctFilter(labels []string) (Stage, error) {
	datas := make(map[string]map[string]int, 0)
	for _, label := range labels {
		datas[label] = make(map[string]int, 0)
	}
	return &distinctFilter{
		labels: labels,
		datas:  datas,
	}, nil
}
func (r *distinctFilter) Process(_ int64, line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	keep := false
	for _, label := range r.labels {
		val, ok := lbs.Get(label)
		if !ok {
			return line, true
		}
		_, ok = r.datas[label][val]
		if ok {
			return line, false
		}
		r.datas[label][val] = 1
		keep = true
	}
	return line, keep
}

func (r *distinctFilter) RequiredLabelNames() []string {
	return []string{}
}
