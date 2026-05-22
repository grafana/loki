package columnar

import "github.com/grafana/loki/v3/pkg/columnar/types"

// Global definitions of types to reduce allocations.

var (
	nullType   = &types.Null{}
	boolType   = &types.Bool{Nullable: true}
	int32Type  = &types.Int32{Nullable: true}
	int64Type  = &types.Int64{Nullable: true}
	uint32Type = &types.Uint32{Nullable: true}
	uint64Type = &types.Uint64{Nullable: true}
	utf8Type   = &types.UTF8{Nullable: true}
)
