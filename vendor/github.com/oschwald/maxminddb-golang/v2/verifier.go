package maxminddb

import (
	"bytes"
	"runtime"
	"unicode/utf8"

	"github.com/oschwald/maxminddb-golang/v2/internal/decoder"
	"github.com/oschwald/maxminddb-golang/v2/internal/mmdberrors"
)

type verifier struct {
	reader *Reader
}

type searchTreeWalker struct {
	reader     *Reader
	offsets    map[uint]bool
	nodeStates []uint8
	stateCount uint
}

const (
	searchTreeNodeUnvisited = 0
	searchTreeNodeVisiting  = 255
)

func (w *searchTreeWalker) verifyNode(node, bitDepth uint) (uint8, error) {
	if bitDepth >= 128 {
		return 0, mmdberrors.NewInvalidDatabaseError(
			"invalid search tree: internal node at bit depth %d",
			bitDepth,
		)
	}
	switch w.nodeStates[node] {
	case searchTreeNodeVisiting:
		return 0, mmdberrors.NewInvalidDatabaseError(
			"invalid search tree: cycle at node %d",
			node,
		)
	case searchTreeNodeUnvisited:
		// Visit the node below.
	default:
		return w.nodeStates[node], nil
	}
	w.nodeStates[node] = searchTreeNodeVisiting
	w.stateCount++

	base := node * w.reader.nodeOffsetMult
	left, right, err := readNodePairBySize(
		w.reader.buffer,
		base,
		w.reader.Metadata.RecordSize,
	)
	if err != nil {
		return 0, err
	}

	leftHeight, err := w.verifyPointer(left, bitDepth+1)
	if err != nil {
		return 0, err
	}
	rightHeight, err := w.verifyPointer(right, bitDepth+1)
	if err != nil {
		return 0, err
	}
	maximumChildHeight := max(leftHeight, rightHeight)
	if maximumChildHeight == 128 {
		return 0, mmdberrors.NewInvalidDatabaseError(
			"invalid search tree: path exceeds 128 bits",
		)
	}
	height := maximumChildHeight + 1
	w.nodeStates[node] = height
	return height, nil
}

func (w *searchTreeWalker) verifyPointer(pointer, bitDepth uint) (uint8, error) {
	if pointer == w.reader.Metadata.NodeCount {
		return 0, nil
	}
	if pointer < w.reader.Metadata.NodeCount {
		return w.verifyNode(pointer, bitDepth)
	}
	offset, err := w.reader.resolveDataPointer(pointer)
	if err != nil {
		return 0, err
	}
	w.offsets[uint(offset)] = true
	return 0, nil
}

// Verify performs comprehensive validation of the MaxMind DB file.
//
// This method validates:
//   - Metadata section: format versions, required fields, and value constraints
//   - Search tree: traverses all networks to verify tree structure integrity
//   - Data section separator: validates the 16-byte separator between tree and data
//   - Data section: verifies all data records referenced by the search tree
//
// The verifier is stricter than the MaxMind DB specification and may return
// errors on some databases that are still readable by normal operations.
// This method is useful for:
//   - Validating database files after download or generation
//   - Debugging database corruption issues
//   - Ensuring database integrity in critical applications
//
// Note: Verification traverses the entire database and may be slow on large files.
// Each data record and the original metadata graph, including unknown metadata
// fields, receives an independent set of decoder operation limits while it is
// materialized for verification.
// A successful result applies only while the Reader's backing file or byte
// slice remains unchanged.
// The method is thread-safe and can be called on an active Reader.
func (r *Reader) Verify() error {
	v := verifier{r}
	if err := v.verifyMetadata(); err != nil {
		return err
	}

	err := v.verifyDatabase()
	runtime.KeepAlive(v.reader)
	return err
}

