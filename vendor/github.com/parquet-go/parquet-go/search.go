package parquet

// Search is like Find, but uses the default ordering of the given type. Search
// and Find are scoped to a given ColumnChunk and find the pages within a
// ColumnChunk which might contain the result.  See Find for more details.
func Search(index ColumnIndex, value Value, typ Type) int {
	return Find(index, value, CompareNullsLast(typ.Compare))
}

// Find uses the ColumnIndex passed as argument to find the page in a column
// chunk (determined by the given ColumnIndex) that the given value is expected
// to be found in.
//
// The function returns the index of the first page that might contain the
// value. If the function determines that the value does not exist in the
// index, NumPages is returned.
//
// If you want to search the entire parquet file, you must iterate over the
// RowGroups and search each one individually, if there are multiple in the
// file. If you call writer.Flush before closing the file, then you will have
// multiple RowGroups to iterate over, otherwise Flush is called once on Close.
//
// The comparison function passed as last argument is used to determine the
// relative order of values. This should generally be the Compare method of
// the column type, but can sometimes be customized to modify how null values
// are interpreted, for example:
//
//	pageIndex := parquet.Find(columnIndex, value,
//		parquet.CompareNullsFirst(typ.Compare),
//	)
func Find(index ColumnIndex, value Value, cmp func(Value, Value) int) int {
	switch {
	case index.IsAscending():
		return binarySearch(index, value, cmp)
	default:
		return linearSearch(index, value, cmp)
	}
}

func binarySearch(index ColumnIndex, value Value, cmp func(Value, Value) int) int {
	numPages := index.NumPages()
	topIndex := numPages
	currentIndex := 0

	// while there's at least one more page to check
	for currentIndex < topIndex {
		// nextIndex is set to halfway between currentIndex and topIndex
		nextIndex := ((topIndex - currentIndex) / 2) + currentIndex

		// Compare against both min and max to handle overlapping page bounds.
		// When page bounds overlap due to truncation, we need to search left
		// to find the first page that might contain the value.
		switch {
		case cmp(value, index.MinValue(nextIndex)) < 0:
			// value < min: can't be in this page or any after it
			topIndex = nextIndex
		case cmp(value, index.MaxValue(nextIndex)) > 0:
			// value > max: can't be in this page or any before it (including nextIndex)
			currentIndex = nextIndex + 1
		default:
			// min <= value <= max: value might be in this page or an earlier one
			// with overlapping bounds, so search left to find the first occurrence
			topIndex = nextIndex
		}
	}

	// After the loop, currentIndex == topIndex points to the candidate page.
	// Verify the value is actually within the page bounds.
	if currentIndex < numPages {
		minValue := index.MinValue(currentIndex)
		maxValue := index.MaxValue(currentIndex)

		// If value is not in pages[currentIndex], then it's not in this columnChunk
		if cmp(value, minValue) < 0 || cmp(value, maxValue) > 0 {
			return numPages
		}
	}

	return currentIndex
}

func linearSearch(index ColumnIndex, value Value, cmp func(Value, Value) int) int {
	n := index.NumPages()

	for i := range n {
		min := index.MinValue(i)
		max := index.MaxValue(i)

		if cmp(min, value) <= 0 && cmp(value, max) <= 0 {
			return i
		}
	}

	return n
}
