package compactor

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/util"
)

// prefetchBytes is the prefetch window that each opened index object reads
// from its start and keeps. Each open object holds a buffer of this full size,
// even when the object is smaller. The memory of a phase is about
// prefetchBytes times its open objects.
const prefetchBytes = 1 * 1024 * 1024

// errCorruptIndex reports an index or index run that breaks an invariant of
// its format. Examples are a wrong run length, stream layouts that differ or
// cannot be read, and postings sections without bounds. No retry can fix it,
// so compaction skips it.
var errCorruptIndex = errors.New("corrupt index")

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

// loadTenantIndexes returns the entries of the tenant's ToC for the window.
// Entry order reflects ToC enumeration order and is not part of the contract
// — callers must not rely on it for correctness.
func loadTenantIndexes(ctx context.Context, bucket objstore.Bucket, window time.Time, tenant string) ([]indexpointers.IndexPointer, error) {
	tocPath := metastore.TableOfContentsPath(tenant, window.UTC().Truncate(metastore.MetastoreWindowSize))

	obj, err := metastore.OpenTableOfContents(ctx, bucket, tocPath)
	if err != nil {
		return nil, err // includes IsObjNotFoundErr — caller checks
	}

	var out []indexpointers.IndexPointer
	for res := range indexpointers.Iter(ctx, obj) {
		pointer, err := res.Value()
		if err != nil {
			return nil, fmt.Errorf("read indexpointers of ToC %s: %w", tocPath, err)
		}
		// ToCs are written per tenant, so another tenant's section means the
		// metastore is corrupt. Compacting it could merge indexes across tenants.
		if pointer.Tenant != tenant {
			panic(fmt.Sprintf("ToC %s of tenant %q holds a section of tenant %q", tocPath, tenant, pointer.Tenant))
		}
		out = append(out, pointer.IndexPointer)
	}
	return out, nil
}

// indexReadConcurrency is the number of index runs that IndexMerge planning
// reads from the bucket at once.
const indexReadConcurrency = 16

// indexRunRead is what IndexMerge planning needs from the index objects of one
// index run.
type indexRunRead struct {
	run indexRun

	// layout is the stream layout of run. It has no meaning when hasLayout is
	// false, because run has no stats sections of the tenant.
	layout    streams.Layout
	hasLayout bool

	// sections has one bounded reference per postings section of the tenant
	// that has rows. Every reference has the planning unit of run, so
	// planning puts every index object of run in one task.
	sections []v2.Section[indexSortKey]
}

// readIndexRun opens the index objects of run once, and reads both the stream
// layout and the postings bounds of run for tenant. It keeps no reference to
// the objects or to their decoded pages, so the garbage collector can free
// them after it returns.
//
// It returns an error that wraps errCorruptIndex if an index object of run:
//   - records a run length other than the number of index objects of run,
//   - has stats sections that do not share one layout,
//   - has a layout that cannot be read,
//   - records no layout while another index object of run does,
//   - records another layout than another index object of run,
//   - has a postings section without a bound column, or with bounds out of
//     order,
//   - has no postings rows of tenant, while the ToC records it in a run.
func readIndexRun(ctx context.Context, bucket objstore.Bucket, tenant string, run indexRun) (indexRunRead, error) {
	opened, err := openRunObjects(ctx, bucket, tenant, run)
	if err != nil {
		return indexRunRead{}, err
	}
	layout, hasLayout, err := indexLayoutOfRun(ctx, tenant, opened)
	if err != nil {
		return indexRunRead{}, err
	}

	reader := newRunBoundsReader(tenant, run)
	var sections []v2.Section[indexSortKey]
	for _, index := range opened {
		refs, err := reader.objectSectionRefs(ctx, index)
		if err != nil {
			return indexRunRead{}, err
		}
		// Planning places an index object by its postings rows, so a task
		// would leave out an object without them. The ToC writer refuses a
		// swap that removes only part of a run.
		if len(refs) == 0 && run.entries[0].RunID != 0 {
			return indexRunRead{}, fmt.Errorf("%w: index %s has no postings rows", errCorruptIndex, index.path)
		}
		sections = append(sections, refs...)
	}
	return indexRunRead{run: run, layout: layout, hasLayout: hasLayout, sections: sections}, nil
}

// runBoundsReader reads the postings bounds of the index objects of one index
// run. It is not safe for concurrent use.
//
// The strings of a decoded postings row point into the buffers of its page.
// The reader interns the strings of each bound, so a bound does not keep a
// page alive after the read. Planning holds the bounds of every run of a
// window at once.
type runBoundsReader struct {
	tenant       string
	planningUnit string
	interned     map[string]string
}

