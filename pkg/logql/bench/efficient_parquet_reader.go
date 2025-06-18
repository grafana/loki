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
	FilterFunc func(value parquet.Value) bool
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

// FilterWithMultipleColumns applies multiple column filters and returns valid row indices
// This implements the filter-first, read-later pattern for efficiency
func (r *EfficientParquetReader) FilterWithMultipleColumns(filters []ColumnFilter) ([]int64, error) {
	if len(filters) == 0 {
		return nil, fmt.Errorf("no filters provided")
	}

	// Start by filtering on the first column to get initial candidates
	validRows, err := r.filterSingleColumn(filters[0])
	if err != nil {
		return nil, fmt.Errorf("failed to filter on column %s: %w", filters[0].ColumnName, err)
	}

	// Apply additional filters to narrow down the results
	for i := 1; i < len(filters); i++ {
		filteredRows, err := r.filterRowsOnColumn(validRows, filters[i])
		if err != nil {
			return nil, fmt.Errorf("failed to filter on column %s: %w", filters[i].ColumnName, err)
		}
		validRows = filteredRows

		// Early exit if no rows pass all filters
		if len(validRows) == 0 {
			break
		}
	}

	return validRows, nil
}

// filterSingleColumn filters on a single column and returns all matching row indices
func (r *EfficientParquetReader) filterSingleColumn(filter ColumnFilter) ([]int64, error) {
	var validRows []int64

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

		// Read and filter values from this column chunk
		validRowsInGroup, err := r.filterColumnChunk(columnChunk, filter, rowOffset)
		if err != nil {
			continue // Skip this row group on error
		}

		validRows = append(validRows, validRowsInGroup...)
	}

	return validRows, nil
}

// filterColumnChunk reads a column chunk and applies the filter
func (r *EfficientParquetReader) filterColumnChunk(columnChunk parquet.ColumnChunk, filter ColumnFilter, rowOffset int64) ([]int64, error) {
	var validRows []int64

	pageReader := columnChunk.Pages()
	localRowIndex := int64(0)

	for {
		page, err := pageReader.ReadPage()
		if err != nil {
			break // End of pages
		}

		//r.ctx.AddPagesScanned(1)
		//r.ctx.AddDecompressedBytes(page.Size())

		// Read values from the page
		values := make([]parquet.Value, page.NumValues())
		n, err := page.Values().ReadValues(values)
		if err != nil && err != io.EOF {
			continue
		}

		// Apply filter to each value
		for i := 0; i < n; i++ {
			if filter.FilterFunc(values[i]) {
				validRows = append(validRows, rowOffset+localRowIndex+int64(i))
			}
		}

		localRowIndex += int64(n)
	}

	return validRows, nil
}

// filterRowsOnColumn filters specific rows on a column (used for subsequent filters)
func (r *EfficientParquetReader) filterRowsOnColumn(rowIndices []int64, filter ColumnFilter) ([]int64, error) {
	if len(rowIndices) == 0 {
		return rowIndices, nil
	}

	var validRows []int64

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

	// Create a map for quick lookup of target row indices
	targetRows := make(map[int64]bool)
	for _, rowIdx := range rowIndices {
		targetRows[rowIdx] = true
	}

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

		// Filter the column chunk for target rows only
		validRowsInGroup, err := r.filterColumnChunkForSpecificRows(columnChunk, filter, rowOffset, targetRows)
		if err != nil {
			continue
		}

		validRows = append(validRows, validRowsInGroup...)
	}

	return validRows, nil
}

// filterColumnChunkForSpecificRows filters a column chunk but only for specific target rows
func (r *EfficientParquetReader) filterColumnChunkForSpecificRows(columnChunk parquet.ColumnChunk, filter ColumnFilter, rowOffset int64, targetRows map[int64]bool) ([]int64, error) {
	var validRows []int64

	pageReader := columnChunk.Pages()
	localRowIndex := int64(0)

	for {
		page, err := pageReader.ReadPage()
		if err != nil {
			break // End of pages
		}

		//r.ctx.AddPagesScanned(1)
		//r.ctx.AddDecompressedBytes(page.Size())

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
			if targetRows[globalRowIndex] && filter.FilterFunc(values[i]) {
				validRows = append(validRows, globalRowIndex)
			}
		}

		localRowIndex += int64(n)
	}

	return validRows, nil
}

