package metastore

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
)

func TestIndexSectionsReader_BloomDenyList(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)

	// Build one index with two data object sections. Both have the stream
	// app=foo, the structured metadata column observed_timestamp with a
	// unique value per section, and the column traceID.
	buildIndex := func(t *testing.T, denyList []string) (*ObjectMetastore, string) {
		t.Helper()
		builder, err := indexobj.NewBuilder(tenantID, logsobj.BuilderBaseConfig{
			TargetPageSize:          1024 * 1024,
			TargetObjectSize:        10 * 1024 * 1024,
			TargetSectionSize:       1024 * 1024,
			BufferSize:              1024 * 1024,
			SectionStripeMergeLimit: 2,
		}, nil, indexobj.NewBuilderMetrics(nil),
			denyList)
		require.NoError(t, err)

		for section, observed := range []string{"1700000000000000001", "1700000000000000002"} {
			s := int64(section)
			observeLabelPostings(builder, "test-path", s, 1, labels.FromStrings("app", "foo"), now.Add(-3*time.Hour), 5)
			observeBloomPostings(t, builder, "test-path", s, 1, now.Add(-3*time.Hour), "observed_timestamp", observed)
			observeBloomPostings(t, builder, "test-path", s, 1, now.Add(-3*time.Hour), "traceID", []string{"abcd", "efgh"}[section])
		}

		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { _ = closer.Close() })
		return uploadIndexObject(t, obj)
	}

	sectionsFor := func(t *testing.T, mstore *ObjectMetastore, path string, predicates ...*labels.Matcher) []int64 {
		t.Helper()
		resp, err := mstore.IndexSectionsReader(ctx, IndexSectionsReaderRequest{
			Indexes: []IndexEntry{{Path: path}},
			SectionsRequest: SectionsRequest{
				Start:      now.Add(-4 * time.Hour),
				End:        now.Add(time.Hour),
				Matchers:   []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")},
				Predicates: predicates,
			},
		})
		require.NoError(t, err)
		t.Cleanup(resp.Reader.Close)
		require.NoError(t, resp.Reader.Open(ctx))

		var sections []int64
		for {
			rec, err := resp.Reader.Read(ctx)
			if err != nil {
				break
			}
			col := rec.Schema().FieldIndices("section.int64")
			require.Len(t, col, 1)
			ids := rec.Column(col[0]).(*array.Int64)
			for i := 0; i < int(rec.NumRows()); i++ {
				sections = append(sections, ids.Value(i))
			}
		}
		slices.Sort(sections)
		return slices.Compact(sections)
	}

	observed := func(value string) *labels.Matcher {
		return labels.MustNewMatcher(labels.MatchEqual, "observed_timestamp", value)
	}
	traceID := func(value string) *labels.Matcher {
		return labels.MustNewMatcher(labels.MatchEqual, "traceID", value)
	}

	t.Run("does not prune sections on a denied column, so no matching line is lost", func(t *testing.T) {
		mstore, path := buildIndex(t, []string{"observed_timestamp"})

		require.Equal(t, []int64{0}, sectionsFor(t, mstore, path, traceID("abcd")), "pruning on other columns must stay on")
		require.Equal(t, []int64{0, 1}, sectionsFor(t, mstore, path, observed("1700000000000000001")))
		require.Equal(t, []int64{0, 1}, sectionsFor(t, mstore, path, observed("1700000000000000002")))
		require.Equal(t, []int64{0, 1}, sectionsFor(t, mstore, path, observed("1700000000000000003")))
	})

	t.Run("keeps pruning other columns while a denied column is in the same query", func(t *testing.T) {
		mstore, path := buildIndex(t, []string{"observed_timestamp"})

		require.Equal(t, []int64{1}, sectionsFor(t, mstore, path, observed("1700000000000000001"), traceID("efgh")))
		require.Empty(t, sectionsFor(t, mstore, path, observed("1700000000000000001"), traceID("not-in-bloom")))
	})

	t.Run("prunes on the column when the deny list is empty", func(t *testing.T) {
		mstore, path := buildIndex(t, nil)

		require.Equal(t, []int64{0}, sectionsFor(t, mstore, path, observed("1700000000000000001")))
		require.Empty(t, sectionsFor(t, mstore, path, observed("1700000000000000003")))
	})
}