func newRunBoundsReader(tenant string, run indexRun) *runBoundsReader {
	return &runBoundsReader{tenant: tenant, planningUnit: run.planningUnit(), interned: make(map[string]string)}
}

// objectSectionRefs returns one bounded reference per postings section of the
// tenant in index that has rows, in section order.
func (r *runBoundsReader) objectSectionRefs(ctx context.Context, index openedIndex) ([]v2.Section[indexSortKey], error) {
	var refs []v2.Section[indexSortKey]
	for sectionIndex, section := range index.obj.Sections() {
		if !postings.CheckSection(section) || section.Tenant != r.tenant {
			continue
		}
		ref, hasRows, err := r.sectionRef(ctx, index.path, int64(sectionIndex), section)
		if err != nil {
			return nil, fmt.Errorf("read postings bounds tenant=%s index=%s section=%d: %w", r.tenant, index.path, sectionIndex, err)
		}
		if hasRows {
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

// sectionRef returns the bounded reference of section. hasRows is false when
// section has no postings rows.
//
// It returns an error that wraps errCorruptIndex if section has no bound
// column or its bounds are out of order. No retry can fix either case.
func (r *runBoundsReader) sectionRef(ctx context.Context, objectPath string, sectionIndex int64, section *dataobj.Section) (ref v2.Section[indexSortKey], hasRows bool, err error) {
	postingsSection, err := postings.Open(ctx, section)
	if err != nil {
		return v2.Section[indexSortKey]{}, false, fmt.Errorf("open postings section: %w", err)
	}
	columns, err := postingsBoundColumns(postingsSection)
	if err != nil {
		return v2.Section[indexSortKey]{}, false, fmt.Errorf("%w: %w", errCorruptIndex, err)
	}

	reader := postings.NewReader(postings.ReaderOptions{Columns: columns})
	if err := reader.Open(ctx); err != nil {
		return v2.Section[indexSortKey]{}, false, fmt.Errorf("open postings reader: %w", err)
	}
	rows := postings.NewRowReader(ctx, reader)
	defer rows.Close()

	// Postings sections use CompareRows order. compareIndexSortKey is its prefix,
	// so the first and last rows bound the section for run planning.
	var first, last postings.Row
	samples := keySamples{interned: r.interned}
	hasRows = rows.Next()
	if hasRows {
		first = rows.At()
		last = first
		samples.add(first)
	}
	for rows.Next() {
		last = rows.At()
		samples.add(last)
	}
	if err := rows.Err(); err != nil {
		return v2.Section[indexSortKey]{}, false, fmt.Errorf("read postings rows: %w", err)
	}
	if !hasRows {
		return v2.Section[indexSortKey]{}, false, nil
	}

	minKey, maxKey := r.key(first), r.key(last)
	if compareIndexSortKey(minKey, maxKey) > 0 {
		return v2.Section[indexSortKey]{}, false, fmt.Errorf("%w: postings section bounds are out of order", errCorruptIndex)
	}
	return v2.Section[indexSortKey]{
		Ref: &compactionv2pb.SectionRef{
			ObjectPath:   objectPath,
			SectionIndex: sectionIndex,
		},
		Min:          minKey,
		Max:          maxKey,
		Keys:         samples.sizes(postingsSection.UncompressedSize()),
		PlanningUnit: r.planningUnit,
	}, true, nil
}

// key returns the sort key of row, with interned strings.
func (r *runBoundsReader) key(row postings.Row) indexSortKey {
	return indexSortKey{
		kind:         row.Kind,
		columnName:   intern(r.interned, row.ColumnName),
		labelValue:   intern(r.interned, row.LabelValue),
		minTimestamp: row.MinTimestamp,
		maxTimestamp: row.MaxTimestamp,
	}
}

// indexKeySampleRows is the number of postings rows that one key sample of a
// section stands for. A larger value saves planner memory, and a smaller one
// lets a split of an index merge cut closer to its budget.
const indexKeySampleRows = 4096

// keySamples samples the posting keys of the rows of one postings section, in
// row order. It keeps the key of the first row of each group of
// indexKeySampleRows rows, so a split of an index merge can cut at real keys
// without holding every key in memory.
type keySamples struct {
	samples   []keySample
	totalRows uint64

	// interned holds the strings of the keys, shared with the bounds of the
	// run.
	interned map[string]string
}

// keySample is the key of the first row of a group of rows.
type keySample struct {
	key indexSortKey

	// rows is the number of rows in the group.
	rows uint64
}

// add samples row if it starts a new group of indexKeySampleRows rows, and
// counts it otherwise.
func (s *keySamples) add(row postings.Row) {
	s.totalRows++
	if n := len(s.samples); n > 0 && s.samples[n-1].rows < indexKeySampleRows {
		s.samples[n-1].rows++
		return
	}
	// The decoded strings of row point into the buffers of a record batch, so
	// the sample keeps interned copies.
	s.samples = append(s.samples, keySample{
		key: indexSortKey{
			kind:       row.Kind,
			columnName: intern(s.interned, row.ColumnName),
			labelValue: intern(s.interned, row.LabelValue),
		},
		rows: 1,
	})
}

// sizes returns the samples with their share of sectionBytes, the
// uncompressed size of the section. Each sample gets a share by its number of
// rows.
func (s keySamples) sizes(sectionBytes uint64) []v2.KeySize[indexSortKey] {
	if s.totalRows == 0 {
		return nil
	}
	sizes := make([]v2.KeySize[indexSortKey], len(s.samples))
	for i, sample := range s.samples {
		sizes[i] = v2.KeySize[indexSortKey]{Key: sample.key, UncompressedSize: sectionBytes * sample.rows / s.totalRows}
	}
	return sizes
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

// openedIndex pairs an opened index object with its path, which errors name.
type openedIndex struct {
	path string
	obj  *dataobj.Object
}

// openRunObjects opens the index objects of run, in the order of run. It
// returns an error that wraps errCorruptIndex if an index object of run
// records a run length other than the number of index objects of run.
//
// Each returned object keeps its prefetch buffer of prefetchBytes, so
// the memory grows with the number of index objects in run.
func openRunObjects(ctx context.Context, bucket objstore.Bucket, tenant string, run indexRun) ([]openedIndex, error) {
	if err := run.checkLength(); err != nil {
		return nil, err
	}
	opened := make([]openedIndex, 0, len(run.entries))
	for _, entry := range run.entries {
		obj, err := dataobj.FromBucket(ctx, bucket, entry.Path, prefetchBytes)
		if err != nil {
			return nil, fmt.Errorf("open source index tenant=%s index=%s: %w", tenant, entry.Path, err)
		}
		opened = append(opened, openedIndex{path: entry.Path, obj: obj})
	}
	return opened, nil
}

// indexLayoutOfRun returns the stream layout that the opened index objects of
// a run record for tenant. hasLayout is false if no index object has a stats
// section of tenant.
//
// Every index object of a run records the layout of the run in a stats
// section, even if the section holds no rows. indexLayoutOfRun returns an
// error that wraps errCorruptIndex if an index object is corrupt, records no
// layout while another index object does, or records another layout.
func indexLayoutOfRun(ctx context.Context, tenant string, run []openedIndex) (layout streams.Layout, hasLayout bool, err error) {
	var (
		source        string
		withoutLayout string
	)
	for _, index := range run {
		objLayout, objHasLayout, err := indexLayout(ctx, index.obj, tenant, index.path)
		if err != nil {
			return streams.Layout{}, false, err
		}
		if !objHasLayout {
			withoutLayout = index.path
			continue
		}
		if hasLayout && !objLayout.Equal(layout) {
			return streams.Layout{}, false, fmt.Errorf("%w: index %s has stream layout %v, but index %s of the same run has %v", errCorruptIndex, index.path, objLayout, source, layout)
		}
		layout, hasLayout, source = objLayout, true, index.path
	}
	if hasLayout && withoutLayout != "" {
		return streams.Layout{}, false, fmt.Errorf("%w: index %s records no stream layout, but index %s of the same run does", errCorruptIndex, withoutLayout, source)
	}
	return layout, hasLayout, nil
}

// logSectionRefsFor returns one bounded reference per log section in the
// stats rows of run, and the shard buckets of run.
//
// It returns no references and the zero ShardBuckets if run has no stats
// section for tenant.
//
// It returns an error that wraps errCorruptIndex if an index object of run:
//   - records a run length other than the number of index objects of run,
//   - has stats sections that do not share one layout,
//   - has a layout that cannot be read,
//   - records no layout while another index object of run does,
//   - records another layout than another index object of run.
func logSectionRefsFor(ctx context.Context, bucket objstore.Bucket, tenant string, run indexRun) ([]v2.Section[logSortPrefix], indexpointers.ShardBuckets, error) {
	opened, err := openRunObjects(ctx, bucket, tenant, run)
	if err != nil {
		return nil, indexpointers.ShardBuckets{}, err
	}
	layout, ok, err := indexLayoutOfRun(ctx, tenant, opened)
	if err != nil {
		return nil, indexpointers.ShardBuckets{}, err
	}
	if !ok {
		return nil, indexpointers.ShardBuckets{}, nil
	}
	labelNames, err := schemaLabelNames(layout.SchemaLabels())
	if err != nil {
		return nil, indexpointers.ShardBuckets{}, fmt.Errorf("parse log sort schema tenant=%s index=%s: %w", tenant, run, err)
	}

	folder := newLogSectionFolder(labelNames)
	for _, index := range opened {
		for _, section := range index.obj.Sections().Filter(stats.CheckSection) {
			if section.Tenant != tenant {
				continue
			}
			if err := folder.addSection(ctx, section); err != nil {
				return nil, indexpointers.ShardBuckets{}, fmt.Errorf("read stats section tenant=%s index=%s: %w", tenant, index.path, err)
			}
		}
	}
	return folder.refs(), indexpointers.NewShardBuckets(layout.ShardLayout()), nil
}

// logSectionID identifies one section of a logs object.
type logSectionID struct {
	path  string
	index int64
}

// logSectionFolder folds stats rows into one bounded reference per log
// section. The stats rows of one log section can come from several stats
// sections and index objects.
type logSectionFolder struct {
	// labelNames are the label names of the sort schema, in schema order.
	labelNames []string
	bySection  map[logSectionID]*v2.Section[logSortPrefix]
	interned   map[string]string
}

func newLogSectionFolder(labelNames []string) *logSectionFolder {
	return &logSectionFolder{
		labelNames: labelNames,
		bySection:  make(map[logSectionID]*v2.Section[logSortPrefix]),
		interned:   make(map[string]string),
	}
}

// addSection folds every row of section, which must be a stats section.
func (f *logSectionFolder) addSection(ctx context.Context, section *dataobj.Section) (err error) {
	statsSection, err := stats.Open(ctx, section)
	if err != nil {
		return fmt.Errorf("open stats section: %w", err)
	}
	reader := stats.NewRowReader(ctx, statsSection)
	defer util.CloseAndHandleError(reader, &err)
	for reader.Next() {
		f.add(reader.At())
	}
	return reader.Err()
}

// add folds stat into the reference of its log section.
//
// The decoded strings of stat point into the buffers of a record batch. The
// plans and the bySection keys keep the label values and object paths for the
// whole LogMerge phase. Without a copy, they keep every batch in memory.
// Interning copies each distinct value once.
func (f *logSectionFolder) add(stat stats.Stat) {
	var labels []string
	if len(f.labelNames) > 0 {
		labels = make([]string, len(f.labelNames))
		for i, name := range f.labelNames {
			labels[i] = intern(f.interned, stat.Labels[name])
		}
	}
	key := newLogSortPrefix(stat.ShardBucket, labels)
	keySize := v2.KeySize[logSortPrefix]{Key: key, UncompressedSize: uint64(stat.UncompressedSize)}

	path := intern(f.interned, stat.ObjectPath)
	id := logSectionID{path: path, index: stat.SectionIndex}
	bounded, ok := f.bySection[id]
	if !ok {
		// A log merge rewrites whole logs objects, so each logs object is a
		// planning unit.
		f.bySection[id] = &v2.Section[logSortPrefix]{
			PlanningUnit: path,
			Ref: &compactionv2pb.SectionRef{
				ObjectPath:       path,
				SectionIndex:     stat.SectionIndex,
				MinKey:           key.labels,
				MaxKey:           key.labels,
				MinTimestamp:     stat.MinTimestamp,
				MaxTimestamp:     stat.MaxTimestamp,
				UncompressedSize: stat.UncompressedSize,
			},
			Min:  key,
			Max:  key,
			Keys: []v2.KeySize[logSortPrefix]{keySize},
		}
		return
	}
	bounded.Keys = append(bounded.Keys, keySize)

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

// refs returns the folded references, sorted by log object path and section
// index.
func (f *logSectionFolder) refs() []v2.Section[logSortPrefix] {
	refs := make([]v2.Section[logSortPrefix], 0, len(f.bySection))
	for _, ref := range f.bySection {
		refs = append(refs, *ref)
	}
	slices.SortFunc(refs, func(a, b v2.Section[logSortPrefix]) int {
		if n := strings.Compare(a.Ref.ObjectPath, b.Ref.ObjectPath); n != 0 {
			return n
		}
		return cmp.Compare(a.Ref.SectionIndex, b.Ref.SectionIndex)
	})
	return refs
}

// intern returns the copy of s in interned. It adds a copy of s on the
// first call for each distinct value.
func intern(interned map[string]string, s string) string {
	if v, ok := interned[s]; ok {
		return v
	}
	v := strings.Clone(s)
	interned[v] = v
	return v
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
