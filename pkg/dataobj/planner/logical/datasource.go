package logical

import "github.com/grafana/loki/v3/pkg/dataobj/planner/schema"

type DummyDatasource struct {
	name   string
	schema schema.Schema
}

func NewDummyDatasource(name string, schema schema.Schema) *DummyDatasource {
	return &DummyDatasource{
		name:   name,
		schema: schema,
	}
}

func (d *DummyDatasource) Schema() schema.Schema {
	return d.schema
}

func (d *DummyDatasource) Name() string {
	return d.name
}
