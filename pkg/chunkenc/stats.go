package chunkenc

type StatsContext interface {
	AddDecompressedBytes(i int64)
	AddDecompressedLines(i int64)
	AddCompressedBytes(i int64)
	AddDuplicates(i int64)
	AddHeadChunkLines(i int64)
	AddHeadChunkBytes(i int64)
}

type noopStatsContext struct{}

func (noopStatsContext) AddDecompressedBytes(i int64) {}
func (noopStatsContext) AddDecompressedLines(i int64) {}
func (noopStatsContext) AddCompressedBytes(i int64)   {}
func (noopStatsContext) AddDuplicates(i int64)        {}
func (noopStatsContext) AddHeadChunkLines(i int64)    {}
func (noopStatsContext) AddHeadChunkBytes(i int64)    {}
