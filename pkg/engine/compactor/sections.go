package compactor

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

const prefetchBytes = 2 * 1024 * 1024

// errCorruptIndex reports an index whose sections of one tenant do not share
// one stream layout, or whose layout cannot be read. No retry can fix it, so
// compaction skips it.
var errCorruptIndex = errors.New("corrupt index")

// indexEntry is one index object listed in a ToC for a particular tenant.
type indexEntry struct {
	Path  string
	Start time.Time
	End   time.Time
}

// logSortPrefix is the planner key for log sections. It orders by the shared
// [streams.SortPrefix], so planner order matches stream ID order in objects.
// labels keeps the separate schema values for [compactionv2pb.SectionRef].
type logSortPrefix struct {
	prefix streams.SortPrefix
	labels []string
}

func newLogSortPrefix(shard uint32, labels []string) logSortPrefix {
	return logSortPrefix{prefix: streams.NewSortPrefix(shard, labels), labels: labels}
}

func compareLogSortPrefix(a, b logSortPrefix) int {
	return a.prefix.Compare(b.prefix)
}

type indexSortKey struct {
	kind                       postings.PostingKind
	columnName, labelValue     string
	minTimestamp, maxTimestamp int64
}

func compareIndexSortKey(a, b indexSortKey) int {
	return cmp.Or(
		cmp.Compare(a.kind, b.kind),
		strings.Compare(a.columnName, b.columnName),
		strings.Compare(a.labelValue, b.labelValue),
		cmp.Compare(a.minTimestamp, b.minTimestamp),
		cmp.Compare(a.maxTimestamp, b.maxTimestamp),
	)
}

func indexKey(row postings.Row) indexSortKey {
	return indexSortKey{
		kind:         row.Kind,
		columnName:   row.ColumnName,
		labelValue:   row.LabelValue,
		minTimestamp: row.MinTimestamp,
		maxTimestamp: row.MaxTimestamp,
	}
}

// loadTenantIndexes returns the entries of the tenant's ToC for the window.
// Entry order reflects ToC enumeration order and is not part of the contract
// — callers must not rely on it for correctness.
func loadTenantIndexes(ctx context.Context, bucket objstore.Bucket, window time.Time, tenant string) ([]indexEntry, error) {
	tocPath := metastore.TableOfContentsPath(tenant, window.UTC().Truncate(metastore.MetastoreWindowSize))

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
	// loop. Reader.Reset(...) at each iteration reuses the reader's internal
	// allocator + record-batch state — matches the upstream pattern in
	// metastore/iter.go's forEachIndexPointer.
	var reader indexpointers.Reader
	defer reader.Close()
	const batchSize = 1024
	scratch := make([]indexEntry, batchSize)

	var out []indexEntry
	for _, section := range obj.Sections().Filter(indexpointers.CheckSection) {
		// ToCs are written per tenant, so another tenant's section means the
		// metastore is corrupt. Compacting it could merge indexes across tenants.
		if section.Tenant != tenant {
			panic(fmt.Sprintf("ToC %s of tenant %q holds a section of tenant %q", tocPath, tenant, section.Tenant))
		}
		entries, err := readAllIndexPointers(ctx, &reader, scratch, section)
		if err != nil {
			return nil, fmt.Errorf("read indexpointers for tenant %s: %w", tenant, err)
		}
		out = append(out, entries...)
	}

	return out, nil
}

// readAllIndexPointers decodes every row of one indexpointers section into
// indexEntry values. The caller owns the Reader and scratch slice; both are
// reused across section iterations.
//
// Mirrors pkg/dataobj/metastore.forEachIndexPointer's structure but drops
// the user.ExtractOrgID tenant filter and the WhereTimeRangeOverlapsWith
// predicate — the compactor reads every row of the tenant's ToC.
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

const indexSectionReadConcurrency = 16

