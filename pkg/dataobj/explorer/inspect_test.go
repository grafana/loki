package explorer

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
)

// buildIndexPointersObject builds a dataobj with one indexpointers section and
// uploads it to bucket at path, so inspectFile can read it back via FromBucket.
// Mirrors pkg/dataobj/index/builder_test.go:472-481.
func buildIndexPointersObject(ctx context.Context, t *testing.T, bucket objstore.Bucket, path, tenant, ptrPath string, start, end time.Time) {
	t.Helper()

	ib := indexpointers.NewBuilder(nil, 1024, 0)
	ib.SetTenant(tenant)
	ib.Append(ptrPath, start, end)

	b := dataobj.NewBuilder(nil)
	require.NoError(t, b.Append(ib))

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	defer closer.Close()

	reader, err := obj.Reader(ctx)
	require.NoError(t, err)
	defer reader.Close()

	require.NoError(t, bucket.Upload(ctx, path, reader))
}

func TestInspectFile_IndexPointers(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	path := "index/v0/tocs/2026-06-03T12_00_00Z.toc"

	// Use a non-UTC location so the test proves UTC normalization happens.
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	start := time.Date(2026, 6, 3, 12, 0, 0, 0, loc)
	end := time.Date(2026, 6, 3, 13, 0, 0, 0, loc)
	wantPath := "index/v0/indexes/abc123.idx"

	buildIndexPointersObject(ctx, t, bucket, path, "tenantID", wantPath, start, end)

	md := inspectFile(ctx, log.NewNopLogger(), bucket, path)
	require.Empty(t, md.Error)

	var found *SectionMetadata
	for i := range md.Sections {
		if len(md.Sections[i].IndexPointers) > 0 {
			found = &md.Sections[i]
			break
		}
	}
	require.NotNil(t, found, "expected a section with decoded IndexPointers")
	require.Len(t, found.IndexPointers, 1)

	row := found.IndexPointers[0]
	require.Equal(t, wantPath, row.Path)
	require.Equal(t, time.UTC, row.StartTs.Location())
	require.Equal(t, time.UTC, row.EndTs.Location())
	require.True(t, start.Equal(row.StartTs))
	require.True(t, end.Equal(row.EndTs))
}
