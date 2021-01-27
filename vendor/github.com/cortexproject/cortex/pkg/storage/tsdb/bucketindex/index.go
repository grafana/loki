package bucketindex

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/block/metadata"

	cortex_tsdb "github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/util"
)

const (
	IndexFilename           = "bucket-index.json"
	IndexCompressedFilename = IndexFilename + ".gz"
	IndexVersion1           = 1

	SegmentsFormatUnknown = ""

	// SegmentsFormat1Based6Digits defined segments numbered with 6 digits numbers in a sequence starting from number 1
	// eg. (000001, 000002, 000003).
	SegmentsFormat1Based6Digits = "1b6d"
)

// Index contains all known blocks and markers of a tenant.
type Index struct {
	// Version of the index format.
	Version int `json:"version"`

	// List of complete blocks (partial blocks are excluded from the index).
	Blocks Blocks `json:"blocks"`

	// List of block deletion marks.
	BlockDeletionMarks BlockDeletionMarks `json:"block_deletion_marks"`

	// UpdatedAt is a unix timestamp (seconds precision) of when the index has been updated
	// (written in the storage) the last time.
	UpdatedAt int64 `json:"updated_at"`
}

func (idx *Index) GetUpdatedAt() time.Time {
	return time.Unix(idx.UpdatedAt, 0)
}

// RemoveBlock removes block and its deletion mark (if any) from index.
func (idx *Index) RemoveBlock(id ulid.ULID) {
	for i := 0; i < len(idx.Blocks); i++ {
		if idx.Blocks[i].ID == id {
			idx.Blocks = append(idx.Blocks[:i], idx.Blocks[i+1:]...)
			break
		}
	}

	for i := 0; i < len(idx.BlockDeletionMarks); i++ {
		if idx.BlockDeletionMarks[i].ID == id {
			idx.BlockDeletionMarks = append(idx.BlockDeletionMarks[:i], idx.BlockDeletionMarks[i+1:]...)
			break
		}
	}
}

// Block holds the information about a block in the index.
type Block struct {
	// Block ID.
	ID ulid.ULID `json:"block_id"`

	// MinTime and MaxTime specify the time range all samples in the block are in (millis precision).
	MinTime int64 `json:"min_time"`
	MaxTime int64 `json:"max_time"`

	// SegmentsFormat and SegmentsNum stores the format and number of chunks segments
	// in the block, if they match a known pattern. We don't store the full segments
	// files list in order to keep the index small. SegmentsFormat is empty if segments
	// are unknown or don't match a known format.
	SegmentsFormat string `json:"segments_format,omitempty"`
	SegmentsNum    int    `json:"segments_num,omitempty"`

	// UploadedAt is a unix timestamp (seconds precision) of when the block has been completed to be uploaded
	// to the storage.
	UploadedAt int64 `json:"uploaded_at"`
}

// Within returns whether the block contains samples within the provided range.
// Input minT and maxT are both inclusive.
func (m *Block) Within(minT, maxT int64) bool {
	// NOTE: Block intervals are half-open: [MinTime, MaxTime).
	return m.MinTime <= maxT && minT < m.MaxTime
}

func (m *Block) GetUploadedAt() time.Time {
	return time.Unix(m.UploadedAt, 0)
}

// ThanosMeta returns a block meta based on the known information in the index.
// The returned meta doesn't include all original meta.json data but only a subset
// of it.
func (m *Block) ThanosMeta(userID string) *metadata.Meta {
	return &metadata.Meta{
		BlockMeta: tsdb.BlockMeta{
			ULID:    m.ID,
			MinTime: m.MinTime,
			MaxTime: m.MaxTime,
			Version: metadata.TSDBVersion1,
		},
		Thanos: metadata.Thanos{
			Version: metadata.ThanosVersion1,
			Labels: map[string]string{
				cortex_tsdb.TenantIDExternalLabel: userID,
			},
			SegmentFiles: m.thanosMetaSegmentFiles(),
		},
	}
}

func (m *Block) thanosMetaSegmentFiles() (files []string) {
	if m.SegmentsFormat == SegmentsFormat1Based6Digits {
		for i := 1; i <= m.SegmentsNum; i++ {
			files = append(files, fmt.Sprintf("%06d", i))
		}
	}

	return files
}

