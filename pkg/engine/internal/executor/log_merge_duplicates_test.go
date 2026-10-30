package executor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v4/pkg/dataobj"
	"github.com/grafana/loki/v4/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v4/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v4/pkg/engine/internal/planner/physical"
)

// mergedRecord is one record read back from a compacted output object. It has
// every field the duplicate check compares, so two equal mergedRecords are an
// exact duplicate pair.
type mergedRecord struct {
	stream   string
	ts       time.Time
	line     string
	metadata string
}

// readMergedRecords returns every record of every compacted log object in
// bucket for tenant. It reads only the objects/tenants/ prefix, so source
// objects uploaded under other paths are not included.
func readMergedRecords(ctx context.Context, t *testing.T, bucket objstore.Bucket, tenant string) []mergedRecord {
	t.Helper()

	var paths []string
	require.NoError(t, bucket.Iter(ctx, "objects/tenants/"+tenant+"/", func(name string) error {
		if !strings.HasSuffix(name, "/") {
			paths = append(paths, name)
		}
		return nil
	}, objstore.WithRecursiveIter()))

	var out []mergedRecord
	for _, path := range paths {
		obj, err := dataobj.FromBucket(ctx, bucket, path, 0)
		require.NoError(t, err)

		streamLabels := make(map[int64]string)
		for _, sec := range obj.Sections().Filter(streams.CheckSection) {
			if sec.Tenant != tenant {
				continue
			}
			ss, err := streams.Open(ctx, sec)
			require.NoError(t, err)
			for res := range streams.IterSection(ctx, ss) {
				stream, err := res.Value()
				require.NoError(t, err)
				streamLabels[stream.ID] = stream.Labels.String()
			}
		}

		for _, sec := range obj.Sections().Filter(logs.CheckSection) {
			if sec.Tenant != tenant {
				continue
			}
			ls, err := logs.Open(ctx, sec)
			require.NoError(t, err)
			for res := range logs.IterSection(ctx, ls) {
				rec, err := res.Value()
				require.NoError(t, err)
				out = append(out, mergedRecord{
					stream:   streamLabels[rec.StreamID],
					ts:       rec.Timestamp,
					line:     string(rec.Line),
					metadata: rec.Metadata.String(),
				})
			}
		}
	}
	return out
}

// countExactDuplicates returns the number of records in recs that repeat an
// earlier record.
func countExactDuplicates(recs []mergedRecord) int {
	seen := make(map[mergedRecord]bool, len(recs))
	var duplicates int
	for _, rec := range recs {
		if seen[rec] {
			duplicates++
		}
		seen[rec] = true
	}
	return duplicates
}

func TestDoLogObjectMerge_KeepsAndCountsDuplicateRecords(t *testing.T) {
	const tenant = "T"
	sortSchema := []string{"label:app"}
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	entry := func(traceID string) push.Entry {
		return push.Entry{
			Timestamp:          ts,
			Line:               "line",
			StructuredMetadata: push.LabelsAdapter{{Name: "trace_id", Value: traceID}},
		}
	}
	want := func(traceID string) mergedRecord {
		return mergedRecord{stream: `{app="a"}`, ts: ts, line: "line", metadata: `{trace_id="` + traceID + `"}`}
	}

	// merge compacts objA and objB, which hold one stream each with the same
	// labels. It returns the duplicate count the task reported and the records
	// the task wrote.
	merge := func(t *testing.T, objA, objB []push.Entry) (reported int, output []mergedRecord) {
		t.Helper()

		ctx := context.Background()
		dataBucket := objstore.NewInMemBucket()
		buildSourceLogObject(t, dataBucket, "objA", sortSchema, map[string][]testStream{
			tenant: {{labels: `{app="a"}`, entries: objA}},
		})
		buildSourceLogObject(t, dataBucket, "objB", sortSchema, map[string][]testStream{
			tenant: {{labels: `{app="a"}`, entries: objB}},
		})

		observer := &recordingLogMergeObserver{}
		c := newTestExecutorContext(t, objstore.NewInMemBucket())
		c.dataBucket = dataBucket
		c.logMergeObserver = observer

		_, err := c.doLogObjectMerge(ctx, &physical.LogMerge{
			Tenant: tenant,
			Runs:   sourceLogRuns(t, dataBucket, tenant, "objA", "objB"),
		})
		require.NoError(t, err)
		require.Len(t, observer.stats, 1)

		return observer.stats[0].DuplicateRecords, readMergedRecords(ctx, t, dataBucket, tenant)
	}

	t.Run("keeps and counts the duplicate when both copies are the only records in their group", func(t *testing.T) {
		reported, output := merge(t,
			[]push.Entry{entry("1")},
			[]push.Entry{entry("1")},
		)

		require.ElementsMatch(t, []mergedRecord{want("1"), want("1")}, output)
		require.Equal(t, 1, countExactDuplicates(output))
		require.Equal(t, 1, reported)
	})

	t.Run("keeps and counts the duplicate when a distinct record shares its stream and timestamp", func(t *testing.T) {
		reported, output := merge(t,
			[]push.Entry{entry("1")},
			[]push.Entry{entry("1"), entry("22")},
		)

		require.ElementsMatch(t, []mergedRecord{want("1"), want("1"), want("22")}, output)
		require.Equal(t, 1, countExactDuplicates(output))
		require.Equal(t, 1, reported)
	})

	t.Run("keeps and counts duplicates that are not adjacent in the merged order", func(t *testing.T) {
		reported, output := merge(t,
			[]push.Entry{entry("1"), entry("22"), entry("333")},
			[]push.Entry{entry("1"), entry("22"), entry("333")},
		)

		require.ElementsMatch(t, []mergedRecord{want("1"), want("1"), want("22"), want("22"), want("333"), want("333")}, output)
		require.Equal(t, 3, countExactDuplicates(output))
		require.Equal(t, 3, reported)
	})

	t.Run("does not count records that differ only in metadata", func(t *testing.T) {
		reported, output := merge(t,
			[]push.Entry{entry("1")},
			[]push.Entry{entry("2")},
		)

		require.ElementsMatch(t, []mergedRecord{want("1"), want("2")}, output)
		require.Equal(t, 0, countExactDuplicates(output))
		require.Equal(t, 0, reported)
	})
}
