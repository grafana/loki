package planner

import (
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
)

// TODO: Extract this definiton to a proto file at pkg/bloombuild/protos/protos.proto

type GapWithBlocks struct {
	bounds v1.FingerprintBounds
	blocks []bloomshipper.BlockRef
}

type Task struct {
	table           string
	tenant          string
	OwnershipBounds v1.FingerprintBounds
	tsdb            tsdb.SingleTenantTSDBIdentifier
	gaps            []GapWithBlocks
}
