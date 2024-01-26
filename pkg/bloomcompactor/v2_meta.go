package bloomcompactor

import (
	"fmt"
	"hash"
	"path"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/pkg/errors"
)

// Job is a declarative description of a compaction job.
// Namely it contains:
//   - the source TSDBs (the source of truth) from which to enumerate series+chunks
//     and ensure they are in the resulting block(s)
//   - The source blocks which may already contain relevant series+chunks so we don't have to
//     re-index them
//   - The fingerprint ownership range to limit by
type Job2 struct {
	UserID string
	Period int

	// The fingerprint range of the block. This is the range _owned_ by the meta and
	// is greater than or equal to the range of the actual data in the underlying blocks.
	OwnershipRange v1.FingerprintBounds

	SourceTSDBs []TSDBRef

	SourceBlocks []BlockRef
}

type TSDBRef struct{}

func (r TSDBRef) Hash(h hash.Hash32) (n int, err error) {
	// TODO(owen-d): implement
	return h.Write(nil)
}

type BlockRef struct{}

func (r BlockRef) Hash(h hash.Hash32) (n int, err error) {
	// TODO(owen-d): implement
	return h.Write(nil)
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
	Sources []TSDBRef

	// A list of blocks that were generated
	Blocks []BlockRef
}

func (m Meta) Address() (string, error) {
	// `bloom/<period>/<tenant>/metas/<start_fp>-<end_fp>-<start_ts>-<end_ts>-<checksum>`
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
