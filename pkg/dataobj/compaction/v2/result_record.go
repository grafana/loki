package compactionv2

import (
	"errors"
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// ResultRecordSchema is the Arrow schema of the record a compaction job emits
// to report its artifacts: one row per artifact, at least one row. Null
// posting keys mean the bounds of the artifact are unknown.
var ResultRecordSchema = arrow.NewSchema([]arrow.Field{
	{Name: "path", Type: arrow.BinaryTypes.String, Nullable: false},
	{Name: "min_posting_key", Type: arrow.BinaryTypes.Binary, Nullable: true},
	{Name: "max_posting_key", Type: arrow.BinaryTypes.Binary, Nullable: true},
}, nil)

// legacyResultRecordSchema is the schema of ResultRecordSchema before it had
// posting key columns: one row with the path of one artifact.
var legacyResultRecordSchema = arrow.NewSchema([]arrow.Field{
	{Name: "path", Type: arrow.BinaryTypes.String, Nullable: false},
}, nil)

// ResultArtifact is one object that a completed task uploaded. It holds only
// what the task decides while it runs: the coordinator knows the time range
// and the shard layout of the outputs of a task when it plans the task.
//
// A ResultArtifact is usable only when returned with a nil error. The zero
// value is not valid.
type ResultArtifact struct {
	Path string

	// PostingKeys bounds the posting keys of an index artifact. The zero
	// value means the bounds are unknown.
	PostingKeys postings.KeyRange
}

// Validate returns an error if the artifact has no path.
func (a ResultArtifact) Validate() error {
	if a.Path == "" {
		return errors.New("result artifact: empty path")
	}
	return nil
}

// EncodeResultArtifacts encodes artifacts as a record batch under
// ResultRecordSchema, one row per artifact. It returns an error if artifacts
// is empty or an artifact is not valid. The caller owns the returned record
// batch.
func EncodeResultArtifacts(mem memory.Allocator, artifacts []ResultArtifact) (arrow.RecordBatch, error) {
	if len(artifacts) == 0 {
		return nil, errors.New("result record: no artifacts")
	}
	b := array.NewRecordBuilder(mem, ResultRecordSchema)
	defer b.Release()
	var (
		paths          = b.Field(0).(*array.StringBuilder)
		minPostingKeys = b.Field(1).(*array.BinaryBuilder)
		maxPostingKeys = b.Field(2).(*array.BinaryBuilder)
	)
	for _, artifact := range artifacts {
		if err := artifact.Validate(); err != nil {
			return nil, err
		}
		paths.Append(artifact.Path)
		if artifact.PostingKeys.IsZero() {
			minPostingKeys.AppendNull()
			maxPostingKeys.AppendNull()
		} else {
			minEncoded, maxEncoded := artifact.PostingKeys.Encode()
			minPostingKeys.Append(minEncoded)
			maxPostingKeys.Append(maxEncoded)
		}
	}
	return b.NewRecordBatch(), nil
}

// DecodeResultArtifacts decodes the artifacts of rec, which
// EncodeResultArtifacts encoded. It returns an error if rec has another
// schema, has no rows, or holds an artifact that is not valid. The artifacts
// do not borrow memory from rec.
//
// It also decodes the record of a worker that uses legacyResultRecordSchema,
// as artifacts with unknown posting key bounds. This lets a coordinator update
// before its workers.
func DecodeResultArtifacts(rec arrow.RecordBatch) ([]ResultArtifact, error) {
	if rec == nil {
		return nil, errors.New("result record: missing record")
	}
	if rec.NumRows() == 0 {
		return nil, errors.New("result record: no rows")
	}
	legacy := rec.Schema().Equal(legacyResultRecordSchema)
	if !legacy && !rec.Schema().Equal(ResultRecordSchema) {
		return nil, errors.New("result record: schema does not match ResultRecordSchema")
	}

	paths := rec.Column(0).(*array.String)
	artifacts := make([]ResultArtifact, 0, rec.NumRows())
	for i := range int(rec.NumRows()) {
		if paths.IsNull(i) {
			return nil, fmt.Errorf("result record: null path in row %d", i)
		}
		artifact := ResultArtifact{Path: strings.Clone(paths.Value(i))}
		if !legacy {
			keys, err := decodePostingKeys(rec.Column(1).(*array.Binary), rec.Column(2).(*array.Binary), i)
			if err != nil {
				return nil, fmt.Errorf("result record: row %d: %w", i, err)
			}
			artifact.PostingKeys = keys
		}
		if err := artifact.Validate(); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

// decodePostingKeys returns the posting key bounds of row i. It returns the
// zero KeyRange if both bounds are null, and an error if only one is.
func decodePostingKeys(minPostingKeys, maxPostingKeys *array.Binary, i int) (postings.KeyRange, error) {
	switch minNull, maxNull := minPostingKeys.IsNull(i), maxPostingKeys.IsNull(i); {
	case minNull && maxNull:
		return postings.KeyRange{}, nil
	case minNull != maxNull:
		return postings.KeyRange{}, errors.New("only one posting key bound is set")
	}
	return postings.DecodeKeyRange(minPostingKeys.Value(i), maxPostingKeys.Value(i))
}
