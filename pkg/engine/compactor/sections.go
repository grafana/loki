package compactor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
)

// indexEntry is one index object listed in a ToC for a particular tenant.
type indexEntry struct {
	Path  string
	Start time.Time
	End   time.Time
}

// tenantIndexes maps tenant ID → ordered list of indexes the ToC references
// for that tenant. Slice order reflects ToC enumeration order and is not
// part of the contract — callers must not rely on it for correctness.
type tenantIndexes map[string][]indexEntry

// loadTenantIndexes reads the ToC for the given window-aligned time and
// returns every (tenant, index path, time range) triple it references.
//
// This is the per-cycle planning input: the coordinator iterates the result
// map and skips tenants whose index slice has length ≤ 1 (the convergence
// gate). If the ToC does not exist for this window the call returns a
// bucket.IsObjNotFoundErr-class error which the coordinator treats as
// "nothing to do this cycle" — the next polling tick re-reads.
//
// Unlike pkg/dataobj/metastore.forEachIndexPointer, this helper does NOT
// filter by user.ExtractOrgID — it walks every tenant's indexpointers
// section and returns them grouped, which is what the coordinator's
// per-tenant loop needs.
func loadTenantIndexes(
	ctx context.Context,
	bucket objstore.Bucket,
	window time.Time,
) (tenantIndexes, error) {
	tocPath := metastore.TableOfContentsPath(window.UTC().Truncate(metastore.MetastoreWindowSize))

	r, err := bucket.Get(ctx, tocPath)
	if err != nil {
		return nil, err // includes IsObjNotFoundErr — caller checks
	}
	defer r.Close()

	buf, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read ToC %s: %w", tocPath, err)
	}
	obj, err := dataobj.FromReaderAt(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		return nil, fmt.Errorf("decode ToC %s: %w", tocPath, err)
	}

	// Hoist the Reader and the per-batch decode scratch above the section
	// loop. A ToC has one indexpointers section per tenant; in large
	// deployments that can be hundreds. Reader.Reset(...) at each iteration
	// reuses the reader's internal allocator + record-batch state — matches
	// the upstream pattern in metastore/iter.go's forEachIndexPointer.
	var reader indexpointers.Reader
	defer reader.Close()
	const batchSize = 1024
	scratch := make([]indexEntry, batchSize)

	out := tenantIndexes{}
	for _, section := range obj.Sections().Filter(indexpointers.CheckSection) {
		tenant := section.Tenant
		entries, err := readAllIndexPointers(ctx, &reader, scratch, section)
		if err != nil {
			return nil, fmt.Errorf("read indexpointers for tenant %s: %w", tenant, err)
		}
		out[tenant] = append(out[tenant], entries...)
	}
	return out, nil
}

// readAllIndexPointers decodes every row of one indexpointers section into
// indexEntry values. The caller owns the Reader and scratch slice; both are
// reused across section iterations.
//
// Mirrors pkg/dataobj/metastore.forEachIndexPointer's structure but drops
// the user.ExtractOrgID tenant filter and the WhereTimeRangeOverlapsWith
// predicate — the compactor reads every row from every tenant in the
// most-recent ToC.
func readAllIndexPointers(ctx context.Context, reader *indexpointers.Reader, scratch []indexEntry, section *dataobj.Section) ([]indexEntry, error) {
	sec, err := indexpointers.Open(ctx, section)
	if err != nil {
		return nil, fmt.Errorf("opening indexpointers section: %w", err)
	}

	reader.Reset(indexpointers.ReaderOptions{Columns: sec.Columns()})
	if err := reader.Open(ctx); err != nil {
		return nil, fmt.Errorf("opening reader: %w", err)
	}

	batchSize := len(scratch)
	var out []indexEntry
	for {
		rec, readErr := reader.Read(ctx, batchSize)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, fmt.Errorf("reading batch: %w", readErr)
		}
		numRows := int(rec.NumRows())
		if numRows == 0 && errors.Is(readErr, io.EOF) {
			break
		}

		// Clear the rows we will populate so prior batches don't leak through.
		for i := range numRows {
			scratch[i] = indexEntry{}
		}

		for colIdx := 0; colIdx < int(rec.NumCols()); colIdx++ {
			col := rec.Column(colIdx)
			pointerCol := sec.Columns()[colIdx]
			switch pointerCol.Type {
			case indexpointers.ColumnTypePath:
				values := col.(*array.String)
				for rIdx := range numRows {
					if col.IsNull(rIdx) {
						continue
					}
					scratch[rIdx].Path = values.Value(rIdx)
				}
			case indexpointers.ColumnTypeMinTimestamp:
				values := col.(*array.Timestamp)
				for rIdx := range numRows {
					if col.IsNull(rIdx) {
						continue
					}
					scratch[rIdx].Start = time.Unix(0, int64(values.Value(rIdx)))
				}
			case indexpointers.ColumnTypeMaxTimestamp:
				values := col.(*array.Timestamp)
				for rIdx := range numRows {
					if col.IsNull(rIdx) {
						continue
					}
					scratch[rIdx].End = time.Unix(0, int64(values.Value(rIdx)))
				}
			}
		}

		for i := range numRows {
			out = append(out, scratch[i])
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	return out, nil
}

// sectionRefsFor converts a tenant's indexes into SectionRefs suitable for
// compactionv2.Plan. Returns one SectionRef per index with timestamp-only
// bounds (empty MinKey/MaxKey); the planner's composite (MinKey, MinTimestamp)
// sort key degrades to single-axis timestamp ordering, which is sufficient
// for index-only compaction.
func sectionRefsFor(indexes []indexEntry) []*compactionv2pb.SectionRef {
	out := make([]*compactionv2pb.SectionRef, len(indexes))
	for i, e := range indexes {
		out[i] = &compactionv2pb.SectionRef{
			ObjectPath:   e.Path,
			SectionIndex: 0,
			MinTimestamp: e.Start.UnixNano(),
			MaxTimestamp: e.End.UnixNano(),
		}
	}
	return out
}
