package tsdb

import (
	"context"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/exp/slices"

	"github.com/grafana/loki/pkg/storage/chunk"
	index_shipper "github.com/grafana/loki/pkg/storage/stores/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

// GetRawFileReaderFunc returns an io.ReadSeeker for reading raw tsdb file from disk
type GetRawFileReaderFunc func() (io.ReadSeeker, error)

func OpenShippableTSDB(p string) (index_shipper.Index, error) {
	id, err := identifierFromPath(p)
	if err != nil {
		return nil, err
	}

	return NewShippableTSDBFile(id)
}

// nolint
// TSDBFile is backed by an actual file and implements the indexshipper/index.Index interface
type TSDBFile struct {
	// reuse Identifier for resolving locations
	Identifier

	// reuse TSDBIndex for reading
	Index

	// to sastisfy Reader() and Close() methods
	getRawFileReader GetRawFileReaderFunc
}

func NewShippableTSDBFile(id Identifier) (*TSDBFile, error) {
	idx, getRawFileReader, err := NewTSDBIndexFromFile(id.Path())
	if err != nil {
		return nil, err
	}

	return &TSDBFile{
		Identifier:       id,
		Index:            idx,
		getRawFileReader: getRawFileReader,
	}, err
}

func (f *TSDBFile) Close() error {
	return f.Index.Close()
}

func (f *TSDBFile) Reader() (io.ReadSeeker, error) {
	return f.getRawFileReader()
}

// nolint
// TSDBIndex is backed by an IndexReader
// and translates the IndexReader to an Index implementation
// It loads the file into memory and doesn't keep a file descriptor open
type TSDBIndex struct {
	reader      IndexReader
	chunkFilter chunk.RequestChunkFilterer
}

// Return the index as well as the underlying raw file reader which isn't exposed as an index
// method but is helpful for building an io.reader for the index shipper
func NewTSDBIndexFromFile(location string) (*TSDBIndex, GetRawFileReaderFunc, error) {
	reader, err := index.NewFileReader(location)
	if err != nil {
		return nil, nil, err
	}

	return NewTSDBIndex(reader), func() (io.ReadSeeker, error) {
		return reader.RawFileReader()
	}, nil
}

func NewTSDBIndex(reader IndexReader) *TSDBIndex {
	return &TSDBIndex{
		reader: reader,
	}
}

func (i *TSDBIndex) Close() error {
	return i.reader.Close()
}

func (i *TSDBIndex) Bounds() (model.Time, model.Time) {
	from, through := i.reader.Bounds()
	return model.Time(from), model.Time(through)
}

func (i *TSDBIndex) SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer) {
	i.chunkFilter = chunkFilter
}

// fn must NOT capture it's arguments. They're reused across series iterations and returned to
// a pool after completion.
func (i *TSDBIndex) forSeries(
	ctx context.Context,
	shard *index.ShardAnnotation,
	fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta),
	matchers ...*labels.Matcher,
) error {
	p, err := PostingsForMatchers(i.reader, shard, matchers...)
	if err != nil {
		return err
	}

	var ls labels.Labels
	chks := ChunkMetasPool.Get()
	defer ChunkMetasPool.Put(chks)

	var filterer chunk.Filterer
	if i.chunkFilter != nil {
		filterer = i.chunkFilter.ForRequest(ctx)
	}

	for p.Next() {
		hash, err := i.reader.Series(p.At(), &ls, &chks)
		if err != nil {
			return err
		}

		// skip series that belong to different shards
		if shard != nil && !shard.Match(model.Fingerprint(hash)) {
			continue
		}

		if filterer != nil && filterer.ShouldFilter(ls) {
			continue
		}

		fn(ls, model.Fingerprint(hash), chks)
	}
	return p.Err()
}

func (i *TSDBIndex) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []ChunkRef, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	queryBounds := newBounds(from, through)
	if res == nil {
		res = ChunkRefsPool.Get()
	}
	res = res[:0]

	if err := i.forSeries(ctx, shard,
		func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {
			// TODO(owen-d): use logarithmic approach
			for _, chk := range chks {

				// current chunk is outside the range of this request
				if !Overlap(queryBounds, chk) {
					continue
				}

				res = append(res, ChunkRef{
					User:        userID, // assumed to be the same, will be enforced by caller.
					Fingerprint: fp,
					Start:       chk.From(),
					End:         chk.Through(),
					Checksum:    chk.Checksum,
				})
			}
		},
		matchers...); err != nil {
		return nil, err
	}

	return res, nil
}

