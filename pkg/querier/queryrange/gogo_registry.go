package queryrange

import (
	"github.com/gogo/protobuf/proto"
)

// Register the wiresmith-generated message types in the gogo proto registry
// under their original gogo fully-qualified names. The results cache stores
// responses inside a gogo types.Any (see pkg/storage/chunk/cache/resultscache),
// whose Marshal/UnmarshalAny resolve the concrete type via proto.MessageName /
// proto.MessageType. wiresmith does not register types with the gogo registry,
// so without this the TypeUrl path fails with `message type "" isn't linked in`
// (WIRESMITH_MIGRATION.md, N4). The names must match the gogoslick output the
// cache may have persisted.
func init() {
	proto.RegisterType((*LokiRequest)(nil), "queryrange.LokiRequest")
	proto.RegisterType((*LokiInstantRequest)(nil), "queryrange.LokiInstantRequest")
	proto.RegisterType((*Plan)(nil), "queryrange.Plan")
	proto.RegisterType((*LokiResponse)(nil), "queryrange.LokiResponse")
	proto.RegisterType((*LokiSeriesRequest)(nil), "queryrange.LokiSeriesRequest")
	proto.RegisterType((*LokiSeriesResponse)(nil), "queryrange.LokiSeriesResponse")
	proto.RegisterType((*LokiLabelNamesResponse)(nil), "queryrange.LokiLabelNamesResponse")
	proto.RegisterType((*LokiData)(nil), "queryrange.LokiData")
	proto.RegisterType((*LokiPromResponse)(nil), "queryrange.LokiPromResponse")
	proto.RegisterType((*IndexStatsResponse)(nil), "queryrange.IndexStatsResponse")
	proto.RegisterType((*VolumeResponse)(nil), "queryrange.VolumeResponse")
	proto.RegisterType((*TopKSketchesResponse)(nil), "queryrange.TopKSketchesResponse")
	proto.RegisterType((*QuantileSketchResponse)(nil), "queryrange.QuantileSketchResponse")
	proto.RegisterType((*CountMinSketchResponse)(nil), "queryrange.CountMinSketchResponse")
	proto.RegisterType((*ShardsResponse)(nil), "queryrange.ShardsResponse")
	proto.RegisterType((*DetectedFieldsResponse)(nil), "queryrange.DetectedFieldsResponse")
	proto.RegisterType((*QueryPatternsResponse)(nil), "queryrange.QueryPatternsResponse")
	proto.RegisterType((*DetectedLabelsResponse)(nil), "queryrange.DetectedLabelsResponse")
	proto.RegisterType((*QueryResponse)(nil), "queryrange.QueryResponse")
	proto.RegisterType((*QueryRequest)(nil), "queryrange.QueryRequest")
}
