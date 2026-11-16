package compactionv2

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// ResultRecordSchema is the Arrow schema of the record a compaction job emits
// to report its artifacts: one row per artifact, at least one row.
var ResultRecordSchema = arrow.NewSchema([]arrow.Field{
	{Name: "path", Type: arrow.BinaryTypes.String, Nullable: false},
	{Name: "min_time", Type: arrow.FixedWidthTypes.Timestamp_ns, Nullable: true},
	{Name: "max_time", Type: arrow.FixedWidthTypes.Timestamp_ns, Nullable: true},
	{Name: "min_posting_key", Type: arrow.BinaryTypes.Binary, Nullable: true},
	{Name: "max_posting_key", Type: arrow.BinaryTypes.Binary, Nullable: true},
}, nil)

// ResultArtifact is one object that a completed task uploaded. It is usable
// only when returned with a nil error. Its newly referenced log objects are
// uploaded too, but the ToC does not reference it yet. The zero value is
// invalid.
type ResultArtifact struct {
	Path string

	// MinTime and MaxTime bound the timestamps of the postings rows of an
	// index artifact. Both bounds are inclusive. Both are zero if the artifact
	// is not an index or holds no postings rows.
	MinTime, MaxTime time.Time

	// PostingKeys bounds the posting keys of an index artifact. The zero
	// value means the bounds are unknown.
	PostingKeys postings.KeyRange
}

// Validate returns an error if the artifact has no path, or if its time range
// sets only one bound or ends before it starts.
func (a ResultArtifact) Validate() error {
	switch {
	case a.Path == "":
		return errors.New("result artifact: empty path")
	case a.MinTime.IsZero() != a.MaxTime.IsZero():
		return fmt.Errorf("result artifact %s: time range sets only one bound", a.Path)
	case a.MaxTime.Before(a.MinTime):
		return fmt.Errorf("result artifact %s: time range ends at %s, before its start at %s", a.Path, a.MaxTime, a.MinTime)
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
		minTimes       = b.Field(1).(*array.TimestampBuilder)
		maxTimes       = b.Field(2).(*array.TimestampBuilder)
		minPostingKeys = b.Field(3).(*array.BinaryBuilder)
		maxPostingKeys = b.Field(4).(*array.BinaryBuilder)
	)
	for _, artifact := range artifacts {
		if err := artifact.Validate(); err != nil {
			return nil, err
		}
		paths.Append(artifact.Path)
		if artifact.MinTime.IsZero() {
			minTimes.AppendNull()
			maxTimes.AppendNull()
		} else {
			minTimes.Append(arrow.Timestamp(artifact.MinTime.UnixNano()))
			maxTimes.Append(arrow.Timestamp(artifact.MaxTime.UnixNano()))
		}
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
func DecodeResultArtifacts(rec arrow.RecordBatch) ([]ResultArtifact, error) {
	if rec == nil {
		return nil, errors.New("result record: missing record")
	}
	if !rec.Schema().Equal(ResultRecordSchema) {
		return nil, errors.New("result record: schema does not match ResultRecordSchema")
	}
	if rec.NumRows() == 0 {
		return nil, errors.New("result record: no rows")
	}
	var (
		paths          = rec.Column(0).(*array.String)
		minTimes       = rec.Column(1).(*array.Timestamp)
		maxTimes       = rec.Column(2).(*array.Timestamp)
		minPostingKeys = rec.Column(3).(*array.Binary)
		maxPostingKeys = rec.Column(4).(*array.Binary)
	)
	artifacts := make([]ResultArtifact, 0, rec.NumRows())
	for i := range int(rec.NumRows()) {
		if paths.IsNull(i) {
			return nil, fmt.Errorf("result record: null path in row %d", i)
		}
		artifact := ResultArtifact{Path: strings.Clone(paths.Value(i))}
		if !minTimes.IsNull(i) {
			artifact.MinTime = time.Unix(0, int64(minTimes.Value(i))).UTC()
		}
		if !maxTimes.IsNull(i) {
			artifact.MaxTime = time.Unix(0, int64(maxTimes.Value(i))).UTC()
		}
		switch minNull, maxNull := minPostingKeys.IsNull(i), maxPostingKeys.IsNull(i); {
		case minNull != maxNull:
			return nil, fmt.Errorf("result record: row %d sets only one posting key bound", i)
		case !minNull:
			keys, err := postings.DecodeKeyRange(minPostingKeys.Value(i), maxPostingKeys.Value(i))
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