func (i *TSDBIndex) Series(ctx context.Context, _ string, from, through model.Time, res []Series, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]Series, error) {
	queryBounds := newBounds(from, through)
	if res == nil {
		res = SeriesPool.Get()
	}
	res = res[:0]

	if err := i.forSeries(ctx, shard,
		func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {
			// TODO(owen-d): use logarithmic approach
			for _, chk := range chks {
				if Overlap(queryBounds, chk) {
					// this series has at least one chunk in the desired range
					res = append(res, Series{
						Labels:      ls.Copy(),
						Fingerprint: fp,
					})
					break
				}
			}
		},
		matchers...); err != nil {
		return nil, err
	}

	return res, nil
}

func (i *TSDBIndex) LabelNames(_ context.Context, _ string, _, _ model.Time, matchers ...*labels.Matcher) ([]string, error) {
	if len(matchers) == 0 {
		return i.reader.LabelNames()
	}

	return labelNamesWithMatchers(i.reader, matchers...)
}

func (i *TSDBIndex) LabelValues(_ context.Context, _ string, _, _ model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	if len(matchers) == 0 {
		return i.reader.LabelValues(name)
	}
	return labelValuesWithMatchers(i.reader, name, matchers...)
}

func (i *TSDBIndex) Checksum() uint32 {
	return i.reader.Checksum()
}

func (i *TSDBIndex) Identifier(string) SingleTenantTSDBIdentifier {
	lower, upper := i.Bounds()
	return SingleTenantTSDBIdentifier{
		TS:       time.Now(),
		From:     lower,
		Through:  upper,
		Checksum: i.Checksum(),
	}
}

func (i *TSDBIndex) Stats(ctx context.Context, userID string, from, through model.Time, acc IndexStatsAccumulator, shard *index.ShardAnnotation, shouldIncludeChunk shouldIncludeChunk, matchers ...*labels.Matcher) error {
	if err := i.forSeries(ctx, shard,
		func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {
			// TODO(owen-d): use logarithmic approach
			var addedStream bool
			for _, chk := range chks {
				if shouldIncludeChunk(chk) {
					if !addedStream {
						acc.AddStream(fp)
						addedStream = true
					}
					acc.AddChunk(fp, chk)
				}
			}
		},
		matchers...); err != nil {
		return err
	}

	return nil
}

type seriesStats struct {
	numChunks  int
	totalBytes uint32
}

type labelSize struct {
	label string
	bytes uint64
}

type labelStat struct {
	label               string
	chunksPerLabel      int
	chunksPerLabelBytes []uint64
	bytesPerLabel       uint64
	bytesPerValue       map[string]uint64
}

