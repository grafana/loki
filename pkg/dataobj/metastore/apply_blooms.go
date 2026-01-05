package metastore

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/compute"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/xcap"
)

type applyBlooms struct {
	logger     log.Logger
	obj        *dataobj.Object
	predicates []*labels.Matcher
	input      ArrowRecordBatchReader
	region     *xcap.Region

	rowsRead uint64
}

func newApplyBlooms(obj *dataobj.Object, predicates []*labels.Matcher, input ArrowRecordBatchReader, region *xcap.Region) ArrowRecordBatchReader {
	var equalPredicates []*labels.Matcher
	for _, p := range predicates {
		if p.Type == labels.MatchEqual {
			equalPredicates = append(equalPredicates, p)
		}
	}

	return &applyBlooms{
		obj:        obj,
		predicates: equalPredicates,
		input:      input,
		region:     region,
	}
}

func (a *applyBlooms) Close() {
	a.input.Close()
}

func (a *applyBlooms) totalReadRows() uint64 {
	return a.rowsRead
}

func (a *applyBlooms) Read(ctx context.Context) (arrow.RecordBatch, error) {
	ctx = xcap.ContextWithRegion(ctx, a.region)
	if len(a.predicates) == 0 {
		rec, err := a.input.Read(ctx)
		if rec != nil {
			a.rowsRead += uint64(rec.NumRows())
		}
		return rec, err
	}
	// TODO(ivkalita): do not read all the input at once
	recs, matchedRows, err := a.readAllInput(ctx)
	if err != nil {
		return nil, err
	}
	a.rowsRead = uint64(matchedRows)
	if len(recs) == 0 {
		return nil, io.EOF
	}

	// we expect all the input records to have the same schema (otherwise we won't be able to merge them)
	commonSchema, err := a.validateInputRecordsSchema(recs)
	if err != nil {
		return nil, fmt.Errorf("validate input schema: %w", err)
	}
	chunks := make([][]arrow.Array, commonSchema.NumFields())

	for _, predicate := range a.predicates {
		if matchedRows == 0 {
			// short-circuiting if there are no matched rows left
			level.Debug(utillog.WithContext(ctx, a.logger)).Log("msg", "no sections resolved", "reason", "no matching predicates")
			return nil, io.EOF
		}

		// find matched section keys for the predicate
		sColumnName := scalar.NewStringScalar(predicate.Name)
		matchedSectionKeys := make(map[SectionKey]struct{})
		err := forEachMatchedPointerSectionKey(ctx, a.obj, sColumnName, predicate.Value, func(sk SectionKey) {
			matchedSectionKeys[sk] = struct{}{}
		})
		if err != nil {
			return nil, fmt.Errorf("reading section keys: %w", err)
		}

		if len(matchedSectionKeys) == 0 {
			return nil, io.EOF
		}

		matchedRows = 0
		for recIdx, rec := range recs {
			mask, err := a.buildKeepBitmask(rec, matchedSectionKeys)
			if err != nil {
				return nil, fmt.Errorf("build keep bitmask: %w", err)
			}

			for colIdx, col := range rec.Columns() {
				out, err := compute.FilterArray(ctx, col, mask, compute.FilterOptions{})
				if err != nil {
					return nil, err
				}
				chunks[colIdx] = append(chunks[colIdx], out)
			}
			// the number of rows is the same in all the columns hence we can use any of them to understand how many
			// rows matched all the predicates up to this point
			matchedRows += chunks[0][recIdx].Len()
		}
	}

	outCols := make([]arrow.Array, commonSchema.NumFields())
	for i := range chunks {
		concat, err := array.Concatenate(chunks[i], memory.DefaultAllocator)
		if err != nil {
			return nil, err
		}
		outCols[i] = concat
	}

	return array.NewRecordBatch(commonSchema, outCols, int64(outCols[0].Len())), nil
}

func (a *applyBlooms) readAllInput(ctx context.Context) ([]arrow.RecordBatch, int, error) {
	var recs []arrow.RecordBatch
	totalRows := 0
	for {
		rec, err := a.input.Read(ctx)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, 0, fmt.Errorf("failed to read record: %w", err)
		}

		if rec != nil && rec.NumRows() > 0 {
			totalRows += int(rec.NumRows())
			recs = append(recs, rec)
		}

		if errors.Is(err, io.EOF) {
			break
		}
	}

	return recs, totalRows, nil
}

