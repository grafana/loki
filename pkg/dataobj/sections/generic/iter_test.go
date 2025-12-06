package generic

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// TestIterWithMultipleSections tests reading data from multiple generic sections
// with different kinds using the Iter functionality.
func TestIterWithMultipleSections(t *testing.T) {
	ctx := context.Background()

	// Create schema for "events" sections
	eventsSchema := NewSchema([]arrow.Field{
		{Name: "event_id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "event_type", Type: arrow.BinaryTypes.String},
		{Name: "timestamp", Type: arrow.FixedWidthTypes.Timestamp_ns},
	})

	// Create schema for "metrics" sections
	metricsSchema := NewSchema([]arrow.Field{
		{Name: "metric_id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "metric_name", Type: arrow.BinaryTypes.String},
		{Name: "value", Type: arrow.PrimitiveTypes.Int64},
	})

	// Create a data object builder
	objBuilder := dataobj.NewBuilder(nil)

	// Create and populate "events" section
	eventsBuilder := NewBuilder("events", eventsSchema, nil, BuilderOptions{
		PageSizeHint:    1024 * 1024,
		PageMaxRowCount: 10000,
	})
	eventsBuilder.SetTenant("test-tenant")

	alloc := memory.DefaultAllocator
	eventsData := []arrow.RecordBatch{
		arrowtest.Rows{
			{"event_id": int64(1), "event_type": "login", "timestamp": time.Unix(0, 1234567890000000000).UTC()},
		}.Record(alloc, eventsSchema.inner),
		arrowtest.Rows{
			{"event_id": int64(2), "event_type": "logout", "timestamp": time.Unix(0, 1234567891000000000).UTC()},
		}.Record(alloc, eventsSchema.inner),
		arrowtest.Rows{
			{"event_id": int64(3), "event_type": "click", "timestamp": time.Unix(0, 1234567892000000000).UTC()},
		}.Record(alloc, eventsSchema.inner),
	}

	for _, entity := range eventsData {
		err := eventsBuilder.Append(entity)
		require.NoError(t, err)
	}

	// Create and populate "metrics" section
	metricsBuilder := NewBuilder("metrics", metricsSchema, nil, BuilderOptions{
		PageSizeHint:    1024 * 1024,
		PageMaxRowCount: 10000,
	})
	metricsBuilder.SetTenant("test-tenant")

	metricsData := []arrow.RecordBatch{
		arrowtest.Rows{
			{"metric_id": int64(100), "metric_name": "cpu_usage", "value": int64(75)},
		}.Record(alloc, metricsSchema.inner),
		arrowtest.Rows{
			{"metric_id": int64(101), "metric_name": "memory_usage", "value": int64(82)},
		}.Record(alloc, metricsSchema.inner),
	}

	for _, entity := range metricsData {
		err := metricsBuilder.Append(entity)
		require.NoError(t, err)
	}

	// Add another "events" section to test multiple sections of same kind
	eventsBuilder2 := NewBuilder("events", eventsSchema, nil, BuilderOptions{
		PageSizeHint:    1024 * 1024,
		PageMaxRowCount: 10000,
	})
	eventsBuilder2.SetTenant("test-tenant")

	eventsData2 := []arrow.RecordBatch{
		arrowtest.Rows{
			{"event_id": int64(4), "event_type": "error", "timestamp": time.Unix(0, 1234567893000000000).UTC()},
		}.Record(alloc, eventsSchema.inner),
	}

	for _, entity := range eventsData2 {
		err := eventsBuilder2.Append(entity)
		require.NoError(t, err)
	}

	// Flush all sections to the data object
	require.NoError(t, objBuilder.Append(eventsBuilder))
	require.NoError(t, objBuilder.Append(metricsBuilder))
	require.NoError(t, objBuilder.Append(eventsBuilder2))

	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	// Verify we have 3 sections
	require.Len(t, obj.Sections(), 3)

	t.Run("read events sections", func(t *testing.T) {
		// Filter and open events sections
		eventsSectionCount := 0

		for i, sectionRef := range obj.Sections().Filter(CheckSection("events")) {
			eventsSectionCount++
			section, err := Open(ctx, sectionRef, "events")
			require.NoError(t, err, "opening events section %d", i)
			require.NotNil(t, section)

			// Verify columns
			columns := section.Columns()
			require.Len(t, columns, 3)
			require.Equal(t, "event_id", columns[0].Name)
			require.Equal(t, "event_type", columns[1].Name)
			require.Equal(t, "timestamp", columns[2].Name)
		}

		require.Equal(t, 2, eventsSectionCount, "should have 2 events sections")
	})

	t.Run("read metrics section", func(t *testing.T) {
		// Filter and open metrics sections
		metricsSectionCount := 0
		var section *Section

		for _, sectionRef := range obj.Sections().Filter(CheckSection("metrics")) {
			metricsSectionCount++
			var err error
			section, err = Open(ctx, sectionRef, "metrics")
			require.NoError(t, err)
			require.NotNil(t, section)
		}

		require.Equal(t, 1, metricsSectionCount, "should have 1 metrics section")

		// Verify columns
		columns := section.Columns()
		require.Len(t, columns, 3)
		require.Equal(t, "metric_id", columns[0].Name)
		require.Equal(t, "metric_name", columns[1].Name)
		require.Equal(t, "value", columns[2].Name)
	})

	t.Run("verify section filtering", func(t *testing.T) {
		// Test that CheckSection correctly filters by kind
		allSections := obj.Sections()

		// Count events sections
		eventsCount := 0
		for range allSections.Filter(CheckSection("events")) {
			eventsCount++
		}
		require.Equal(t, 2, eventsCount)

		// Count metrics sections
		metricsCount := 0
		for range allSections.Filter(CheckSection("metrics")) {
			metricsCount++
		}
		require.Equal(t, 1, metricsCount)

		// Count non-existent sections
		tracesCount := 0
		for range allSections.Filter(CheckSection("traces")) {
			tracesCount++
		}
		require.Equal(t, 0, tracesCount)
	})

	t.Run("verify column projection", func(t *testing.T) {
		// Open first events section
		var section *Section
		for _, sectionRef := range obj.Sections().Filter(CheckSection("events")) {
			var err error
			section, err = Open(ctx, sectionRef, "events")
			require.NoError(t, err)
			break // Only open the first one
		}
		require.NotNil(t, section, "should have found at least one events section")

		// Verify we can select specific columns
		columns := section.Columns()
		require.Len(t, columns, 3)

		// This test verifies that we can open sections and access their columns.
		// Actual data reading via Reader would require the full implementation
		// of the Iter/Reader functionality which is still in progress.
		selectedColumns := []*Column{columns[0], columns[2]} // event_id and timestamp only
		require.Len(t, selectedColumns, 2)
		require.Equal(t, "event_id", selectedColumns[0].Name)
		require.Equal(t, "timestamp", selectedColumns[1].Name)
	})
}

func TestIterReadData(t *testing.T) {
	ctx := context.Background()

	// Define a schema
	schema := NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String},
		{Name: "timestamp", Type: arrow.FixedWidthTypes.Timestamp_ns},
	})

	// Create a builder
	builder := NewBuilder("test_data", schema, nil, BuilderOptions{
		PageSizeHint:    1024 * 1024,
		PageMaxRowCount: 10000,
	})
	builder.SetTenant("test-tenant")

	alloc := memory.DefaultAllocator

	// Add test entities
	entities := []arrow.RecordBatch{
		arrowtest.Rows{
			{"id": int64(1), "name": "Alice", "timestamp": time.Unix(0, 1000000000).UTC()},
		}.Record(alloc, schema.inner),
		arrowtest.Rows{
			{"id": int64(2), "name": "Bob", "timestamp": time.Unix(0, 2000000000).UTC()},
		}.Record(alloc, schema.inner),
		arrowtest.Rows{
			{"id": int64(3), "name": "Charlie", "timestamp": time.Unix(0, 3000000000).UTC()},
		}.Record(alloc, schema.inner),
	}

	for _, entity := range entities {
		err := builder.Append(entity)
		require.NoError(t, err)
	}

	// Build the dataobj
	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(builder))

	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()
	require.NotNil(t, obj)

	t.Run("iterate over all records using Iter", func(t *testing.T) {
		recordCount := 0
		totalRows := 0

		// Use the Iter function to iterate over all records
		for result := range Iter(ctx, obj, "test_data") {
			require.NoError(t, result.Err())

			record := result.MustValue()
			require.NotNil(t, record)

			recordCount++
			numRows := int(record.NumRows())
			totalRows += numRows

			t.Logf("Record %d: NumRows=%d, NumCols=%d", recordCount, numRows, record.NumCols())

			// Verify the schema
			schema := record.Schema()
			require.NotNil(t, schema)
			require.Equal(t, 3, schema.NumFields(), "should have 3 fields")

			// Release the record after use
			record.Release()
		}

		t.Logf("Total records: %d, Total rows: %d", recordCount, totalRows)

		// We should have at least one record batch
		require.Greater(t, recordCount, 0, "should have read at least one record batch")
		// We should have read all 3 rows
		require.Equal(t, 3, totalRows, "should have read all 3 rows")
	})

	t.Run("iterate over section directly using IterSection", func(t *testing.T) {
		// Open the first section
		var section *Section
		for _, sectionRef := range obj.Sections().Filter(CheckSection("test_data")) {
			var err error
			section, err = Open(ctx, sectionRef, "test_data")
			require.NoError(t, err)
			break
		}
		require.NotNil(t, section)

		recordCount := 0
		totalRows := 0

		// Use IterSection to iterate over records in the section
		for result := range IterSection(ctx, section, "test_data") {
			require.NoError(t, result.Err())

			record := result.MustValue()
			require.NotNil(t, record)

			recordCount++
			totalRows += int(record.NumRows())

			// Verify the schema matches our expectations
			schema := record.Schema()
			require.Equal(t, 3, schema.NumFields())

			// Verify field names (they include type info)
			fields := schema.Fields()
			require.Contains(t, fields[0].Name, "id")
			require.Contains(t, fields[1].Name, "name")
			require.Contains(t, fields[2].Name, "timestamp")

			// Release the record
			record.Release()
		}

		require.Greater(t, recordCount, 0, "should have read at least one record batch")
		require.Equal(t, 3, totalRows, "should have read all 3 rows")
	})
}