func (i *TSDBIndex) MoreStats(ctx context.Context, matchers ...*labels.Matcher) error {
	series := 0
	totalChunks := 0
	maxChunks := 0
	totalBytes := uint64(0)
	stats := []seriesStats{}
	count1Chunk := 0
	//dataByLabel := map[string]uint64{}
	statsByLabel := map[string]*labelStat{}
	err := i.forSeries(ctx, nil,
		func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {
			series++
			if len(chks) < 2 {
				count1Chunk++
			}
			totalChunks += len(chks)
			if len(chks) > maxChunks {
				maxChunks = len(chks)
			}
			bytes := uint32(0)
			chunkBytes := make([]uint64, 0, len(chks))
			for _, c := range chks {
				b := c.KB << 10
				bytes += b
				chunkBytes = append(chunkBytes, uint64(b))
			}
			totalBytes += uint64(bytes)

			for _, l := range ls {
				//if cur, ok := dataByLabel[l.Name+"|"+l.Value]; ok {
				//	dataByLabel[l.Name+"|"+l.Value] = cur + uint64(bytes)
				//} else {
				//	dataByLabel[l.Name+"|"+l.Value] = uint64(bytes)
				//}
				if cur, ok := statsByLabel[l.Name]; ok {
					statsByLabel[l.Name].bytesPerLabel = cur.bytesPerLabel + uint64(bytes)
					statsByLabel[l.Name].chunksPerLabel = cur.chunksPerLabel + len(chks)
					statsByLabel[l.Name].chunksPerLabelBytes = append(statsByLabel[l.Name].chunksPerLabelBytes, chunkBytes...)
					if cv, ook := statsByLabel[l.Name].bytesPerValue[l.Value]; ook {
						statsByLabel[l.Name].bytesPerValue[l.Value] = cv + uint64(bytes)
					} else {
						statsByLabel[l.Name].bytesPerValue[l.Value] = uint64(bytes)
					}
				} else {
					statsByLabel[l.Name] = &labelStat{
						label:               l.Name,
						bytesPerLabel:       uint64(bytes),
						chunksPerLabel:      len(chks),
						chunksPerLabelBytes: chunkBytes,
						bytesPerValue:       map[string]uint64{l.Value: uint64(bytes)},
					}
				}

			}

			stats = append(stats, seriesStats{
				numChunks:  len(chks),
				totalBytes: bytes,
			})
			//sum := uint32(0)
			//max := uint32(0)
			//min := uint32(math.MaxUint32)
			//
			//slices.SortFunc[index.ChunkMeta](chks, func(a index.ChunkMeta, b index.ChunkMeta) bool {
			//	if a.KB < b.KB {
			//		return true
			//	}
			//	return false
			//})
			//
			//for _, c := range chks {
			//	bytes := c.KB << 10
			//	sum += bytes
			//	if bytes > max {
			//		max = bytes
			//	}
			//	if bytes < min {
			//		min = bytes
			//	}
			//}
			//
			//m := median(chks)
			//p99 := quantile(0.99, chks)
			//
			//fmt.Printf("labels=\"%s\" num=%d max=%d min=%d avg=%f median=%d 99=%f\n", ls.String(), len(chks), max, min, float64(sum)/float64(len(chks)), m, p99)
		},
		matchers...)
	slices.SortFunc[seriesStats](stats, func(a seriesStats, b seriesStats) bool {
		if a.numChunks < b.numChunks {
			return true
		}
		return false
	})

	fmt.Printf("totalSeries=%d totalChunks=%d totalBytes=%s maxChunksPerSeries=%d avgChunksPerSeries=%f medianChunksPerSeries=%d p50numChunks=%f count1=%d\n", series, totalChunks, humanize.Bytes(totalBytes), maxChunks, float64(totalChunks)/float64(series), medianStats(stats), quantileStats(0.50, stats), count1Chunk)

	//labelSizes := make([]labelSize, 0, len(dataByLabel))
	//for l, s := range dataByLabel {
	//	labelSizes = append(labelSizes, labelSize{
	//		label: l,
	//		bytes: s,
	//	})
	//}
	//
	//slices.SortFunc[labelSize](labelSizes, func(a labelSize, b labelSize) bool {
	//	return a.bytes < b.bytes
	//})
	//
	//for _, ls := range labelSizes {
	//	fmt.Printf("label: %s bytes: %s\n", ls.label, humanize.Bytes(ls.bytes))
	//}

	// looking for what labels lead to the most streams with the fewest data

	labelStats := make([]*labelStat, 0, len(statsByLabel))
	for _, s := range statsByLabel {
		labelStats = append(labelStats, s)
	}
	// Sort by bytes
	slices.SortFunc[*labelStat](labelStats, func(a *labelStat, b *labelStat) bool {
		return a.bytesPerLabel < b.bytesPerLabel
	})
	// Sort by num label values
	slices.SortFunc[*labelStat](labelStats, func(a *labelStat, b *labelStat) bool {
		return len(a.bytesPerValue) < len(b.bytesPerValue)
	})

	// add average and median bytes want an even distribution

	for _, s := range labelStats {

		// get all the stats for each value
		chunksForVal := make([]uint64, 0, len(s.bytesPerValue))
		for _, v := range s.bytesPerValue {
			chunksForVal = append(chunksForVal, v)
		}
		slices.SortFunc[uint64](chunksForVal, func(a uint64, b uint64) bool {
			return a < b
		})
		max := uint64(0)
		min := uint64(math.MaxUint64)
		sumBytesForAllVal := uint64(0)
		for _, b := range chunksForVal {
			if b > max {
				max = b
			}
			if b < min {
				min = b
			}
			sumBytesForAllVal += b
		}

		// get stats for all chunks
		avgAllChunks := s.bytesPerLabel / uint64(len(s.chunksPerLabelBytes))
		maxAllChunks := uint64(0)
		minAllChunks := uint64(math.MaxUint64)
		for _, c := range s.chunksPerLabelBytes {
			if c > maxAllChunks {
				maxAllChunks = c
			}
			if c < minAllChunks {
				minAllChunks = c
			}
		}
		slices.SortFunc[uint64](s.chunksPerLabelBytes, func(a uint64, b uint64) bool {
			return a < b
		})
		medAllChunks := medianBytes(s.chunksPerLabelBytes)

		avg := sumBytesForAllVal / uint64(len(chunksForVal))
		//avgPerChunk := avg / uint64(s.chunks)
		med := medianBytes(chunksForVal)
		//medianPerChunk := med / uint64(s.chunks)
		//fmt.Printf("label=%s bytes=%s chunks=%d numLabelVals=%d max=%s min=%s avg=%s avg/chunk=%s median=%s median/chunk=%s\n", s.label, humanize.Bytes(s.bytes), s.chunks, len(s.bytesPerValue), humanize.Bytes(max), humanize.Bytes(min), humanize.Bytes(avg), humanize.Bytes(avgPerChunk), humanize.Bytes(med), humanize.Bytes(medianPerChunk))
		fmt.Println("label name", "total bytes", "total chunks", "number values", "max bytes per value", "min bytes per value", "avg bytes per value", "median bytes per value", "max chunks size", "min chunk size", "avg chunks size", "median chunks size")
		fmt.Printf("label=%s bytes=%s chunks=%d numLabelVals=%d max/val=%s min/val=%s avg/val=%s median/val=%s max/label=%s min/label=%s avg/label=%s med/label=%s\n", s.label, humanize.Bytes(s.bytesPerLabel), s.chunksPerLabel, len(s.bytesPerValue), humanize.Bytes(max), humanize.Bytes(min), humanize.Bytes(avg), humanize.Bytes(med), humanize.Bytes(maxAllChunks), humanize.Bytes(minAllChunks), humanize.Bytes(avgAllChunks), humanize.Bytes(medAllChunks))
	}

	return err
}

