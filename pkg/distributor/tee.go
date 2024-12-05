package distributor

import "github.com/grafana/loki/v3/pkg/distributor/model"

// Tee implementations can duplicate the log streams to another endpoint.
type Tee interface {
	Duplicate(tenant string, streams []model.KeyedStream)
}

// WrapTee wraps a new Tee around an existing Tee.
func WrapTee(existing, new Tee) Tee {
	if existing == nil {
		return new
	}
	if multi, ok := existing.(*multiTee); ok {
		return &multiTee{append(multi.tees, new)}
	}
	return &multiTee{tees: []Tee{existing, new}}
}

type multiTee struct {
	tees []Tee
}

func (m *multiTee) Duplicate(tenant string, streams []model.KeyedStream) {
	for _, tee := range m.tees {
		tee.Duplicate(tenant, streams)
	}
}
