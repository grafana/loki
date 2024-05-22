package planner

import (
	"context"
	"github.com/google/uuid"
	"time"

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
	id string

	table           string
	tenant          string
	OwnershipBounds v1.FingerprintBounds
	tsdb            tsdb.SingleTenantTSDBIdentifier
	gaps            []GapWithBlocks

	// Tracking
	queueTime time.Time
	ctx       context.Context
}

func NewTask(table, tenant string, bounds v1.FingerprintBounds, tsdb tsdb.SingleTenantTSDBIdentifier, gaps []GapWithBlocks) *Task {
	return &Task{
		id: uuid.NewString(),

		table:           table,
		tenant:          tenant,
		OwnershipBounds: bounds,
		tsdb:            tsdb,
		gaps:            gaps,
	}
}

func (t *Task) ID() string {
	return t.id
}

func (t *Task) WithQueueTracking(ctx context.Context, queueTime time.Time) *Task {
	t.queueTime = queueTime
	t.ctx = ctx
	return t
}