// ReadSpecificRows reads only the specified columns for the given row indices
// This implements the late materialization pattern - only read full records for filtered rows
func (r *EfficientParquetReader) ReadSpecificRows(rowIndices []int64, columnNames []string) ([]FilteredRecord, error) {
	if len(rowIndices) == 0 {
		return []FilteredRecord{}, nil
	}

	// Create a map for quick lookup
	targetRows := make(map[int64]bool)
	for _, rowIdx := range rowIndices {
		targetRows[rowIdx] = true
	}

	// Find column indices
	columnIndices := make(map[string]int)
	for _, colName := range columnNames {
		for i, field := range r.schema.Columns() {
			for _, f := range field {
				if f == colName {
					columnIndices[colName] = i
					break
				}
			}
		}
	}

	// Store results
	results := make(map[int64]*FilteredRecord)
	for _, rowIdx := range rowIndices {
		results[rowIdx] = &FilteredRecord{RowIndex: rowIdx}
	}

	// Read each requested column
	for colName, colIndex := range columnIndices {
		err := r.readColumnForRows(colName, colIndex, targetRows, results)
		if err != nil {
			return nil, fmt.Errorf("failed to read column %s: %w", colName, err)
		}
	}

	// Convert map to slice in the original order
	var records []FilteredRecord
	for _, rowIdx := range rowIndices {
		if record, exists := results[rowIdx]; exists {
			records = append(records, *record)
		}
	}

	return records, nil
}

// readColumnForRows reads a specific column for the target rows
func (r *EfficientParquetReader) readColumnForRows(columnName string, columnIndex int, targetRows map[int64]bool, results map[int64]*FilteredRecord) error {
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

		// Get column chunk
		columnChunks := rowGroup.ColumnChunks()
		if columnIndex >= len(columnChunks) {
			continue
		}
		columnChunk := columnChunks[columnIndex]

		// Read values for target rows
		err := r.readColumnChunkForRows(columnChunk, columnName, rowOffset, targetRows, results)
		if err != nil {
			continue // Skip this chunk on error
		}
	}

	return nil
}

// readColumnChunkForRows reads values from a column chunk for specific rows
func (r *EfficientParquetReader) readColumnChunkForRows(columnChunk parquet.ColumnChunk, columnName string, rowOffset int64, targetRows map[int64]bool, results map[int64]*FilteredRecord) error {
	pageReader := columnChunk.Pages()
	localRowIndex := int64(0)

	for {
		page, err := pageReader.ReadPage()
		if err != nil {
			break // End of pages
		}
		// TODO: Page skipping, because our predicate is in the filter function
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
			if targetRows[globalRowIndex] {
				if record, exists := results[globalRowIndex]; exists {
					r.setColumnValue(record, columnName, values[i])
				}
			}
		}

		localRowIndex += int64(n)
	}

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
		key := strings.TrimPrefix(columnName, "md$")
		key = strings.TrimPrefix(key, "label$")
		if _, ok := mdColumns[key]; ok && !value.IsNull() {
			record.Metadata[key] = value.String()
		} else if _, ok := labelColumns[key]; ok && !value.IsNull() {
			record.Labels[key] = value.String()
		}
	}
}

// Helper functions for creating common filters

// CreateTimestampFilter creates a filter for timestamp ranges
func CreateTimestampFilter(reader *parquet.File, minTime, maxTime int64) ColumnFilter {
	/* 	columnType, found := getColumnTypeByName(reader.Schema(), "Timestamp")
	   	if !found {
	   		panic("timestamp column not found")
	   	}
	*/
	return ColumnFilter{
		ColumnName: "timestamp",
		FilterFunc: func(value parquet.Value) bool {
			var timestamp int64
			timestamp = value.Int64()
			/* typeOf := reflect.ValueOf(&timestamp).Elem()
			err := columnType.AssignValue(typeOf, value)
			if err != nil {
				panic("failed to assign time value")
			} */
			return timestamp >= minTime && timestamp <= maxTime
		},
	}
}

// CreateStringFilter creates a filter for exact string matching
func CreateStringFilter(columnName, searchString string) ColumnFilter {
	return ColumnFilter{
		ColumnName: columnName,
		FilterFunc: func(value parquet.Value) bool {
			if str := value.String(); str == searchString {
				return true
			}
			return false
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
		FilterFunc: func(value parquet.Value) bool {
			if str := value.String(); str != "" && re.MatchString(str) {
				return true
			}
			return false
		},
	}
}

// CreateStringContainsFilter creates a filter for string contains matching
func CreateStringContainsFilter(columnName, substring string) ColumnFilter {
	return ColumnFilter{
		ColumnName: columnName,
		FilterFunc: func(value parquet.Value) bool {
			if str := value.String(); str != "" && contains(str, substring) {
				return true
			}
			return false
		},
	}
}

// CreateIntRangeFilter creates a filter for integer ranges
func CreateIntRangeFilter(columnName string, minVal, maxVal int64) ColumnFilter {
	return ColumnFilter{
		ColumnName: columnName,
		FilterFunc: func(value parquet.Value) bool {
			if intVal := value.Int64(); intVal >= minVal && intVal <= maxVal {
				return true
			}
			return false
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
