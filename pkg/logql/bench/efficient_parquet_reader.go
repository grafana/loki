package bench

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/parquet-go/parquet-go"
)

// FilteredRecord represents a record with only the columns we need
type FilteredRecord struct {
	RowIndex  int64
	Timestamp int64
	Line      string
	Metadata  map[string]string
	Labels    map[string]string
}

// ColumnFilter represents a filter function for a specific column
type ColumnFilter struct {
	ColumnName string
	FilterFunc func(value parquet.Value) int
}

// FilterResult represents the result of filtering with partial data populated
type FilterResult struct {
	Records          []FilteredRecord
	PopulatedColumns map[string]bool // Track which columns were populated during filtering
}

var labelColumns = map[string]struct{}{
	"cluster":      {},
	"container":    {},
	"datacenter":   {},
	"namespace":    {},
	"env":          {},
	"pod":          {},
	"region":       {},
	"service":      {},
	"service_name": {},
}
var mdColumns = map[string]struct{}{
	"level":                           {},
	"detected_level":                  {},
	"resource_auth_methods":           {},
	"resource_auth_provider":          {},
	"resource_broker_id":              {},
	"resource_cluster":                {},
	"resource_cluster_id":             {},
	"resource_db_cluster":             {},
	"resource_db_instance":            {},
	"resource_db_system":              {},
	"resource_db_version":             {},
	"resource_deployment_environment": {},
	"resource_hostname":               {},
	"resource_node_name":              {},
	"resource_num_partitions":         {},
	"resource_os_type":                {},
	"resource_os_version":             {},
	"resource_redis_cluster":          {},
	"resource_redis_db":               {},
	"resource_redis_max_memory":       {},
	"resource_service_name":           {},
	"resource_service_namespace":      {},
	"resource_service_version":        {},
	"resource_telemetry_sdk_language": {},
	"resource_telemetry_sdk_name":     {},
	"span_id":                         {},
	"trace_id":                        {},
}

// EfficientParquetReader provides methods for efficiently reading and filtering large parquet files
type EfficientParquetReader struct {
	ctx    *stats.Context
	file   *parquet.File
	schema *parquet.Schema
}

// NewEfficientParquetReader creates a new efficient parquet reader
func NewEfficientParquetReader(ctx context.Context, file *parquet.File) (*EfficientParquetReader, error) {
	statsCtx := stats.FromContext(ctx)
	return &EfficientParquetReader{
		ctx:    statsCtx,
		file:   file,
		schema: file.Schema(),
	}, nil
}

// Close closes the reader
func (r *EfficientParquetReader) Close() error {
	// parquet.File doesn't require explicit closing
	return nil
}

// setColumnValue sets the appropriate field in the FilteredRecord based on column name
func (r *EfficientParquetReader) setColumnValue(record *FilteredRecord, columnName string, value parquet.Value) {
	switch columnName {
	case "timestamp":
		record.Timestamp = value.Int64()
	case "line":
		record.Line = value.String()
	default:
		// Handle metadata or other fields
		if record.Metadata == nil {
			record.Metadata = make(map[string]string)
		}
		if record.Labels == nil {
			record.Labels = make(map[string]string)
		}
		// Use the original column name for storing in metadata/labels
		if _, ok := mdColumns[columnName]; ok && !value.IsNull() {
			record.Metadata[columnName] = value.String()
		} else if _, ok := labelColumns[columnName]; ok && !value.IsNull() {
			record.Labels[columnName] = value.String()
		}
	}
}