func (m *Block) String() string {
	minT := util.TimeFromMillis(m.MinTime).UTC()
	maxT := util.TimeFromMillis(m.MaxTime).UTC()

	return fmt.Sprintf("%s (min time: %s max time: %s)", m.ID, minT.String(), maxT.String())
}

func BlockFromThanosMeta(meta metadata.Meta) *Block {
	segmentsFormat, segmentsNum := detectBlockSegmentsFormat(meta)

	return &Block{
		ID:             meta.ULID,
		MinTime:        meta.MinTime,
		MaxTime:        meta.MaxTime,
		SegmentsFormat: segmentsFormat,
		SegmentsNum:    segmentsNum,
	}
}

func detectBlockSegmentsFormat(meta metadata.Meta) (string, int) {
	if num, ok := detectBlockSegmentsFormat1Based6Digits(meta); ok {
		return SegmentsFormat1Based6Digits, num
	}

	return "", 0
}

func detectBlockSegmentsFormat1Based6Digits(meta metadata.Meta) (int, bool) {
	// Check the (deprecated) SegmentFiles.
	if len(meta.Thanos.SegmentFiles) > 0 {
		for i, f := range meta.Thanos.SegmentFiles {
			if fmt.Sprintf("%06d", i+1) != f {
				return 0, false
			}
		}
		return len(meta.Thanos.SegmentFiles), true
	}

	// Check the Files.
	if len(meta.Thanos.Files) > 0 {
		num := 0
		for _, file := range meta.Thanos.Files {
			if !strings.HasPrefix(file.RelPath, block.ChunksDirname+string(filepath.Separator)) {
				continue
			}
			if fmt.Sprintf("%s%s%06d", block.ChunksDirname, string(filepath.Separator), num+1) != file.RelPath {
				return 0, false
			}
			num++
		}

		if num > 0 {
			return num, true
		}
	}

	return 0, false
}

// BlockDeletionMark holds the information about a block's deletion mark in the index.
type BlockDeletionMark struct {
	// Block ID.
	ID ulid.ULID `json:"block_id"`

	// DeletionTime is a unix timestamp (seconds precision) of when the block was marked to be deleted.
	DeletionTime int64 `json:"deletion_time"`
}

func (m *BlockDeletionMark) GetDeletionTime() time.Time {
	return time.Unix(m.DeletionTime, 0)
}

// ThanosMeta returns the Thanos deletion mark.
func (m *BlockDeletionMark) ThanosDeletionMark() *metadata.DeletionMark {
	return &metadata.DeletionMark{
		ID:           m.ID,
		Version:      metadata.DeletionMarkVersion1,
		DeletionTime: m.DeletionTime,
	}
}

func BlockDeletionMarkFromThanosMarker(mark *metadata.DeletionMark) *BlockDeletionMark {
	return &BlockDeletionMark{
		ID:           mark.ID,
		DeletionTime: mark.DeletionTime,
	}
}

// BlockDeletionMarks holds a set of block deletion marks in the index. No ordering guaranteed.
type BlockDeletionMarks []*BlockDeletionMark

func (s BlockDeletionMarks) GetULIDs() []ulid.ULID {
	ids := make([]ulid.ULID, len(s))
	for i, m := range s {
		ids[i] = m.ID
	}
	return ids
}

func (s BlockDeletionMarks) Clone() BlockDeletionMarks {
	clone := make(BlockDeletionMarks, len(s))
	for i, m := range s {
		v := *m
		clone[i] = &v
	}
	return clone
}

// Blocks holds a set of blocks in the index. No ordering guaranteed.
type Blocks []*Block

func (s Blocks) GetULIDs() []ulid.ULID {
	ids := make([]ulid.ULID, len(s))
	for i, m := range s {
		ids[i] = m.ID
	}
	return ids
}

func (s Blocks) String() string {
	b := strings.Builder{}

	for idx, m := range s {
		if idx > 0 {
			b.WriteString(", ")
		}
		b.WriteString(m.String())
	}

	return b.String()
}
