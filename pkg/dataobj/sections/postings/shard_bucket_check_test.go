package postings

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

func TestCheckShardBucketRanges(t *testing.T) {
	builder := NewBuilder(nil, 0, 0, 1<<20)
	timestamp := time.Unix(0, 0).UTC()
	builder.ObserveLabelPosting(LabelObservation{
		ObjectPath: "/obj", SectionIndex: 0, ColumnName: "env", LabelValue: "prod",
		StreamID: 1, Time: At(timestamp), UncompressedSize: 100, ShardBucket: 3,
	})
	builder.PrepareBloomColumn("/obj", 0, "trace_id", 1000, 0)
	require.NoError(t, builder.ObserveBloomPosting(BloomObservation{
		ObjectPath: "/obj", SectionIndex: 0, ColumnName: "trace_id",
		StreamID: 2, Timestamp: timestamp, UncompressedSize: 200,
	}))
	sections := openBuilderSections(t, builder)

	t.Run("passes every section that the builder wrote, with or without label rows", func(t *testing.T) {
		for _, section := range sections {
			require.NoError(t, CheckShardBucketRanges(section))
		}
	})

	t.Run("fails a section with label rows and without the min shard bucket column", func(t *testing.T) {
		var labelSection *Section
		for _, section := range sections {
			if sectionColumn(section, ColumnTypeLabelValue) != nil {
				labelSection = section
			}
		}
		require.NotNil(t, labelSection)

		withoutMinColumn := &Section{
			inner: labelSection.inner,
			columns: slices.DeleteFunc(slices.Clone(labelSection.columns), func(column *Column) bool {
				return column.Type == ColumnTypeMinShardBucket
			}),
		}

		require.ErrorContains(t, CheckShardBucketRanges(withoutMinColumn), "min_shard_bucket")
	})
}

// openBuilderSections flushes the builder into an object and opens every postings section of it.
func openBuilderSections(t *testing.T, builder *Builder) []*Section {
	t.Helper()

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(builder))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	var sections []*Section
	for _, section := range obj.Sections() {
		if !CheckSection(section) {
			continue
		}
		opened, err := Open(t.Context(), section)
		require.NoError(t, err)
		sections = append(sections, opened)
	}
	require.NotEmpty(t, sections)
	return sections
}
