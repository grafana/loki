package storage

import (
	"bytes"
	"context"
	"strings"
	"time"

	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

// PostFetcherChunkFilterer filters chunks based on pipeline for log selector expr.
type PostFetcherChunkFilterer interface {
	PostFetchFilter(ctx context.Context, chunks []chunk.Chunk, s config.SchemaConfig) ([]chunk.Chunk, []string, error)
	SetQueryRangeTime(from time.Time, through time.Time, nextChunk *LazyChunk)
	Limit() uint32
	Selector() string
}

// RequestChunkFilterer creates ChunkFilterer for a given request context.
type RequestPostFetcherChunkFilterer interface {
	ForRequest(req *logproto.QueryRequest) PostFetcherChunkFilterer
	ForSampleRequest(req *logproto.SampleQueryRequest) PostFetcherChunkFilterer
}

type requestPostFetcherChunkFilterer struct {
	maxParallelPipelineChunk int
}

func NewRequestPostFetcherChunkFiltererForRequest(maxParallelPipelineChunk int) RequestPostFetcherChunkFilterer {
	return &requestPostFetcherChunkFilterer{maxParallelPipelineChunk: maxParallelPipelineChunk}
}
func (c *requestPostFetcherChunkFilterer) ForRequest(req *logproto.QueryRequest) PostFetcherChunkFilterer {
	return &chunkFiltererByExpr{selector: req.Selector, direction: req.Direction, maxParallelPipelineChunk: c.maxParallelPipelineChunk, limit: req.Limit}
}

func (c *requestPostFetcherChunkFilterer) ForSampleRequest(sampleReq *logproto.SampleQueryRequest) PostFetcherChunkFilterer {
	return &chunkFiltererByExpr{selector: sampleReq.Selector, direction: logproto.FORWARD, isSampleExpr: true, maxParallelPipelineChunk: c.maxParallelPipelineChunk}
}

type chunkFiltererByExpr struct {
	isSampleExpr             bool
	maxParallelPipelineChunk int
	direction                logproto.Direction
	selector                 string
	limit                    uint32
	from                     time.Time
	through                  time.Time
	nextChunk                *LazyChunk
}

func (c *chunkFiltererByExpr) SetQueryRangeTime(from time.Time, through time.Time, nextChunk *LazyChunk) {
	c.from = from
	c.through = through
	c.nextChunk = nextChunk
}

func (c *chunkFiltererByExpr) Limit() uint32 {
	return c.limit
}

func (c *chunkFiltererByExpr) Selector() string {
	return c.selector
}

func (c *chunkFiltererByExpr) PostFetchFilter(ctx context.Context, chunks []chunk.Chunk, s config.SchemaConfig) ([]chunk.Chunk, []string, error) {
	if len(chunks) == 0 {
		return chunks, nil, nil
	}
	postFilterChunkLen := 0
	log, ctx := spanlogger.New(ctx, "Batch.ParallelPostFetchFilter")
	log.Span.LogFields(otlog.Int("chunks", len(chunks)))
	defer func() {
		log.Span.LogFields(otlog.Int("postFilterChunkLen", postFilterChunkLen))
		log.Span.Finish()
	}()

	var postFilterLogSelector syntax.LogSelectorExpr
	var queryLogql string
	if c.isSampleExpr {
		sampleExpr, err := syntax.ParseSampleExpr(c.selector)
		if err != nil {
			return nil, nil, err
		}
		queryLogql = sampleExpr.String()
		postFilterLogSelector, err = sampleExpr.Selector()
		if err != nil {
			return nil, nil, err
		}
	} else {
		logSelector, err := syntax.ParseLogSelector(c.selector, true)
		if err != nil {
			return nil, nil, err
		}
		queryLogql = logSelector.String()
		postFilterLogSelector = logSelector
	}

	if !postFilterLogSelector.HasFilter() {
		return chunks, nil, nil
	}
	preFilterLogql := postFilterLogSelector.String()
	log.Span.SetTag("postFilter", true)
	log.Span.LogFields(otlog.String("logql", queryLogql))
	log.Span.LogFields(otlog.String("postFilterPreFilterLogql", preFilterLogql))
	//removeLineFmtAbel := false
	if strings.Contains(preFilterLogql, "line_format") {
		//removeLineFmt(postFilterLogSelector)
		//removeLineFmtAbel = true
		//trigger ci
		log.Span.LogFields(otlog.String("resultPostFilterPreFilterLogql", postFilterLogSelector.String()))
		return chunks, nil, nil
	}
	//log.Span.SetTag("remove_line_format", removeLineFmtAbel)
	result := make([]chunk.Chunk, 0)
	resultKeys := make([]string, 0)

	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	queuedChunks := make(chan chunk.Chunk)
	go func() {
		for _, c := range chunks {
			queuedChunks <- c
		}
		close(queuedChunks)
	}()
	processedChunks := make(chan *chunkWithKey)
	errors := make(chan error)
	for i := 0; i < min(c.maxParallelPipelineChunk, len(chunks)); i++ {
		go func() {
			for cnk := range queuedChunks {
				if ctx.Err() != nil {
					errors <- ctx.Err()
					continue
				}
				cnkWithKey, err := c.pipelineExecChunk(ctx, cnk, postFilterLogSelector, s)
				if ctx.Err() != nil {
					errors <- ctx.Err()
					continue
				}
				if err != nil {
					errors <- err
				} else {
					processedChunks <- cnkWithKey
				}
			}
		}()
	}
	var lastErr error
	for i := 0; i < len(chunks); i++ {
		select {
		case chunkWithKey := <-processedChunks:
			result = append(result, chunkWithKey.cnk)
			resultKeys = append(resultKeys, chunkWithKey.key)
			if chunkWithKey.isPostFilter {
				postFilterChunkLen++
			}
		case err := <-errors:
			lastErr = err
		}
	}
	return result, resultKeys, lastErr
}

var EmptyLabel = labels.Labels{
	{Name: model.MetricNameLabel, Value: "foo"},
	{Name: "bar", Value: "baz"},
}

func (c *chunkFiltererByExpr) pipelineExecChunk(ctx context.Context, cnk chunk.Chunk, logSelector syntax.LogSelectorExpr, s config.SchemaConfig) (*chunkWithKey, error) {
	pipeline, err := logSelector.Pipeline()
	if err != nil {
		return nil, err
	}
	blocks := 0
	postLen := 0
	log, ctx := spanlogger.New(ctx, "chunkFiltererByExpr.pipelineExecChunk")
	defer func() {
		log.Span.LogFields(otlog.Int("blocks", blocks))
		log.Span.LogFields(otlog.Int("postFilterChunkLen", postLen))
		log.Span.Finish()
	}()

	if cnk.Metric.String() == EmptyLabel.String() {
		return &chunkWithKey{cnk: cnk, key: s.ExternalKey(cnk.ChunkRef), isPostFilter: false}, nil
	}

	streamPipeline := pipeline.ForStream(labels.NewBuilder(cnk.Metric).Del(labels.MetricName).Labels())
	chunkData := cnk.Data
	lazyChunk := LazyChunk{Chunk: cnk}
	newCtr, statCtx := stats.NewContext(ctx)
	lokiChunk := chunkData.(*chunkenc.Facade).LokiChunk()
	startTime, endTime := lokiChunk.Bounds()
	if c.from.Before(startTime) {
		startTime = c.from
	}
	if c.through.After(endTime) {
		endTime = c.through
	}

	iterator, err := lazyChunk.Iterator(statCtx, startTime, endTime, c.direction, streamPipeline, c.nextChunk)
	if err != nil {
		return nil, err
	}

	postFilterChunkData := chunkenc.NewMemChunk(lokiChunk.Encoding(), chunkenc.UnorderedHeadBlockFmt, cnk.Data.Size(), cnk.Data.Size())
	headChunkBytes := int64(0)
	headChunkLine := int64(0)
	decompressedLines := int64(0)
	for iterator.Next() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		entry := iterator.Entry()
		//reset line after post filter.
		//entry.Line = iterator.ProcessLine()
		//fmt.Println("label:", s.ExternalKey(cnk.ChunkRef), ",line accept :", entry.Line)
		err := postFilterChunkData.Append(&entry)
		if err != nil {
			return nil, err
		}
		headChunkBytes += int64(len(entry.Line))
		headChunkLine += int64(1)
		decompressedLines += int64(1)
	}
	if err := postFilterChunkData.Close(); err != nil {
		return nil, err
	}

	ft, et := postFilterChunkData.Bounds()
	firstTime, lastTime := util.RoundToMilliseconds(ft, et)
	postFilterCh := chunk.NewChunk(
		cnk.UserID, cnk.FingerprintModel(), cnk.Metric,
		chunkenc.NewFacade(postFilterChunkData, 0, 0),
		firstTime,
		lastTime,
	)
	chunkSize := postFilterChunkData.BytesSize() + 4*1024 // size + 4kB should be enough room for cortex header
	if err := postFilterCh.EncodeTo(bytes.NewBuffer(make([]byte, 0, chunkSize))); err != nil {
		return nil, err
	}

	decompressedBytes := int64(0)
	compressedBytes := int64(0)
	isPostFilter := false
	postLen = postFilterChunkData.Size()
	if postFilterChunkData.Size() != 0 {
		isPostFilter = true
		decompressedBytes = int64(postFilterChunkData.BytesSize())
		encodedBytes, err := postFilterCh.Encoded()
		if err != nil {
			return nil, err
		}
		compressedBytes = int64(len(encodedBytes))
	}
	chunkStats := newCtr.Ingester().Store.Chunk
	statContext := stats.FromContext(ctx)
	statContext.AddHeadChunkLines(chunkStats.GetHeadChunkLines() - headChunkLine)
	statContext.AddDecompressedLines(chunkStats.GetDecompressedLines() - decompressedLines)
	statContext.AddHeadChunkBytes(chunkStats.GetHeadChunkBytes() - headChunkBytes)
	statContext.AddDecompressedBytes(chunkStats.GetDecompressedBytes() - decompressedBytes)
	statContext.AddCompressedBytes(chunkStats.GetCompressedBytes() - compressedBytes)

	return &chunkWithKey{cnk: postFilterCh, key: s.ExternalKey(cnk.ChunkRef), isPostFilter: isPostFilter}, nil
}

//func removeLineFmt(selector syntax.LogSelectorExpr) {
//	selector.Walk(func(e interface{}) {
//		pipelineExpr, ok := e.(*syntax.PipelineExpr)
//		if !ok {
//			return
//		}
//		stages := pipelineExpr.MultiStages
//		temp := pipelineExpr.MultiStages[:0]
//		for i, stageExpr := range stages {
//			_, ok := stageExpr.(*syntax.LineFmtExpr)
//			if !ok {
//				temp = append(temp, stageExpr)
//				continue
//			}
//			var found bool
//			for j := i; j < len(pipelineExpr.MultiStages); j++ {
//				if _, ok := pipelineExpr.MultiStages[j].(*syntax.LabelParserExpr); ok {
//					found = true
//					break
//				}
//				if _, ok := pipelineExpr.MultiStages[j].(*syntax.LineFilterExpr); ok {
//					found = true
//					break
//				}
//			}
//			if found {
//				temp = append(temp, stageExpr)
//			}
//		}
//		pipelineExpr.MultiStages = temp
//	})
//}

type chunkWithKey struct {
	cnk          chunk.Chunk
	key          string
	isPostFilter bool
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