// FilterWithMultipleColumns applies multiple column filters and returns FilteredRecords with data populated during filtering
// This implements an optimized filter-and-populate pattern for efficiency
func (r *EfficientParquetReader) FilterWithMultipleColumns(filters []ColumnFilter) (*FilterResult, error) {
	if len(filters) == 0 {
		return nil, fmt.Errorf("no filters provided")
	}

	// Start by filtering on the first column and populating initial records
	result, err := r.filterAndPopulateSingleColumn(filters[0])
	if err != nil {
		return nil, fmt.Errorf("failed to filter on column %s: %w", filters[0].ColumnName, err)
	}

	// Apply additional filters to narrow down the results and populate more columns
	for i := 1; i < len(filters); i++ {
		filteredResult, err := r.filterAndPopulateExistingRecords(result, filters[i])
		if err != nil {
			return nil, fmt.Errorf("failed to filter on column %s: %w", filters[i].ColumnName, err)
		}
		result = filteredResult

		// Early exit if no rows pass all filters
		if len(result.Records) == 0 {
			break
		}
	}

	return result, nil
}

// filterAndPopulateSingleColumn filters on a single column and returns FilteredRecords with that column populated
func (r *EfficientParquetReader) filterAndPopulateSingleColumn(filter ColumnFilter) (*FilterResult, error) {
	var records []FilteredRecord
	populatedColumns := make(map[string]bool)
	populatedColumns[filter.ColumnName] = true

	// Find the column index
	var fileColumnName string
	if filter.ColumnName == "timestamp" {
		fileColumnName = "timestamp"
	} else if filter.ColumnName == "line" {
		fileColumnName = "line"
	} else if _, ok := labelColumns[filter.ColumnName]; ok {
		fileColumnName = fmt.Sprintf("label$%s", filter.ColumnName)
	} else if _, ok := mdColumns[filter.ColumnName]; ok {
		fileColumnName = fmt.Sprintf("md$%s", filter.ColumnName)
	} else {
		return nil, fmt.Errorf("column %s not found", filter.ColumnName)
	}

	columnIndex := -1
	for i, field := range r.schema.Columns() {
		for _, f := range field {
			if f == fileColumnName {
				columnIndex = i
				break
			}
		}
	}

	if columnIndex == -1 {
		return nil, fmt.Errorf("column %s not found", filter.ColumnName)
	}

	// Read through all row groups
	rowGroups := r.file.RowGroups()
	for rgIndex := 0; rgIndex < len(rowGroups); rgIndex++ {
		rowGroup := rowGroups[rgIndex]

		// Calculate row offset for this row group
		rowOffset := int64(0)
		for i := 0; i < rgIndex; i++ {
			rowOffset += rowGroups[i].NumRows()
		}

		// Get column chunk for the specific column
		columnChunks := rowGroup.ColumnChunks()
		if columnIndex >= len(columnChunks) {
			continue
		}
		columnChunk := columnChunks[columnIndex]

		// Read, filter, and populate values from this column chunk
		recordsInGroup, err := r.filterAndPopulateColumnChunk(columnChunk, filter, rowOffset)
		if err != nil {
			continue // Skip this row group on error
		}

		records = append(records, recordsInGroup...)
	}

	return &FilterResult{
		Records:          records,
		PopulatedColumns: populatedColumns,
	}, nil
}

// filterAndPopulateColumnChunk reads a column chunk, applies the filter, and populates matching records
func (r *EfficientParquetReader) filterAndPopulateColumnChunk(columnChunk parquet.ColumnChunk, filter ColumnFilter, rowOffset int64) ([]FilteredRecord, error) {
	var records []FilteredRecord

	pageReader := columnChunk.Pages()
	localRowIndex := int64(0)

	for {
		page, err := pageReader.ReadPage()
		if err != nil {
			break // End of pages
		}
		min, max, ok := page.Bounds()
		if ok && filter.FilterFunc(parquet.Value(min)) < 0 && filter.FilterFunc(parquet.Value(max)) > 0 {
			fmt.Println("skipping page populate", min, max)
			continue
		}

		r.ctx.AddPagesScanned(1)
		r.ctx.AddDecompressedBytes(page.Size())

		// Read values from the page
		values := make([]parquet.Value, page.NumValues())
		n, err := page.Values().ReadValues(values)
		if err != nil && err != io.EOF {
			continue
		}

		// Apply filter and populate records for matching values
		for i := 0; i < n; i++ {
			if filter.FilterFunc(values[i]) == 0 {
				record := FilteredRecord{
					RowIndex: rowOffset + localRowIndex + int64(i),
					Metadata: make(map[string]string),
					Labels:   make(map[string]string),
				}
				r.setColumnValue(&record, getActualColumnName(filter.ColumnName), values[i])
				records = append(records, record)
			}
		}

		localRowIndex += int64(n)
	}

	return records, nil
}