func (v *verifier) verifyMetadata() error {
	if len(v.reader.buffer) != 0 {
		markerOffset := bytes.LastIndex(v.reader.buffer, metadataStartMarker)
		if markerOffset < 0 {
			return mmdberrors.NewInvalidDatabaseError("metadata marker not found")
		}
		metadataOffset := markerOffset + len(metadataStartMarker)
		if err := decoder.VerifyMetadata(v.reader.buffer[metadataOffset:]); err != nil {
			return err
		}
	}

	metadata := v.reader.Metadata

	if metadata.BinaryFormatMajorVersion != 2 {
		return testError(
			"binary_format_major_version",
			2,
			metadata.BinaryFormatMajorVersion,
		)
	}

	if metadata.BinaryFormatMinorVersion != 0 {
		return testError(
			"binary_format_minor_version",
			0,
			metadata.BinaryFormatMinorVersion,
		)
	}

	if metadata.DatabaseType == "" {
		return testError(
			"database_type",
			"non-empty string",
			metadata.DatabaseType,
		)
	}
	if !utf8.ValidString(metadata.DatabaseType) {
		return mmdberrors.NewInvalidDatabaseError("database_type contains invalid UTF-8")
	}
	for language, description := range metadata.Description {
		if !utf8.ValidString(language) || !utf8.ValidString(description) {
			return mmdberrors.NewInvalidDatabaseError("description contains invalid UTF-8")
		}
	}
	for _, language := range metadata.Languages {
		if !utf8.ValidString(language) {
			return mmdberrors.NewInvalidDatabaseError("languages contains invalid UTF-8")
		}
	}

	if len(metadata.Description) == 0 {
		return testError(
			"description",
			"non-empty map",
			metadata.Description,
		)
	}

	if metadata.IPVersion != 4 && metadata.IPVersion != 6 {
		return testError(
			"ip_version",
			"4 or 6",
			metadata.IPVersion,
		)
	}

	if metadata.RecordSize != 24 &&
		metadata.RecordSize != 28 &&
		metadata.RecordSize != 32 {
		return testError(
			"record_size",
			"24, 28, or 32",
			metadata.RecordSize,
		)
	}

	if metadata.NodeCount == 0 {
		return testError(
			"node_count",
			"positive integer",
			metadata.NodeCount,
		)
	}
	return nil
}

func (v *verifier) verifyDatabase() error {
	offsets, err := v.verifySearchTree()
	if err != nil {
		return err
	}

	if err := v.verifyDataSectionSeparator(); err != nil {
		return err
	}

	return v.reader.decoder.VerifyDataSection(offsets)
}

func (v *verifier) verifySearchTree() (map[uint]bool, error) {
	offsets, _, err := v.verifySearchTreeWithStateCount()
	return offsets, err
}

func (v *verifier) verifySearchTreeWithStateCount() (map[uint]bool, uint, error) {
	offsets := make(map[uint]bool)
	reader := v.reader
	nodeCount := reader.Metadata.NodeCount
	if reader.Metadata.RecordSize != 24 &&
		reader.Metadata.RecordSize != 28 &&
		reader.Metadata.RecordSize != 32 {
		return nil, 0, mmdberrors.NewInvalidDatabaseError("unsupported record size")
	}
	if reader.nodeOffsetMult == 0 || nodeCount > uint(len(reader.buffer))/reader.nodeOffsetMult {
		return nil, 0, mmdberrors.NewInvalidDatabaseError(
			"bounds check failed during search tree verification",
		)
	}
	bitDepth := uint8(0)
	if reader.Metadata.IPVersion == 4 {
		bitDepth = 96
	}
	walker := searchTreeWalker{
		reader:     reader,
		offsets:    offsets,
		nodeStates: make([]uint8, int(nodeCount)),
	}
	height, err := walker.verifyNode(0, uint(bitDepth))
	if err != nil {
		return nil, walker.stateCount, err
	}
	if uint(bitDepth)+uint(height) > 128 {
		return nil, walker.stateCount, mmdberrors.NewInvalidDatabaseError(
			"invalid search tree: path exceeds 128 bits",
		)
	}

	return offsets, walker.stateCount, nil
}

func (v *verifier) verifyDataSectionSeparator() error {
	separatorStart := searchTreeSizeBytes(
		v.reader.Metadata.NodeCount,
		v.reader.Metadata.RecordSize,
	)
	separatorEnd := separatorStart + dataSectionSeparatorSize
	if separatorEnd < separatorStart || separatorEnd > uint(len(v.reader.buffer)) {
		return mmdberrors.NewInvalidDatabaseError(
			"unexpected end of database while reading data section separator",
		)
	}

	separator := v.reader.buffer[separatorStart:separatorEnd]

	var zeroSeparator [16]byte
	if [16]byte(separator) != zeroSeparator {
		return mmdberrors.NewInvalidDatabaseError(
			"unexpected byte in data separator: %v",
			separator,
		)
	}
	return nil
}

func testError(
	field string,
	expected any,
	actual any,
) error {
	return mmdberrors.NewInvalidDatabaseError(
		"%v - Expected: %v Actual: %v",
		field,
		expected,
		actual,
	)
}
