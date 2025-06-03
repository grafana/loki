// The windowing package provides utilities for grouping elements into windows
// of a specified size.
//
// Windowing is typically used for batch downloading pages from a data object,
// where you want to download up to a certain number of bytes per request. This
// results in fewer requests to the object storage backend, but can result in
// some "garbage" data being downloaded if there are gaps between elements in a
// window.
package windowing

import (
	"cmp"
	"iter"
	"slices"
)

const (
	// S3WindowSize specifies the maximum amount of data to download at once from
	// S3. 16MB is chosen based on S3's [recommendations] for Byte-Range fetches,
	// which recommends either 8MB or 16MB.
	//
	// As windowing is designed to reduce the number of requests made to object
	// storage, 16MB is chosen over 8MB, as it will lead to fewer requests.
	//
	// [recommendations]: https://docs.aws.amazon.com/whitepapers/latest/s3-optimizing-performance-best-practices/use-byte-range-fetches.html
	S3WindowSize = 64_000_000
)

// Element is a single element within a window.
type Element[T any] struct {
	Data  T   // Windows data.
	Index int // Index of the element in the original slice pre-windowing.
}

// A Window represents a window of elements.
type Window[T any] []Element[T]

// Start returns the first element in the window. If the window is empty, Start
// returns the zero value of T.
func (w Window[T]) Start() T {
	if len(w) == 0 {
		var zero T
		return zero
	}
	return w[0].Data
}

// End returns the last element in the window. If the window is empty, End
// returns the zero value of T.
func (w Window[T]) End() T {
	if len(w) == 0 {
		var zero T
		return zero
	}
	return w[len(w)-1].Data
}

// GetRegion is a function that returns the positional information about value
// for windowing. The function should return the offset of T and the byte size
// of T.
type GetRegion[T any] func(value T) (offset, size uint64)

// Iter iterates through windows of values, where each window is at most
// windowSize bytes. Elements in the window are sorted by offset. The position
// of the element in the values slice can be retrieved from the Index field of
// the returned elements.
//
// Iter has undefined behaviour for values whose regions overlap.
func Iter[T any](values []T, getRegion GetRegion[T], windowSize int64) iter.Seq[Window[T]] {
	// Sort elements by their start position.
	sortedElements := make(Window[T], len(values))
	for i, element := range values {
		sortedElements[i] = Element[T]{Data: element, Index: i}
	}
	slices.SortFunc(sortedElements, func(a, b Element[T]) int {
		aOffset, _ := getRegion(a.Data)
		bOffset, _ := getRegion(b.Data)
		return cmp.Compare(aOffset, bOffset)
	})

	return func(yield func(Window[T]) bool) {
		var start, end int

		for end < len(sortedElements) {
			startElement := sortedElements[start]
			currentElement := sortedElements[end]

			var (
				startOffset, _     = getRegion(startElement.Data)
				endOffset, endSize = getRegion(currentElement.Data)
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