// filterAndPopulateExistingRecords filters existing records on a new column and populates that column's data
func (r *EfficientParquetReader) filterAndPopulateExistingRecords(result *FilterResult, filter ColumnFilter) (*FilterResult, error) {
	if len(result.Records) == 0 {
		return result, nil
	}

	// Create a map for quick lookup of target row indices
	targetRows := make(map[int64]*FilteredRecord)
	for i := range result.Records {
		targetRows[result.Records[i].RowIndex] = &result.Records[i]
	}

	// Find the column index
	columnIndex := -1
	var fileColumnName string
	if filter.ColumnName == "timestamp" {
		fileColumnName = "timestamp"
	} else if filter.ColumnName == "line" {
		fileColumnName = "line"
	} else if _, ok := labelColumns[filter.ColumnName]; ok {
		fileColumnName = fmt.Sprintf("label$%s", filter.ColumnName)
	} else if _, ok := mdColumns[filter.ColumnName]; ok {
		fileColumnName = fmt.Sprintf("md$%s", filter.ColumnName)
	} else {
		return nil, fmt.Errorf("column %s not found", filter.ColumnName)
	}

outer:
	for i, field := range r.schema.Columns() {
		for _, f := range field {
			if f == fileColumnName {
				columnIndex = i
				break outer
			}
		}
	}

	if columnIndex == -1 {
		return nil, fmt.Errorf("column %s not found", filter.ColumnName)
	}

	var validRecords []FilteredRecord

	// Read through all row groups
	rowGroups := r.file.RowGroups()
	for rgIndex := 0; rgIndex < len(rowGroups); rgIndex++ {
		rowGroup := rowGroups[rgIndex]

		rowOffset := int64(0)
		for i := 0; i < rgIndex; i++ {
			rowOffset += rowGroups[i].NumRows()
		}

		// Check if any target rows are in this row group
		rowGroupEnd := rowOffset + rowGroup.NumRows()
		hasTargetRows := false
		for rowIdx := range targetRows {
			if rowIdx >= rowOffset && rowIdx < rowGroupEnd {
				hasTargetRows = true
				break
			}
		}

		if !hasTargetRows {
			continue
		}

		// Get column chunk for the specific column
		columnChunks := rowGroup.ColumnChunks()
		if columnIndex >= len(columnChunks) {
			continue
		}
		columnChunk := columnChunks[columnIndex]

		// Filter and populate the column chunk for target rows only
		validRowsInGroup, err := r.filterAndPopulateColumnChunkForSpecificRows(columnChunk, filter, rowOffset, targetRows)
		if err != nil {
			continue
		}

		validRecords = append(validRecords, validRowsInGroup...)
	}

	// Update populated columns
	newPopulatedColumns := make(map[string]bool)
	for k, v := range result.PopulatedColumns {
		newPopulatedColumns[k] = v
	}
	newPopulatedColumns[filter.ColumnName] = true

	return &FilterResult{
		Records:          validRecords,
		PopulatedColumns: newPopulatedColumns,
	}, nil
}

