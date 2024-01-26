package bloomcompactor

import (
	"fmt"
	"hash"
	"path"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/pkg/errors"
)

type BlockRef struct{}

func (r BlockRef) Hash(h hash.Hash32) (n int, err error) {
	// TODO(owen-d): implement
	return h.Write(nil)
}

type MetaRef struct {
	UserID string
	Period int
}

type Meta struct {
	UserID string
	Period int

	// The fingerprint range of the block. This is the range _owned_ by the meta and
	// is greater than or equal to the range of the actual data in the underlying blocks.
	OwnershipRange v1.FingerprintBounds

	// Old blocks which can be deleted in the future. These should be from pervious compaction rounds.
	Tombstones []BlockRef

	// The specific TSDB files used to generate the block.
	Sources []tsdb.SingleTenantTSDBIdentifier

	// A list of blocks that were generated
	Blocks []BlockRef
}

// `bloom/<period>/<tenant>/metas/<start_fp>-<end_fp>-<start_ts>-<end_ts>-<checksum>`
func (m Meta) Address() (string, error) {
	checksum, err := m.Checksum()
	if err != nil {
		return "", errors.Wrap(err, "getting checksum")
	}

	joined := path.Join(
		"bloom",
		fmt.Sprintf("%v", m.Period),
		m.UserID,
		"metas",
		m.OwnershipRange.String(),
		fmt.Sprintf("%v", checksum),
	)

	return fmt.Sprintf("%s.json", joined), nil
}

func (m Meta) Checksum() (uint32, error) {
	h := v1.Crc32HashPool.Get()
	defer v1.Crc32HashPool.Put(h)

	_, err := h.Write([]byte(m.UserID))
	if err != nil {
		return 0, errors.Wrap(err, "writing UserID")
	}

	_, err = h.Write([]byte(fmt.Sprintf("%v", m.Period)))
	if err != nil {
		return 0, errors.Wrap(err, "writing Period")
	}

	_, err = h.Write([]byte(m.OwnershipRange.String()))
	if err != nil {
		return 0, errors.Wrap(err, "writing OwnershipRange")
	}

	for _, tombstone := range m.Tombstones {
		_, err = tombstone.Hash(h)
		if err != nil {
			return 0, errors.Wrap(err, "writing Tombstones")
		}
	}

	for _, source := range m.Sources {
		_, err = source.Hash(h)
		if err != nil {
			return 0, errors.Wrap(err, "writing Sources")
		}
	}

	for _, block := range m.Blocks {
		_, err = block.Hash(h)
		if err != nil {
			return 0, errors.Wrap(err, "writing Blocks")
		}
	}

	return h.Sum32(), nil

}

type TSDBStore interface {
	ResolveTSDBs() ([]*tsdb.TSDBFile, error)
}

type MetaStore interface {
	GetMetas([]MetaRef) ([]Meta, error)
	PutMeta(Meta) error
	ResolveMetas(bounds v1.FingerprintBounds) ([]MetaRef, error)
}

type BlockStore interface {
	// TODO(owen-d): flesh out
	GetBlocks([]BlockRef) ([]interface{}, error)
	PutBlock(interface{}) error
}