func (a *applyBlooms) buildKeepBitmask(rec arrow.RecordBatch, matchedSectionKeys map[SectionKey]struct{}) (arrow.Array, error) {
	maskB := array.NewBooleanBuilder(memory.DefaultAllocator)
	maskB.Reserve(int(rec.NumRows()))

	buf := make([]pointers.SectionPointer, rec.NumRows())
	num, err := pointers.FromRecordBatch(rec, buf, pointers.PopulateSectionKey)
	if err != nil {
		return nil, err
	}

	for i := range num {
		sk := SectionKey{
			ObjectPath: buf[i].Path,
			SectionIdx: buf[i].Section,
		}
		_, keep := matchedSectionKeys[sk]
		maskB.Append(keep)
	}

	return maskB.NewBooleanArray(), nil
}

func (a *applyBlooms) validateInputRecordsSchema(recs []arrow.RecordBatch) (*arrow.Schema, error) {
	if len(recs) == 0 {
		return nil, fmt.Errorf("no records")
	}

	commonSchema := recs[0].Schema()
	for i := 1; i < len(recs); i++ {
		if !recs[i].Schema().Equal(commonSchema) {
			return nil, fmt.Errorf("input records schema mismatch")
		}
	}

	// applyBlooms only requires a SectionKey (path + section) to be present in the input.
	var (
		foundPath    bool
		foundSection bool
	)
	for _, field := range commonSchema.Fields() {
		if foundPath && foundSection {
			break
		}
		switch pointers.ColumnTypeFromField(field) {
		case pointers.ColumnTypePath:
			foundPath = true
			if field.Type.ID() != arrow.STRING {
				return nil, fmt.Errorf("invalid path column type: %s", field.Type)
			}
		case pointers.ColumnTypeSection:
			foundSection = true
			if field.Type.ID() != arrow.INT64 {
				return nil, fmt.Errorf("invalid section column type: %s", field.Type)
			}
		default:
			continue
		}
	}

	if !foundPath || !foundSection {
		return nil, fmt.Errorf("record is missing mandatory fields path/section")
	}

	return commonSchema, nil
}

type bloomStatsProvider interface {
	totalReadRows() uint64
}

var _ bloomStatsProvider = (*applyBlooms)(nil)

func forEachMatchedPointerSectionKey(
	ctx context.Context,
	object *dataobj.Object,
	columnName scalar.Scalar,
	matchColumnValue string,
	f func(key SectionKey),
) error {
	targetTenant, err := user.ExtractOrgID(ctx)
	if err != nil {
		return fmt.Errorf("extracting org ID: %w", err)
	}

	var reader pointers.Reader
	defer reader.Close()

	const batchSize = 128
	buf := make([]pointers.SectionPointer, batchSize)

	for _, section := range object.Sections().Filter(pointers.CheckSection) {
		if section.Tenant != targetTenant {
			continue
		}

		sec, err := pointers.Open(ctx, section)
		if err != nil {
			return fmt.Errorf("opening section: %w", err)
		}

		pointerCols, err := findPointersColumnsByTypes(
			sec.Columns(),
			pointers.ColumnTypePath,
			pointers.ColumnTypeSection,
			pointers.ColumnTypeColumnName,
			pointers.ColumnTypeValuesBloomFilter,
		)
		if err != nil {
			return fmt.Errorf("finding pointers columns: %w", err)
		}

		var (
			colColumnName *pointers.Column
			colBloom      *pointers.Column
		)

		for _, c := range pointerCols {
			if c.Type == pointers.ColumnTypeColumnName {
				colColumnName = c
			}
			if c.Type == pointers.ColumnTypeValuesBloomFilter {
				colBloom = c
			}
			if colColumnName != nil && colBloom != nil {
				break
			}
		}

		if colColumnName == nil || colBloom == nil {
			// the section has no rows for blooms and can be ignored completely
			continue
		}

		reader.Reset(
			pointers.ReaderOptions{
				Columns: pointerCols,
				Predicates: []pointers.Predicate{
					pointers.WhereBloomFilterMatches(colColumnName, colBloom, columnName, matchColumnValue),
				},
			},
		)

		for {
			rec, readErr := reader.Read(ctx, batchSize)
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return fmt.Errorf("reading record batch: %w", readErr)
			}

			if rec != nil && rec.NumRows() > 0 {
				num, err := pointers.FromRecordBatch(rec, buf, pointers.PopulateSectionKey)
				if err != nil {
					return err
				}
				for i := range num {
					sk := SectionKey{
						ObjectPath: buf[i].Path,
						SectionIdx: buf[i].Section,
					}
					f(sk)
				}
			}

			if errors.Is(readErr, io.EOF) {
				break
			}
		}
	}

	return nil
}
