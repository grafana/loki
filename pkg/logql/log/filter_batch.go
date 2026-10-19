package log

import (
	"bytes"
	"sync"
)

// LinesBatch is a batch of lines stored back to back in one buffer.
type LinesBatch struct {
	// Data holds the lines back to back. Line i is Data[Offsets[i]:Offsets[i+1]]. Data can hold
	// bytes outside the lines, and a line can be empty.
	Data []byte

	// Offsets holds one more element than there are lines. It does not have to start at 0.
	//
	// Offsets must not decrease, and its last element must not exceed len(Data). The functions of
	// this package do not check these rules. A LinesBatch that breaks them can make a function
	// panic or return a wrong result.
	Offsets []int32
}

func (l LinesBatch) count() int {
	if len(l.Offsets) == 0 {
		return 0
	}
	return len(l.Offsets) - 1
}

// line returns line i. The capacity of the result ends with the line, so a Filterer that appends
// to its line cannot overwrite the next line.
func (l LinesBatch) line(i int32) []byte {
	return l.Data[l.Offsets[i]:l.Offsets[i+1]:l.Offsets[i+1]]
}

// defaultSequentialIndexes holds 0, 1, 2, and so on. FilterBatch slices it for a batch of up to
// 4096 lines, so the batch needs no list of its own. It is shared and read-only.
var defaultSequentialIndexes = func() []int32 {
	s := make([]int32, 4096)
	for i := range s {
		s[i] = int32(i)
	}
	return s
}()

// FilterBatch appends to dstIndexes the index of every line in batch that passes f, in ascending
// order, and returns the result. The result is the same as calling f.Filter on each line.
//
// FilterBatch does not retain batch after it returns. It is safe for concurrent use if f is.
func FilterBatch(f Filterer, batch LinesBatch, dstIndexes []int32) []int32 {
	n := batch.count()
	if n <= len(defaultSequentialIndexes) {
		return filterBatchAtIndexes(f, batch, defaultSequentialIndexes[:n], dstIndexes)
	}

	srcIndexes := make([]int32, n)
	for i := range srcIndexes {
		srcIndexes[i] = int32(i)
	}
	return filterBatchAtIndexes(f, batch, srcIndexes, dstIndexes)
}

// batchFilterer is a Filterer that tests a batch of lines at once.
type batchFilterer interface {
	Filterer

	// filterBatch appends to dstIndexes the index of every line in srcIndexes that passes the
	// filter, in ascending order. srcIndexes lists the lines to test: distinct indexes in
	// ascending order.
	//
	// dstIndexes may be srcIndexes[:0]. The j-th index that filterBatch appends then overwrites
	// srcIndexes[j], so filterBatch must read srcIndexes[j] before it appends that index.
	// filterBatch must not write to srcIndexes in any other way, because srcIndexes can be the
	// shared defaultSequentialIndexes.
	filterBatch(batch LinesBatch, srcIndexes, dstIndexes []int32) []int32
}

// scratchPool holds the index lists that orFilter and notFilter need, so a call does not allocate
// them.
var scratchPool = sync.Pool{New: func() any {
	s := make([]int32, 0, 1024)
	return &s
}}

func getScratchIndexes() *[]int32 {
	return scratchPool.Get().(*[]int32)
}

func putScratchIndexes(s *[]int32) {
	*s = (*s)[:0]
	scratchPool.Put(s)
}

func (trueFilter) filterBatch(_ LinesBatch, srcIndexes, dstIndexes []int32) []int32 {
	return append(dstIndexes, srcIndexes...)
}

func (l *containsFilter) filterBatch(batch LinesBatch, srcIndexes, dstIndexes []int32) []int32 {
	if len(l.match) == 0 {
		return append(dstIndexes, srcIndexes...)
	}

	// filterBatchCaseSensitive matches case, so a case-insensitive filter tests each line of
	// srcIndexes instead. It also tests every line of the batch, so it applies only when srcIndexes
	// lists every line: distinct, ascending indexes, as many as the batch has lines.
	if l.caseInsensitive || len(srcIndexes) != batch.count() {
		for _, i := range srcIndexes {
			if contains(batch.line(i), l.match, l.caseInsensitive) {
				dstIndexes = append(dstIndexes, i)
			}
		}
		return dstIndexes
	}
	return l.filterBatchCaseSensitive(batch, dstIndexes)
}

// filterBatchCaseSensitive appends to dstIndexes the index of every line in batch that holds
// l.match, in ascending order. It tests every line of the batch, not a subset, and it matches
// case, so the caller must not use it when l.caseInsensitive is set. l.match must not be empty.
//
// It searches the whole buffer with one bytes.Index call per hit, instead of one call per line.
func (l *containsFilter) filterBatchCaseSensitive(batch LinesBatch, dstIndexes []int32) []int32 {
	n := batch.count()
	if n == 0 {
		return dstIndexes
	}

	var (
		offsets = batch.Offsets
		pos     = int(offsets[0])
		end     = int(offsets[n])
		row     = 0
	)
	for pos+len(l.match) <= end {
		i := bytes.Index(batch.Data[pos:end], l.match)
		if i < 0 {
			break
		}
		hit := pos + i

		// Find the line that holds the hit: the last r with offsets[r] <= hit. Hits come in
		// ascending order and often sit in the next line, so gallop from the previous line
		// before the binary search.
		lo, step := row, 1
		for lo+step <= n && int(offsets[lo+step]) <= hit {
			lo += step
			step <<= 1
		}
		hi := min(lo+step, n) // offsets[hi] > hit
		for hi-lo > 1 {
			mid := lo + (hi-lo)/2
			if int(offsets[mid]) <= hit {
				lo = mid
			} else {
				hi = mid
			}
		}

		// The buffer has no separator between lines, so l.match can match across two lines.
		// Such a hit belongs to no line. A real hit can start inside it, so continue at hit+1.
		if hit+len(l.match) > int(offsets[lo+1]) {
			pos = hit + 1
			row = lo
			continue
		}

		dstIndexes = append(dstIndexes, int32(lo))
		pos = int(offsets[lo+1])
		row = lo + 1
	}
	return dstIndexes
}

