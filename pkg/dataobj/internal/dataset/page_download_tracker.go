package dataset

// NewOnPagesDownloadedFunc returns a [RowReaderStatsTracker] that calls fn with
// the compressed and the uncompressed size of each batch of pages that the
// reader downloads. The tracker ignores the other reader stats.
//
// NewOnPagesDownloadedFunc returns nil if fn is nil.
//
// Packages outside pkg/dataobj cannot import this package, so postings and
// sortmerge re-export this function.
func NewOnPagesDownloadedFunc(fn func(compressedBytes, uncompressedBytes int64)) RowReaderStatsTracker {
	if fn == nil {
		return nil
	}
	return onPagesDownloadedFunc(fn)
}

type onPagesDownloadedFunc func(compressedBytes, uncompressedBytes int64)

func (onPagesDownloadedFunc) OnColumnPredicateBuilt(Column, ColumnReadPageStats) {}

func (f onPagesDownloadedFunc) OnPagesDownloaded(compressedBytes, uncompressedBytes int64) {
	f(compressedBytes, uncompressedBytes)
}
