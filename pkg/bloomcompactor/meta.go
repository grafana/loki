package bloomcompactor

import (
	"fmt"
	"path"

	"github.com/pkg/errors"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb"
)

const (
	BloomPrefix = "bloom"
	MetasPrefix = "metas"
)

type MetaRef struct {
	OwnershipRange v1.FingerprintBounds
	Checksum       uint32
}

// `bloom/<period>/<tenant>/metas/<start_fp>-<end_fp>-<checksum>.json`
func (m MetaRef) Address(tenant string, period int) (string, error) {
	joined := path.Join(
		BloomPrefix,
		fmt.Sprintf("%v", period),
		tenant,
		MetasPrefix,
		fmt.Sprintf("%v-%v", m.OwnershipRange, m.Checksum),
	)

	return fmt.Sprintf("%s.json", joined), nil
}

type Meta struct {

	// The fingerprint range of the block. This is the range _owned_ by the meta and
	// is greater than or equal to the range of the actual data in the underlying blocks.
	OwnershipRange v1.FingerprintBounds

	// Old blocks which can be deleted in the future. These should be from previous compaction rounds.
	Tombstones []bloomshipper.BlockRef

	// The specific TSDB files used to generate the block.
	Sources []tsdb.SingleTenantTSDBIdentifier

	// A list of blocks that were generated
	Blocks []bloomshipper.BlockRef
}

// Generate MetaRef from Meta
func (m Meta) Ref() (MetaRef, error) {
	checksum, err := m.Checksum()
	if err != nil {
		return MetaRef{}, errors.Wrap(err, "getting checksum")
	}
	return MetaRef{
		OwnershipRange: m.OwnershipRange,
		Checksum:       checksum,
	}, nil
}

func (m Meta) Checksum() (uint32, error) {
	h := v1.Crc32HashPool.Get()
	defer v1.Crc32HashPool.Put(h)

	_, err := h.Write([]byte(m.OwnershipRange.String()))
	if err != nil {
		return 0, errors.Wrap(err, "writing OwnershipRange")
	}

	for _, tombstone := range m.Tombstones {
		err = tombstone.Hash(h)
		if err != nil {
			return 0, errors.Wrap(err, "writing Tombstones")
		}
	}

	for _, source := range m.Sources {
		err = source.Hash(h)
		if err != nil {
			return 0, errors.Wrap(err, "writing Sources")
		}
	}

	for _, block := range m.Blocks {
		err = block.Hash(h)
		if err != nil {
			return 0, errors.Wrap(err, "writing Blocks")
		}
	}

	return h.Sum32(), nil

}

type TSDBStore interface {
	ResolveTSDBs() ([]*tsdb.SingleTenantTSDBIdentifier, error)
	LoadTSDB(id tsdb.Identifier, bounds v1.FingerprintBounds) (v1.CloseableIterator[*v1.Series], error)
}

type MetaStore interface {
	ResolveMetas(bounds v1.FingerprintBounds) ([]MetaRef, error)
	GetMetas([]MetaRef) ([]Meta, error)
	PutMeta(Meta) error
}

type BlockStore interface {
	// TODO(owen-d): flesh out|integrate against bloomshipper.Client
	GetBlocks([]bloomshipper.BlockRef) ([]*v1.Block, error)
	PutBlock(interface{}) error
}
