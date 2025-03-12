package encoding

import (
	"cmp"
	"iter"
	"slices"
)

// The windowing utilities allow for grouping subsections of a file into
// windows of a specified size; for example, given a slices of pages to
// download and a window size of 16MB, pages will be grouped such that the
// first byte of the first page and the last byte of the last page are no more
// than 16MB apart.

// window represents a window of file subsections.
type window[T any] []windowedElement[T]

// Start returns the first element in the window.
func (w window[T]) Start() T {
	var zero T
	if len(w) == 0 {
		return zero
	}
	return w[0].Data
}

// End returns the last element in the window.
func (w window[T]) End() T {
	var zero T
	if len(w) == 0 {
		return zero
	}
	return w[len(w)-1].Data
}

type windowedElement[T any] struct {
	Data     T   // Windowed data.
	Position int // Position of the element in the original slice pre-windowing.
}

type getElementInfo[T any] func(v T) (offset, size uint64)

// iterWindows groups elements into windows of a specified size, returning an
// iterator over the windows. The input slice is not modified.
func iterWindows[T any](elements []T, getInfo getElementInfo[T], windowSize int64) iter.Seq[window[T]] {
	// Sort elements by their start position.
	sortedElements := make(window[T], len(elements))
	for i, element := range elements {
		sortedElements[i] = windowedElement[T]{Data: element, Position: i}
	}
	slices.SortFunc(sortedElements, func(a, b windowedElement[T]) int {
		aOffset, _ := getInfo(a.Data)
		bOffset, _ := getInfo(b.Data)
		return cmp.Compare(aOffset, bOffset)
	})

	return func(yield func(window[T]) bool) {
		var start, end int

		for end < len(sortedElements) {
			startElement := sortedElements[start]
			currentElement := sortedElements[end]

			var (
				startOffset, _     = getInfo(startElement.Data)
				endOffset, endSize = getInfo(currentElement.Data)
			)

			var (
				startByte = startOffset
				endByte   = endOffset + endSize
			)

			switch {
			case endByte-startByte > uint64(windowSize) && start == end:
				// We have an empty window and the element is larger than the current
				// window size. We want to immediately add the page into the window and
				// yield what we have.
				end++

				if !yield(sortedElements[start:end]) {
					return
				}
				start = end

			case endByte-startByte > uint64(windowSize) && start < end:
				// Including end in the window would exceed the window size; we yield
				// everything up to end and start a new window from end.
				//
				// We *do not* increment end here; if we did, we would start with two
				// elements in the next window.
				if !yield(sortedElements[start:end]) {
					return
				}
				start = end

			default:
				// The element fits within the window size; move end forward so it gets
				// included.
				end++
			}
		}

		// Yield all remaining elements.
		if start < len(sortedElements) {
			yield(sortedElements[start:])
		}
	}
}
