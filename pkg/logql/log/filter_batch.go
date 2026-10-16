package log

import (
	"bytes"
	"sync"
)

// Lines is a batch of lines stored back to back in one buffer.
//
// Line i is Data[Offsets[i]:Offsets[i+1]], so Offsets holds one more element than there are
// lines. Offsets does not have to start at 0, and Data can hold bytes outside the lines. A line
// can be empty.
//
// Offsets must not decrease, and its last element must not exceed len(Data). The functions of
// this package do not check these rules. A Lines that breaks them can make a function panic or
// return a wrong result.
type Lines struct {
	Data    []byte
	Offsets []int32
}

func (l Lines) count() int {
	if len(l.Offsets) == 0 {
		return 0
	}
	return len(l.Offsets) - 1
}

// line returns line i. The capacity of the result ends with the line, so a Filterer that appends
// to its line cannot overwrite the next line.
func (l Lines) line(i int32) []byte {
	return l.Data[l.Offsets[i]:l.Offsets[i+1]:l.Offsets[i+1]]
}

// batchFilterer is a Filterer that tests a batch of lines at once.
type batchFilterer interface {
	Filterer

	// filterBatch appends to dst the index of every line in cand that passes the filter, in
	// ascending order. cand lists the lines to test: distinct indexes in ascending order.
	//
	// dst may be cand[:0]. The j-th index that filterBatch appends then overwrites cand[j], so
	// filterBatch must read cand[j] before it appends that index. filterBatch must not write to
	// cand in any other way, because cand can be the shared allLines.
	filterBatch(dst []int32, lines Lines, cand []int32) []int32
}

// allLines holds 0, 1, 2, and so on. FilterBatch slices it for a batch of up to 4096 lines, so
// the batch needs no list of its own. It is shared and read-only.
var allLines = func() []int32 {
	s := make([]int32, 4096)
	for i := range s {
		s[i] = int32(i)
	}
	return s
}()

// FilterBatch appends to dst the index of every line in lines that passes f, in ascending order,
// and returns the result. The result is the same as calling f.Filter on each line.
//
// A case-sensitive literal filter that tests every line of the batch searches the whole buffer
// with one bytes.Index call per hit, not one call per line. A Filterer without a batch form still
// works: FilterBatch calls Filter on each line.
//
// FilterBatch does not retain lines after it returns. It is safe for concurrent use if f is.
func FilterBatch(f Filterer, dst []int32, lines Lines) []int32 {
	n := lines.count()
	if n <= len(allLines) {
		return filterCandidates(f, dst, lines, allLines[:n])
	}

	cand := make([]int32, n)
	for i := range cand {
		cand[i] = int32(i)
	}
	return filterCandidates(f, dst, lines, cand)
}

func filterCandidates(f Filterer, dst []int32, lines Lines, cand []int32) []int32 {
	switch f := f.(type) {
	case wrapper:
		// A wrapper adds a Matcher to a Filterer. Test the inner filter so its batch form applies.
		if f.Filterer != nil {
			return filterCandidates(f.Filterer, dst, lines, cand)
		}
	case batchFilterer:
		return f.filterBatch(dst, lines, cand)
	}

	for _, i := range cand {
		if f.Filter(lines.line(i)) {
			dst = append(dst, i)
		}
	}
	return dst
}

// scratchPool holds the index lists that orFilter and notFilter need, so a call does not allocate
// them.
var scratchPool = sync.Pool{New: func() any {
	s := make([]int32, 0, 1024)
	return &s
}}

func getScratch() *[]int32 {
	return scratchPool.Get().(*[]int32)
}

func putScratch(s *[]int32) {
	*s = (*s)[:0]
	scratchPool.Put(s)
}

func (trueFilter) filterBatch(dst []int32, _ Lines, cand []int32) []int32 {
	return append(dst, cand...)
}

func (l *containsFilter) filterBatch(dst []int32, lines Lines, cand []int32) []int32 {
	if len(l.match) == 0 {
		return append(dst, cand...)
	}

	// bytes.Index has no case-insensitive form, so that case tests each candidate. The whole-buffer
	// search below tests every line, so it applies only when cand lists every line: distinct,
	// ascending indexes, as many as the batch has lines.
	if l.caseInsensitive || len(cand) != lines.count() {
		for _, i := range cand {
			if contains(lines.line(i), l.match, l.caseInsensitive) {
				dst = append(dst, i)
			}
		}
		return dst
	}
	return appendHits(dst, lines, l.match)
}

// appendHits appends to dst the index of every line that holds needle. It searches the whole
// buffer with one bytes.Index per hit, instead of one call per line. needle must not be empty.
func appendHits(dst []int32, lines Lines, needle []byte) []int32 {
	n := lines.count()
	if n == 0 {
		return dst
	}

	var (
		offsets = lines.Offsets
		pos     = int(offsets[0])
		end     = int(offsets[n])
		row     = 0
	)
	for pos+len(needle) <= end {
		i := bytes.Index(lines.Data[pos:end], needle)
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

		// The buffer has no separator between lines, so needle can match across two lines.
		// Such a hit belongs to no line. A real hit can start inside it, so continue at hit+1.
		if hit+len(needle) > int(offsets[lo+1]) {
			pos = hit + 1
			row = lo
			continue
		}

		dst = append(dst, int32(lo))
		pos = int(offsets[lo+1])
		row = lo + 1
	}
	return dst
}

func (f containsAllFilter) filterBatch(dst []int32, lines Lines, cand []int32) []int32 {
	if len(f.matches) == 0 {
		return append(dst, cand...)
	}

	base := len(dst)
	for k := range f.matches {
		if k == 0 {
			dst = f.matches[k].filterBatch(dst, lines, cand)
		} else {
			dst = f.matches[k].filterBatch(dst[:base], lines, dst[base:])
		}
		if len(dst) == base {
			break
		}
	}
	return dst
}

func (a andFilters) filterBatch(dst []int32, lines Lines, cand []int32) []int32 {
	if len(a.filters) == 0 {
		return append(dst, cand...)
	}

	base := len(dst)
	for k, f := range a.filters {
		if k == 0 {
			dst = filterCandidates(f, dst, lines, cand)
		} else {
			// Each later filter tests the lines that the previous one kept. They sit in
			// dst[base:], and the later filter overwrites them in place with its own result.
			dst = filterCandidates(f, dst[:base], lines, dst[base:])
		}
		if len(dst) == base {
			break
		}
	}
	return dst
}

func (a andFilter) filterBatch(dst []int32, lines Lines, cand []int32) []int32 {
	base := len(dst)
	dst = filterCandidates(a.left, dst, lines, cand)
	if len(dst) == base {
		return dst
	}
	return filterCandidates(a.right, dst[:base], lines, dst[base:])
}

func (a orFilter) filterBatch(dst []int32, lines Lines, cand []int32) []int32 {
	left, rest, right := getScratch(), getScratch(), getScratch()
	defer func() {
		putScratch(left)
		putScratch(rest)
		putScratch(right)
	}()

	*left = filterCandidates(a.left, *left, lines, cand)
	// The right side tests only the lines that the left side dropped.
	*rest = appendDifference(*rest, cand, *left)
	*right = filterCandidates(a.right, *right, lines, *rest)
	return appendUnion(dst, *left, *right)
}

func (n notFilter) filterBatch(dst []int32, lines Lines, cand []int32) []int32 {
	hits := getScratch()
	defer putScratch(hits)

	*hits = filterCandidates(n.MatcherFilterer, *hits, lines, cand)
	return appendDifference(dst, cand, *hits)
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