func medianBytes(values []uint64) uint64 {
	var median uint64
	l := len(values)
	if l == 0 {
		return 0
	} else if l%2 == 0 {
		median = (values[l/2-1] + values[l/2]) / 2
	} else {
		median = values[l/2]
	}

	//Return as bytes
	return median
}

func medianStats(values []seriesStats) int {
	var median int
	l := len(values)
	if l == 0 {
		return 0
	} else if l%2 == 0 {
		median = (values[l/2-1].numChunks + values[l/2].numChunks) / 2
	} else {
		median = values[l/2].numChunks
	}

	//Return as bytes
	return median

}

func quantileStats(q float64, values []seriesStats) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	if q < 0 {
		return math.Inf(-1)
	}
	if q > 1 {
		return math.Inf(+1)
	}

	n := float64(len(values))
	// When the quantile lies between two samples,
	// we use a weighted average of the two samples.
	rank := q * (n - 1)

	lowerIndex := math.Max(0, math.Floor(rank))
	upperIndex := math.Min(n-1, lowerIndex+1)

	weight := rank - math.Floor(rank)

	lb := float64(values[int(lowerIndex)].numChunks) * (1 - weight)
	ub := float64(values[int(upperIndex)].numChunks) * weight
	return lb + ub
}

func median(values []index.ChunkMeta) uint32 {
	var median uint32
	l := len(values)
	if l == 0 {
		return 0
	} else if l%2 == 0 {
		median = (values[l/2-1].KB + values[l/2].KB) / 2
	} else {
		median = values[l/2].KB
	}

	//Return as bytes
	return median << 10

}

func quantile(q float64, values []index.ChunkMeta) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	if q < 0 {
		return math.Inf(-1)
	}
	if q > 1 {
		return math.Inf(+1)
	}

	n := float64(len(values))
	// When the quantile lies between two samples,
	// we use a weighted average of the two samples.
	rank := q * (n - 1)

	lowerIndex := math.Max(0, math.Floor(rank))
	upperIndex := math.Min(n-1, lowerIndex+1)

	weight := rank - math.Floor(rank)

	lb := (float64(values[int(lowerIndex)].KB) * (1 - weight)) * 1024
	ub := (float64(values[int(upperIndex)].KB) * weight) * 1024
	return lb + ub
}