func (f containsAllFilter) filterBatch(batch LinesBatch, srcIndexes, dstIndexes []int32) []int32 {
	if len(f.matches) == 0 {
		return append(dstIndexes, srcIndexes...)
	}

	base := len(dstIndexes)
	for k := range f.matches {
		if k == 0 {
			dstIndexes = f.matches[k].filterBatch(batch, srcIndexes, dstIndexes)
		} else {
			// Each later match tests the lines that the previous one kept. They sit in
			// dstIndexes[base:], and the later match overwrites them in place with its own result.
			dstIndexes = f.matches[k].filterBatch(batch, dstIndexes[base:], dstIndexes[:base])
		}
		if len(dstIndexes) == base {
			break
		}
	}
	return dstIndexes
}

func (a andFilters) filterBatch(batch LinesBatch, srcIndexes, dstIndexes []int32) []int32 {
	if len(a.filters) == 0 {
		return append(dstIndexes, srcIndexes...)
	}

	base := len(dstIndexes)
	for k, f := range a.filters {
		if k == 0 {
			dstIndexes = filterBatchAtIndexes(f, batch, srcIndexes, dstIndexes)
		} else {
			// Each later filter tests the lines that the previous one kept. They sit in
			// dstIndexes[base:], and the later filter overwrites them in place with its own result.
			dstIndexes = filterBatchAtIndexes(f, batch, dstIndexes[base:], dstIndexes[:base])
		}
		if len(dstIndexes) == base {
			break
		}
	}
	return dstIndexes
}

func (a andFilter) filterBatch(batch LinesBatch, srcIndexes, dstIndexes []int32) []int32 {
	base := len(dstIndexes)
	dstIndexes = filterBatchAtIndexes(a.left, batch, srcIndexes, dstIndexes)
	if len(dstIndexes) == base {
		return dstIndexes
	}
	return filterBatchAtIndexes(a.right, batch, dstIndexes[base:], dstIndexes[:base])
}

func (a orFilter) filterBatch(batch LinesBatch, srcIndexes, dstIndexes []int32) []int32 {
	left, rest, right := getScratchIndexes(), getScratchIndexes(), getScratchIndexes()
	defer func() {
		putScratchIndexes(left)
		putScratchIndexes(rest)
		putScratchIndexes(right)
	}()

	*left = filterBatchAtIndexes(a.left, batch, srcIndexes, *left)
	// The right side tests only the lines that the left side dropped.
	*rest = appendDifference(*rest, srcIndexes, *left)
	*right = filterBatchAtIndexes(a.right, batch, *rest, *right)
	return appendUnion(dstIndexes, *left, *right)
}

func (n notFilter) filterBatch(batch LinesBatch, srcIndexes, dstIndexes []int32) []int32 {
	hits := getScratchIndexes()
	defer putScratchIndexes(hits)

	*hits = filterBatchAtIndexes(n.MatcherFilterer, batch, srcIndexes, *hits)
	return appendDifference(dstIndexes, srcIndexes, *hits)
}

// filterBatchAtIndexes appends to dstIndexes the index of every line in srcIndexes that passes f.
// It uses the batch form of f when f has one, and calls Filter on each line otherwise.
func filterBatchAtIndexes(f Filterer, batch LinesBatch, srcIndexes, dstIndexes []int32) []int32 {
	switch f := f.(type) {
	case wrapper:
		// A wrapper adds a Matcher to a Filterer. Test the inner filter so its batch form applies.
		if f.Filterer != nil {
			return filterBatchAtIndexes(f.Filterer, batch, srcIndexes, dstIndexes)
		}
	case batchFilterer:
		return f.filterBatch(batch, srcIndexes, dstIndexes)
	}

	for _, i := range srcIndexes {
		if f.Filter(batch.line(i)) {
			dstIndexes = append(dstIndexes, i)
		}
	}
	return dstIndexes
}

// appendDifference appends to dst the elements of a that are not in b. Both hold distinct indexes
// in ascending order. dst may be a[:0].
func appendDifference(dst, a, b []int32) []int32 {
	j := 0
	for _, x := range a {
		for j < len(b) && b[j] < x {
			j++
		}
		if j < len(b) && b[j] == x {
			continue
		}
		dst = append(dst, x)
	}
	return dst
}

// appendUnion appends to dst the elements of a and b, each once. Both hold distinct indexes in
// ascending order. dst must not share memory with a or b.
func appendUnion(dst, a, b []int32) []int32 {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			dst = append(dst, a[i])
			i++
		case a[i] > b[j]:
			dst = append(dst, b[j])
			j++
		default:
			dst = append(dst, a[i])
			i++
			j++
		}
	}
	dst = append(dst, a[i:]...)
	return append(dst, b[j:]...)
}