func indexSectionRefsFor(ctx context.Context, bucket objstore.Bucket, tenant string, entries []indexEntry) ([]v2.Section[indexSortKey], error) {
	type sectionRead struct {
		objectPath   string
		sectionIndex int64
		section      *dataobj.Section
	}

	var reads []sectionRead
	for _, entry := range entries {
		obj, err := dataobj.FromBucket(ctx, bucket, entry.Path, prefetchBytes)
		if err != nil {
			return nil, fmt.Errorf("open index tenant=%s index=%s: %w", tenant, entry.Path, err)
		}
		for sectionIndex, section := range obj.Sections() {
			if postings.CheckSection(section) && section.Tenant == tenant {
				reads = append(reads, sectionRead{
					objectPath:   entry.Path,
					sectionIndex: int64(sectionIndex),
					section:      section,
				})
			}
		}
	}

	results := make([]*v2.Section[indexSortKey], len(reads))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(indexSectionReadConcurrency)
	for i, read := range reads {
		group.Go(func() error {
			ref, err := indexSectionRef(groupCtx, read.objectPath, read.sectionIndex, read.section)
			if err != nil {
				return fmt.Errorf("read postings bounds tenant=%s index=%s section=%d: %w", tenant, read.objectPath, read.sectionIndex, err)
			}
			results[i] = ref
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}

	refs := make([]v2.Section[indexSortKey], 0, len(results))
	for _, ref := range results {
		if ref != nil {
			refs = append(refs, *ref)
		}
	}
	return refs, nil
}

func indexSectionRef(ctx context.Context, objectPath string, sectionIndex int64, section *dataobj.Section) (*v2.Section[indexSortKey], error) {
	postingsSection, err := postings.Open(ctx, section)
	if err != nil {
		return nil, fmt.Errorf("open postings section: %w", err)
	}
	columns, err := postingsBoundColumns(postingsSection)
	if err != nil {
		return nil, err
	}

	reader := postings.NewReader(postings.ReaderOptions{Columns: columns})
	if err := reader.Open(ctx); err != nil {
		return nil, fmt.Errorf("open postings reader: %w", err)
	}
	rows := postings.NewRowReader(ctx, reader)
	defer rows.Close()

	// Postings sections use CompareRows order. compareIndexSortKey is its prefix,
	// so the first and last rows bound the section for run planning.
	var first, last postings.Row
	hasRows := rows.Next()
	if hasRows {
		first = rows.At()
		last = first
	}
	for rows.Next() {
		last = rows.At()
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read postings rows: %w", err)
	}
	if !hasRows {
		return nil, nil
	}

	minKey, maxKey := indexKey(first), indexKey(last)
	if compareIndexSortKey(minKey, maxKey) > 0 {
		return nil, errors.New("postings section bounds are out of order")
	}
	return &v2.Section[indexSortKey]{
		Ref: &compactionv2pb.SectionRef{
			ObjectPath:   objectPath,
			SectionIndex: sectionIndex,
		},
		Min: minKey,
		Max: maxKey,
	}, nil
}

func postingsBoundColumns(section *postings.Section) ([]*postings.Column, error) {
	required := []postings.ColumnType{
		postings.ColumnTypeKind,
		postings.ColumnTypeColumnName,
		postings.ColumnTypeLabelValue,
		postings.ColumnTypeMinTimestamp,
		postings.ColumnTypeMaxTimestamp,
	}
	byType := make(map[postings.ColumnType]*postings.Column, len(section.Columns()))
	for _, column := range section.Columns() {
		byType[column.Type] = column
	}

	columns := make([]*postings.Column, 0, len(required))
	for _, columnType := range required {
		column, ok := byType[columnType]
		if !ok {
			return nil, fmt.Errorf("postings section has no %s column", columnType)
		}
		columns = append(columns, column)
	}
	return columns, nil
}

// indexLayout returns the stream layout of the data objects that obj indexes
// for tenant. ok is false if the index has no stats section for tenant. It
// returns an error that wraps errCorruptIndex if the sections of the index do
// not share one layout, or if the layout cannot be read.
func indexLayout(ctx context.Context, obj *dataobj.Object, tenant, idxPath string) (layout streams.Layout, ok bool, err error) {
	layout, ok, err = indexobj.TenantLayout(ctx, obj, tenant)
	if errors.Is(err, indexobj.ErrMixedLayouts) || errors.Is(err, indexobj.ErrInvalidLayout) {
		return streams.Layout{}, false, fmt.Errorf("%w: index %s: %w", errCorruptIndex, idxPath, err)
	}
	if err != nil {
		return streams.Layout{}, false, fmt.Errorf("read stream layout tenant=%s index=%s: %w", tenant, idxPath, err)
	}
	return layout, ok, nil
}

// indexLayoutAt opens the index at idxPath and returns its layout; see
// indexLayout.
func indexLayoutAt(ctx context.Context, bucket objstore.Bucket, tenant, idxPath string) (streams.Layout, bool, error) {
	obj, err := dataobj.FromBucket(ctx, bucket, idxPath, prefetchBytes)
	if err != nil {
		return streams.Layout{}, false, fmt.Errorf("open source index tenant=%s index=%s: %w", tenant, idxPath, err)
	}
	return indexLayout(ctx, obj, tenant, idxPath)
}

// logSectionRefsFor returns one bounded reference per log section indexed by
// idxPath. It returns an error that wraps errCorruptIndex if the sections of
// the index do not share one stream layout.
func logSectionRefsFor(ctx context.Context, bucket objstore.Bucket, tenant, idxPath string) ([]v2.Section[logSortPrefix], error) {
	obj, err := dataobj.FromBucket(ctx, bucket, idxPath, prefetchBytes)
	if err != nil {
		return nil, fmt.Errorf("open source index tenant=%s index=%s: %w", tenant, idxPath, err)
	}
	layout, ok, err := indexLayout(ctx, obj, tenant, idxPath)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	labelNames, err := schemaLabelNames(layout.SchemaLabels())
	if err != nil {
		return nil, fmt.Errorf("parse log sort schema tenant=%s index=%s: %w", tenant, idxPath, err)
	}

	type sectionID struct {
		path  string
		index int64
	}

	var (
		reader stats.Reader

		bySection = make(map[sectionID]*v2.Section[logSortPrefix])
	)
	defer reader.Close()

	const batchSize = 1024
	scratch := make([]stats.Stat, batchSize)
	for _, section := range obj.Sections().Filter(stats.CheckSection) {
		if section.Tenant != tenant {
			continue
		}

		statsSection, err := stats.Open(ctx, section)
		if err != nil {
			return nil, fmt.Errorf("open stats section tenant=%s index=%s: %w", tenant, idxPath, err)
		}

		reader.Reset(stats.ReaderOptions{Columns: statsSection.Columns()})
		if err := reader.Open(ctx); err != nil {
			return nil, fmt.Errorf("opening reader tenant=%s index=%s: %w", tenant, idxPath, err)
		}

		for {
			record, readErr := reader.Read(ctx, batchSize)
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return nil, fmt.Errorf("reading batch tenant=%s index=%s: %w", tenant, idxPath, readErr)
			}
			numRows := int(record.NumRows())
			if numRows == 0 && errors.Is(readErr, io.EOF) {
				break
			}

			rows := scratch[:numRows]
			n, err := stats.FromRecordBatch(record, rows)
			if err != nil {
				return nil, fmt.Errorf("decode stats batch tenant=%s index=%s: %w", tenant, idxPath, err)
			}

			for _, stat := range rows[:n] {
				var labels []string
				if len(labelNames) > 0 {
					labels = make([]string, len(labelNames))
					for i, name := range labelNames {
						labels[i] = stat.Labels[name]
					}
				}
				key := newLogSortPrefix(stat.ShardBucket, labels)

				id := sectionID{path: stat.ObjectPath, index: stat.SectionIndex}
				bounded, ok := bySection[id]
				if !ok {
					bySection[id] = &v2.Section[logSortPrefix]{
						Ref: &compactionv2pb.SectionRef{
							ObjectPath:       stat.ObjectPath,
							SectionIndex:     stat.SectionIndex,
							MinKey:           key.labels,
							MaxKey:           key.labels,
							MinTimestamp:     stat.MinTimestamp,
							MaxTimestamp:     stat.MaxTimestamp,
							UncompressedSize: stat.UncompressedSize,
						},
						Min: key,
						Max: key,
					}
					continue
				}

				if compareLogSortPrefix(key, bounded.Min) < 0 {
					bounded.Min = key
					bounded.Ref.MinKey = key.labels
				}
				if compareLogSortPrefix(key, bounded.Max) > 0 {
					bounded.Max = key
					bounded.Ref.MaxKey = key.labels
				}
				bounded.Ref.MinTimestamp = min(bounded.Ref.MinTimestamp, stat.MinTimestamp)
				bounded.Ref.MaxTimestamp = max(bounded.Ref.MaxTimestamp, stat.MaxTimestamp)
				bounded.Ref.UncompressedSize += stat.UncompressedSize
			}

			if errors.Is(readErr, io.EOF) {
				break
			}
		}
	}

	refs := make([]v2.Section[logSortPrefix], 0, len(bySection))
	for _, ref := range bySection {
		refs = append(refs, *ref)
	}
	slices.SortFunc(refs, func(a, b v2.Section[logSortPrefix]) int {
		if n := strings.Compare(a.Ref.ObjectPath, b.Ref.ObjectPath); n != 0 {
			return n
		}
		return cmp.Compare(a.Ref.SectionIndex, b.Ref.SectionIndex)
	})
	return refs, nil
}

// schemaLabelNames returns the label names of the fully-qualified schema
// labels, in order.
func schemaLabelNames(schemaLabels []string) ([]string, error) {
	var labelNames []string
	for _, entry := range schemaLabels {
		typ, name, ok := strings.Cut(entry, ":")
		if !ok || typ != "label" || name == "" {
			return nil, fmt.Errorf("invalid log sort key %q", entry)
		}
		labelNames = append(labelNames, name)
	}
	return labelNames, nil
}
