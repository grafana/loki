package bloom_shipper

import (
	"context"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/config"
)

type BlockRef struct {
	TenantID      string
	TSDBSource    string
	BlockFilePath string
	//todo check if it should be string
	Checksum string

	MinFingerprint, MaxFingerprint uint64
	// check if it has to be time.Time
	StartTimestamp, EndTimestamp uint64
}

// todo rename it
type Meta struct {
	TenantID                       string
	TableName                      string
	MinFingerprint, MaxFingerprint uint64
	// check if it has to be time.Time
	StartTimestamp, EndTimestamp uint64
	//todo check if it should be string
	Checksum string

	Tombstones []BlockRef
	Blocks     []BlockRef
}

type MetaSearchParams struct {
	TenantID       string
	MinFingerprint uint64
	MaxFingerprint uint64
	// check if it has to be time.Time
	StartTimestamp uint64
	EndTimestamp   uint64
}

type MetaShipper interface {
	// Returns all metas that are within MinFingerprint-MaxFingerprint fingerprint range
	// and intersect time period from StartTimestamp to EndTimestamp.
	GetAll(ctx context.Context, params MetaSearchParams) ([]Meta, error)
	Upload(ctx context.Context, meta Meta) error
	Delete(ctx context.Context, meta Meta) error
}

type Block struct {
	BlockRef
	//todo TBD
	Data []byte
}

type BlockShipper interface {
	GetBlocks(ctx context.Context, references []BlockRef) ([]Block, error)
	UploadBlocks(ctx context.Context, blocks []Block) error
	DeleteBlocks(ctx context.Context, blocks []Block) error
}

type Shipper interface {
	MetaShipper
	BlockShipper
	Stop()
}

func NewShipper(periodicConfigs []config.PeriodConfig, storageConfig storage.Config) Shipper {
	return nil
}
