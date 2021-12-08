package chunkenc

type fakeStatsContext struct {
	decompressedBytes int64
	decompressedLines int64
	compressedBytes   int64
	duplicates        int64
	headChunkLines    int64
	headChunkBytes    int64
}

func (f *fakeStatsContext) AddDecompressedBytes(i int64) { f.decompressedBytes += i }
func (f *fakeStatsContext) AddDecompressedLines(i int64) { f.decompressedLines += i }
func (f *fakeStatsContext) AddCompressedBytes(i int64)   { f.compressedBytes += i }
func (f *fakeStatsContext) AddDuplicates(i int64)        { f.duplicates += i }
func (f *fakeStatsContext) AddHeadChunkLines(i int64)    { f.headChunkLines += i }
func (f *fakeStatsContext) AddHeadChunkBytes(i int64)    { f.headChunkBytes += i }