// filterAndPopulateColumnChunkForSpecificRows filters a column chunk and populates data for specific target rows
func (r *EfficientParquetReader) filterAndPopulateColumnChunkForSpecificRows(columnChunk parquet.ColumnChunk, filter ColumnFilter, rowOffset int64, targetRows map[int64]*FilteredRecord) ([]FilteredRecord, error) {
	var validRecords []FilteredRecord

	pageReader := columnChunk.Pages()
	localRowIndex := int64(0)

	for {
		page, err := pageReader.ReadPage()
		if err != nil {
			break // End of pages
		}

		min, max, ok := page.Bounds()
		if ok && filter.FilterFunc(min) < 0 && filter.FilterFunc(max) > 0 {
			fmt.Println("skipping page", min, max, filter.ColumnName)
			continue
		}

		r.ctx.AddPagesScanned(1)
		r.ctx.AddDecompressedBytes(page.Size())

		// Read values from the page
		values := make([]parquet.Value, page.NumValues())
		valuesReader := page.Values()
		n, err := valuesReader.ReadValues(values)
		if err != nil && err != io.EOF {
			continue
		}

		// Check only the values for target rows
		for i := 0; i < n; i++ {
			globalRowIndex := rowOffset + localRowIndex + int64(i)
			if record, exists := targetRows[globalRowIndex]; exists {
				if filter.FilterFunc(values[i]) == 0 {
					// Populate the column data in the existing record
					r.setColumnValue(record, getActualColumnName(filter.ColumnName), values[i])
					validRecords = append(validRecords, *record)
				}
			}
		}

		localRowIndex += int64(n)
	}

	return validRecords, nil
}

// ReadRemainingColumns reads only the columns that weren't populated during filtering
func (r *EfficientParquetReader) ReadRemainingColumns(filterResult *FilterResult, requestedColumns []string) ([]FilteredRecord, error) {
	if len(filterResult.Records) == 0 {
		return []FilteredRecord{}, nil
	}

	// Determine which columns still need to be read
	columnsToRead := make([]string, 0)
	for _, colName := range requestedColumns {
		if !filterResult.PopulatedColumns[colName] {
			columnsToRead = append(columnsToRead, colName)
		}
	}

	// If all requested columns were already populated during filtering, return as-is
	if len(columnsToRead) == 0 {
		return filterResult.Records, nil
	}

	// Create a map for quick lookup
	recordsByRowIndex := make(map[int64]*FilteredRecord)
	for i := range filterResult.Records {
		recordsByRowIndex[filterResult.Records[i].RowIndex] = &filterResult.Records[i]
	}

	// Find column indices for remaining columns
	columnIndices := make(map[string]int)
	for _, colName := range columnsToRead {
		fileColumnName := getFileColumnName(colName)
		for i, field := range r.schema.Columns() {
			for _, f := range field {
				if f == fileColumnName {
					columnIndices[colName] = i
					break
				}
			}
		}
	}

	// Read each remaining column
	for colName, colIndex := range columnIndices {
		err := r.readColumnForExistingRecords(colName, colIndex, recordsByRowIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to read column %s: %w", colName, err)
		}
	}

	return filterResult.Records, nil
}

// readColumnForExistingRecords reads a specific column for existing FilteredRecord objects
func (r *EfficientParquetReader) readColumnForExistingRecords(columnName string, columnIndex int, records map[int64]*FilteredRecord) error {
	// Read through all row groups
	rowGroups := r.file.RowGroups()
	for rgIndex := 0; rgIndex < len(rowGroups); rgIndex++ {
		rowGroup := rowGroups[rgIndex]

		rowOffset := int64(0)
		for i := 0; i < rgIndex; i++ {
			rowOffset += rowGroups[i].NumRows()
		}

		// Check if any target rows are in this row group
		rowGroupEnd := rowOffset + rowGroup.NumRows()
		hasTargetRows := false
		for rowIdx := range records {
			if rowIdx >= rowOffset && rowIdx < rowGroupEnd {
				hasTargetRows = true
				break
			}
		}

		if !hasTargetRows {
			continue
		}

		// Get column chunk
		columnChunks := rowGroup.ColumnChunks()
		if columnIndex >= len(columnChunks) {
			continue
		}
		columnChunk := columnChunks[columnIndex]

		// Read values for target rows
		err := r.readColumnChunkForExistingRecords(columnChunk, columnName, rowOffset, records)
		if err != nil {
			continue // Skip this chunk on error
		}
	}

	return nil
}

