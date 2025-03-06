package physical

import "github.com/grafana/loki/v3/pkg/dataobj/planner/schema"

type DataObjScan struct {
	name    string
	sources []string
	schema  schema.Schema
}

func (*DataObjScan) ID() NodeType {
	return NodeTypeDataObjScan
}

func (s *DataObjScan) Children() []Node {
	return nil
}

func (s *DataObjScan) Schema() schema.Schema {
	return s.schema
}

func (*DataObjScan) isNode() {}

func (s *DataObjScan) Accept(v Visitor) (bool, error) {
	return v.VisitDataObjScan(s)
}
