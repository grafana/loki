package queryrangebase

import (
	"github.com/gogo/protobuf/proto"
)

// Register the wiresmith-generated message types in the gogo proto registry
// under their original gogo fully-qualified names, so the results cache's
// gogo types.Any (Marshal/UnmarshalAny) can resolve them by TypeUrl
// (WIRESMITH_MIGRATION.md, N4).
func init() {
	proto.RegisterType((*PrometheusRequest)(nil), "queryrangebase.PrometheusRequest")
	proto.RegisterType((*PrometheusResponse)(nil), "queryrangebase.PrometheusResponse")
	proto.RegisterType((*PrometheusData)(nil), "queryrangebase.PrometheusData")
	proto.RegisterType((*SampleStream)(nil), "queryrangebase.SampleStream")
}
