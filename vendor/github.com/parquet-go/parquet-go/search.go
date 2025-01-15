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
	n := index.NumPages()
	curIdx := 0
	topIdx := n

	// while there's at least one more page to check
	for (topIdx - curIdx) > 1 {

		// nextIdx is set to halfway between curIdx and topIdx
		nextIdx := ((topIdx - curIdx) / 2) + curIdx

		smallerThanMin := cmp(value, index.MinValue(nextIdx))

		switch {
		// search below pages[nextIdx]
		case smallerThanMin < 0:
			topIdx = nextIdx
		// search pages[nextIdx] and above
		case smallerThanMin > 0:
			curIdx = nextIdx
		case smallerThanMin == 0:
			// this case is hit when winValue == value of nextIdx
			// we must check below this index to find if there's
			// another page before this.
			// e.g. searching for first page 3 is in:
			// [1,2,3]
			// [3,4,5]
			// [6,7,8]

			// if the page proceeding this has a maxValue matching the value we're
			// searching, continue the search.
			// otherwise, we can return early
			//
			// cases covered by else block
			// if cmp(value, index.MaxValue(nextIdx-1)) < 0: the value is only in this page
			// if cmp(value, index.MaxValue(nextIdx-1)) > 0: we've got a sorting problem with overlapping pages
			//
			// bounds check not needed for nextIdx-1 because nextIdx is guaranteed to be at least curIdx + 1
			// line 82 & 85 above
			if cmp(value, index.MaxValue(nextIdx-1)) == 0 {
				topIdx = nextIdx
			} else {
				return nextIdx
			}
		}
	}

	// last page check, if it wasn't explicitly found above
	if curIdx < n {

		// check pages[curIdx] for value
		min := index.MinValue(curIdx)
		max := index.MaxValue(curIdx)

		// if value is not in pages[curIdx], then it's not in this columnChunk
		if cmp(value, min) < 0 || cmp(value, max) > 0 {
			curIdx = n
		}
	}

	return curIdx
}

func linearSearch(index ColumnIndex, value Value, cmp func(Value, Value) int) int {
	n := index.NumPages()

	for i := 0; i < n; i++ {
		min := index.MinValue(i)
		max := index.MaxValue(i)

		if cmp(min, value) <= 0 && cmp(value, max) <= 0 {
			return i
		}
	}

	return n
}
