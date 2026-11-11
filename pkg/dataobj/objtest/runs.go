package objtest

import (
	"context"
	"slices"
	"strings"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
)

// RunFile is one index object of a run that [Builder.SplitIndexesIntoRuns]
// wrote.
type RunFile struct {
	// Path is the key of the index object, under the index storage prefix.
	Path string
	// PostingKeys bounds the posting keys of the index object.
	PostingKeys postings.KeyRange
}

// SplitIndexesIntoRuns replaces every index object with a run of at most parts
// index objects, split by posting key as the compactor will split them. It
// returns the index objects of every run. Call it after [Builder.Close].
//
// Each index object of a run holds the postings of a contiguous range of
// posting keys, and never splits the rows of one key. The first index object
// also holds every stats row. The ToC records, for each index object, the run
// ID, the posting key range, the shard layout of the original index, and the
// time range of the original ToC entry.
//
// The ToC writer can't record run IDs, so SplitIndexesIntoRuns rewrites each
// ToC of the indexes directly.
func (b *Builder) SplitIndexesIntoRuns(parts int) []RunFile {
	require.True(b.t, b.closed, "call Close before SplitIndexesIntoRuns: it splits the indexes that Close writes")
	require.Positive(b.t, parts)
	ctx := b.t.Context()

	var tocPaths []string
	require.NoError(b.t, b.indexBucket.Iter(ctx, metastore.TocPrefix, func(name string) error {
		if strings.HasSuffix(name, ".toc") {
			tocPaths = append(tocPaths, name)
		}
		return nil
	}, objstore.WithRecursiveIter()))

	// An index object can sit in the ToCs of several windows. Split it once.
	split := make(map[string][]RunFile)
	var files []RunFile
	for _, tocPath := range tocPaths {
		var pointers []indexpointers.IndexPointer
		for res := range indexpointers.Iter(ctx, b.readObject(ctx, b.indexBucket, tocPath)) {
			pointer, err := res.Value()
			require.NoError(b.t, err)
			pointers = append(pointers, pointer.IndexPointer)
		}

		toc, err := indexobj.NewBuilder(Tenant, metastore.DefaultTocBuilderConfig, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(b.t, err)
		for i, pointer := range pointers {
			original := b.readObject(ctx, b.indexBucket, pointer.Path)
			shardBuckets, err := indexobj.TenantShardBuckets(ctx, original, Tenant)
			require.NoError(b.t, err)

			runFiles, ok := split[pointer.Path]
			if !ok {
				runFiles = b.splitIndex(ctx, original, parts)
				split[pointer.Path] = runFiles
				files = append(files, runFiles...)
			}
			for _, file := range runFiles {
				require.NoError(b.t, toc.AppendIndexPointer(indexpointers.IndexPointer{
					Path:         file.Path,
					StartTs:      pointer.StartTs,
					EndTs:        pointer.EndTs,
					RunID:        indexpointers.RunID(i + 1),
					PostingKeys:  file.PostingKeys,
					ShardBuckets: shardBuckets,
				}))
			}
		}
		b.uploadObject(ctx, toc, tocPath)
	}
	return files
}

// splitIndex writes the run of at most parts index objects that replaces
// original, and returns its index objects.
func (b *Builder) splitIndex(ctx context.Context, original *dataobj.Object, parts int) []RunFile {
	var rows []postings.Row
	for _, section := range original.Sections().Filter(postings.CheckSection) {
		if section.Tenant == Tenant {
			rows = append(rows, readRows(b.t, ctx, section, openPostingsRows)...)
		}
	}
	var statRows []stats.Stat
	for _, section := range original.Sections().Filter(stats.CheckSection) {
		if section.Tenant == Tenant {
			statRows = append(statRows, readRows(b.t, ctx, section, openStatsRows)...)
		}
	}
	require.NotEmpty(b.t, rows, "an index object without postings can't be split by posting key")
	slices.SortStableFunc(rows, postings.CompareRows)

	layout, _, err := indexobj.TenantLayout(ctx, original, Tenant)
	require.NoError(b.t, err)

	var files []RunFile
	for i, part := range splitByKey(rows, parts) {
		builder, err := indexobj.NewMergeBuilder(b.builderConfig, nil, Tenant, layout)
		require.NoError(b.t, err)
		if i == 0 {
			for _, stat := range statRows {
				require.NoError(b.t, builder.AppendStat(Tenant, stat))
			}
		}
		for _, row := range part {
			if row.Kind == postings.KindBloom {
				require.NoError(b.t, builder.AppendPostingsBloomEntry(Tenant, row.BloomEntry()))
			} else {
				require.NoError(b.t, builder.AppendPostingsLabelEntry(Tenant, row.LabelEntry()))
			}
		}

		obj, closer, err := builder.Flush()
		require.NoError(b.t, err)
		path, err := index.ObjectKey(ctx, obj)
		require.NoError(b.t, err)
		reader, err := obj.Reader(ctx)
		require.NoError(b.t, err)
		require.NoError(b.t, b.indexBucket.Upload(ctx, path, reader))
		require.NoError(b.t, reader.Close())
		require.NoError(b.t, closer.Close())

		keys, err := postings.NewKeyRange(part[0].Key(), part[len(part)-1].Key())
		require.NoError(b.t, err)
		files = append(files, RunFile{Path: path, PostingKeys: keys})
	}
	return files
}

// splitByKey cuts rows, which are in key order, into at most parts groups of
// whole posting keys. The groups hold about as many keys each.
func splitByKey(rows []postings.Row, parts int) [][]postings.Row {
	var keyStarts []int
	for i, row := range rows {
		if i == 0 || row.Key() != rows[i-1].Key() {
			keyStarts = append(keyStarts, i)
		}
	}
	parts = min(parts, len(keyStarts))

	out := make([][]postings.Row, 0, parts)
	for part := range parts {
		from := keyStarts[part*len(keyStarts)/parts]
		to := len(rows)
		if part+1 < parts {
			to = keyStarts[(part+1)*len(keyStarts)/parts]
		}
		out = append(out, rows[from:to])
	}
	return out
}

// rowIterator is the row iterator of a section.
type rowIterator[R any] interface {
	Next() bool
	At() R
	Err() error
	Close() error
}

func openPostingsRows(ctx context.Context, section *dataobj.Section) (rowIterator[postings.Row], error) {
	opened, err := postings.Open(ctx, section)
	if err != nil {
		return nil, err
	}
	reader := postings.NewReader(postings.ReaderOptions{Columns: opened.Columns()})
	if err := reader.Open(ctx); err != nil {
		return nil, err
	}
	return postings.NewRowReader(ctx, reader), nil
}

func openStatsRows(ctx context.Context, section *dataobj.Section) (rowIterator[stats.Stat], error) {
	opened, err := stats.Open(ctx, section)
	if err != nil {
		return nil, err
	}
	return stats.NewRowReader(ctx, opened), nil
}

// readRows returns every row of section.
func readRows[R any](t require.TestingT, ctx context.Context, section *dataobj.Section, open func(context.Context, *dataobj.Section) (rowIterator[R], error)) []R {
	rows, err := open(ctx, section)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	var out []R
	for rows.Next() {
		out = append(out, rows.At())
	}
	require.NoError(t, rows.Err())
	return out
}

// readObject reads the object at path in bucket.
func (b *Builder) readObject(ctx context.Context, bucket objstore.Bucket, path string) *dataobj.Object {
	obj, err := dataobj.FromBucket(ctx, bucket, path, 0)
	require.NoError(b.t, err, "reading %s", path)
	return obj
}

// uploadObject flushes builder and stores its object in the index bucket at
// path.
func (b *Builder) uploadObject(ctx context.Context, builder *indexobj.Builder, path string) {
	obj, closer, err := builder.Flush()
	require.NoError(b.t, err)
	defer func() { require.NoError(b.t, closer.Close()) }()

	reader, err := obj.Reader(ctx)
	require.NoError(b.t, err)
	defer func() { require.NoError(b.t, reader.Close()) }()
	require.NoError(b.t, b.indexBucket.Upload(ctx, path, reader), "uploading %s", path)
}