// readColumnChunkForExistingRecords reads values from a column chunk for existing FilteredRecord objects
func (r *EfficientParquetReader) readColumnChunkForExistingRecords(columnChunk parquet.ColumnChunk, columnName string, rowOffset int64, records map[int64]*FilteredRecord) error {
	pageReader := columnChunk.Pages()
	localRowIndex := int64(0)

	for {
		page, err := pageReader.ReadPage()
		if err != nil {
			break // End of pages
		}

		r.ctx.AddPagesScanned(1)
		r.ctx.AddDecompressedBytes(page.Size())

		// Read values from the page
		values := make([]parquet.Value, page.NumValues())
		n, err := page.Values().ReadValues(values)
		if err != nil && err != io.EOF {
			continue
		}

		// Store values for target rows
		for i := 0; i < n; i++ {
			globalRowIndex := rowOffset + localRowIndex + int64(i)
			if record, exists := records[globalRowIndex]; exists {
				r.setColumnValue(record, getActualColumnName(columnName), values[i])
			}
		}

		localRowIndex += int64(n)
	}

	return nil
}

// Helper function to get the actual column name (without prefixes)
func getActualColumnName(columnName string) string {
	if columnName == "timestamp" || columnName == "line" {
		return columnName
	}
	// For metadata and label columns, we want to store them with their original names
	return columnName
}

// Helper function to get the file column name (with prefixes)
func getFileColumnName(columnName string) string {
	if columnName == "timestamp" {
		return "timestamp"
	} else if columnName == "line" {
		return "line"
	} else if _, ok := labelColumns[columnName]; ok {
		return fmt.Sprintf("label$%s", columnName)
	} else if _, ok := mdColumns[columnName]; ok {
		return fmt.Sprintf("md$%s", columnName)
	}
	return columnName
}

// Helper functions for creating common filters

// CreateTimestampFilter creates a filter for timestamp ranges
func CreateTimestampFilter(reader *parquet.File, minTime, maxTime int64) ColumnFilter {
	return ColumnFilter{
		ColumnName: "timestamp",
		FilterFunc: func(value parquet.Value) int {
			timestamp := value.Int64()
			if timestamp >= minTime && timestamp <= maxTime {
				return 0
			}
			if timestamp < minTime {
				return -1
			}
			if timestamp > maxTime {
				return 1
			}
			return -1
		},
	}
}

// CreateStringFilter creates a filter for exact string matching
func CreateStringFilter(columnName, searchString string) ColumnFilter {
	return ColumnFilter{
		ColumnName: columnName,
		FilterFunc: func(value parquet.Value) int {
			return strings.Compare(searchString, value.String())
		},
	}
}

// CreateStringFilter creates a filter for exact string matching
func CreateRegexFilter(columnName, regexString string) ColumnFilter {
	re, err := regexp.Compile(regexString)
	if err != nil {
		panic(err)
	}
	return ColumnFilter{
		ColumnName: columnName,
		FilterFunc: func(value parquet.Value) int {
			if str := value.String(); str != "" && re.MatchString(str) {
				return 0
			}
			return 1
		},
	}
}

// CreateIntRangeFilter creates a filter for integer ranges
func CreateIntRangeFilter(columnName string, minVal, maxVal int64) ColumnFilter {
	return ColumnFilter{
		ColumnName: columnName,
		FilterFunc: func(value parquet.Value) int {
			intVal := value.Int64()
			if intVal >= minVal && intVal <= maxVal {
				return 0
			}
			if intVal < minVal {
				return -1
			}
			if intVal > maxVal {
				return 1
			}
			return -1
		},
	}
}

// Helper function for string contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (len(substr) == 0 || indexOfSubstring(s, substr) >= 0)
}

// Simple string search helper
func indexOfSubstring(s, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
