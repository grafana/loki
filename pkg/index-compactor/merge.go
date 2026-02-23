package indexcompactor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/scratch"
)

const (
	tocWindowSize      = 12 * time.Hour
	intermediatePrefix = ".intermediate/"
)

type windowedMerger struct {
	cfg        logsobj.BuilderBaseConfig
	windowSize time.Duration

	mu      sync.Mutex
	windows map[time.Time]*windowState
}

type windowState struct {
	mu             sync.Mutex
	builder        *indexobj.Builder
	streamCount    int
	streamPtrCount int
	colPtrCount    int
}

func newWindowedMerger(cfg logsobj.BuilderBaseConfig, windowSize time.Duration) *windowedMerger {
	return &windowedMerger{
		cfg:        cfg,
		windowSize: windowSize,
		windows:    make(map[time.Time]*windowState),
	}
}

func (wm *windowedMerger) getOrCreate(window time.Time) (*windowState, error) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	ws, ok := wm.windows[window]
	if ok {
		return ws, nil
	}
	builder, err := indexobj.NewBuilder(wm.cfg, scratch.NewMemory())
	if err != nil {
		return nil, fmt.Errorf("creating builder for window %s: %w", window.Format(time.RFC3339), err)
	}
	ws = &windowState{builder: builder}
	wm.windows[window] = ws
	return ws, nil
}

func (ws *windowState) appendStream(tenant string, s streams.Stream) (int64, error) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	newID, err := ws.builder.AppendStream(tenant, s)
	if err == nil {
		ws.streamCount++
	}
	return newID, err
}

func (ws *windowState) appendStreamPointer(tenant, path string, section, streamIDInIndex, streamIDInObject int64, startTs, endTs time.Time, lineCount, uncompressedSize int64) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	err := ws.builder.AppendStreamPointer(tenant, path, section, streamIDInIndex, streamIDInObject, startTs, endTs, lineCount, uncompressedSize)
	if err == nil {
		ws.streamPtrCount++
	}
	return err
}

func (ws *windowState) appendColumnIndex(tenant, path string, section int64, columnName string, columnIndex int64, valuesBloomFilter []byte) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	err := ws.builder.AppendColumnIndex(tenant, path, section, columnName, columnIndex, valuesBloomFilter)
	if err == nil {
		ws.colPtrCount++
	}
	return err
}

type splitResult struct {
	window           time.Time
	startTs, endTs   time.Time
	lineCount        int64
	uncompressedSize int64
}

// splitPointer splits a stream pointer across all windows it overlaps,
// interpolating LineCount and UncompressedSize proportionally to duration.
func (wm *windowedMerger) splitPointer(ptr *pointers.SectionPointer) []splitResult {
	totalDuration := ptr.EndTs.Sub(ptr.StartTs)

	if totalDuration <= 0 {
		window := ptr.StartTs.Truncate(wm.windowSize)
		return []splitResult{{
			window:           window,
			startTs:          ptr.StartTs,
			endTs:            ptr.EndTs,
			lineCount:        ptr.LineCount,
			uncompressedSize: ptr.UncompressedSize,
		}}
	}

	firstWindow := ptr.StartTs.Truncate(wm.windowSize)
	lastWindow := ptr.EndTs.Truncate(wm.windowSize)

	if firstWindow.Equal(lastWindow) {
		return []splitResult{{
			window:           firstWindow,
			startTs:          ptr.StartTs,
			endTs:            ptr.EndTs,
			lineCount:        ptr.LineCount,
			uncompressedSize: ptr.UncompressedSize,
		}}
	}

	var results []splitResult

	for w := firstWindow; !w.After(lastWindow); w = w.Add(wm.windowSize) {
		windowEnd := w.Add(wm.windowSize)

		clampedStart := ptr.StartTs
		if w.After(ptr.StartTs) {
			clampedStart = w
		}
		clampedEnd := ptr.EndTs
		if windowEnd.Before(ptr.EndTs) {
			clampedEnd = windowEnd
		}

		overlap := clampedEnd.Sub(clampedStart)
		if overlap <= 0 {
			continue
		}

		fraction := float64(overlap) / float64(totalDuration)
		results = append(results, splitResult{
			window:           w,
			startTs:          clampedStart,
			endTs:            clampedEnd,
			lineCount:        int64(math.Round(float64(ptr.LineCount) * fraction)),
			uncompressedSize: int64(math.Round(float64(ptr.UncompressedSize) * fraction)),
		})
	}

	if len(results) > 1 {
		var totalLC, totalUS int64
		maxIdx := 0
		for i, r := range results {
			totalLC += r.lineCount
			totalUS += r.uncompressedSize
			if r.lineCount > results[maxIdx].lineCount {
				maxIdx = i
			}
		}
		results[maxIdx].lineCount += ptr.LineCount - totalLC
		results[maxIdx].uncompressedSize += ptr.UncompressedSize - totalUS
	}

	return results
}

type bufferedSourceData struct {
	streams  map[string]map[int64]streams.Stream
	pointers map[string][]pointers.SectionPointer
}

func readSourceObject(ctx context.Context, obj *dataobj.Object) (*bufferedSourceData, error) {
	data := &bufferedSourceData{
		streams:  make(map[string]map[int64]streams.Stream),
		pointers: make(map[string][]pointers.SectionPointer),
	}

	for _, section := range obj.Sections().Filter(streams.CheckSection) {
		tenant := section.Tenant
		if _, ok := data.streams[tenant]; !ok {
			data.streams[tenant] = make(map[int64]streams.Stream)
		}

		streamSection, err := streams.Open(ctx, section)
		if err != nil {
			return nil, fmt.Errorf("opening streams section: %w", err)
		}

		rowReader := streams.NewRowReader(streamSection)
		if err := rowReader.Open(ctx); err != nil {
			return nil, fmt.Errorf("opening stream row reader: %w", err)
		}

		buf := make([]streams.Stream, 1024)
		for {
			n, err := rowReader.Read(ctx, buf)
			if err != nil && !errors.Is(err, io.EOF) {
				rowReader.Close()
				return nil, fmt.Errorf("reading streams: %w", err)
			}
			if n == 0 && errors.Is(err, io.EOF) {
				break
			}
			for _, s := range buf[:n] {
				cp := s
				cp.Labels = s.Labels.Copy()
				data.streams[tenant][s.ID] = cp
			}
		}
		rowReader.Close()
	}

	for _, section := range obj.Sections().Filter(pointers.CheckSection) {
		tenant := section.Tenant

		pointersSection, err := pointers.Open(ctx, section)
		if err != nil {
			return nil, fmt.Errorf("opening pointers section: %w", err)
		}

		for result := range pointers.IterSection(ctx, pointersSection) {
			ptr, err := result.Value()
			if err != nil {
				return nil, fmt.Errorf("iterating pointers: %w", err)
			}
			if len(ptr.ValuesBloomFilter) > 0 {
				bloom := make([]byte, len(ptr.ValuesBloomFilter))
				copy(bloom, ptr.ValuesBloomFilter)
				ptr.ValuesBloomFilter = bloom
			}
			data.pointers[tenant] = append(data.pointers[tenant], ptr)
		}
	}

	return data, nil
}

type sectionKey struct {
	path    string
	section int64
}

func objectKey(ctx context.Context, object *dataobj.Object) (string, error) {
	h := sha256.New224()

	reader, err := object.Reader(ctx)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	if _, err := io.Copy(h, reader); err != nil {
		return "", err
	}

	var sumBytes [sha256.Size224]byte
	sum := h.Sum(sumBytes[:0])
	sumStr := hex.EncodeToString(sum[:])

	return fmt.Sprintf("indexes/%s/%s", sumStr[:2], sumStr[2:]), nil
}

// processSourceData routes buffered data from one source object into the
// appropriate per-window builders.
func (wm *windowedMerger) processSourceData(data *bufferedSourceData, logger log.Logger) (streamCount, streamPtrCount, colPtrCount int, err error) {
	sectionWindows := make(map[sectionKey]map[time.Time]struct{})

	// window → tenant → sourceStreamID → mergedStreamID
	windowStreamIDs := make(map[time.Time]map[string]map[int64]int64)

	ensureStream := func(window time.Time, tenant string, sourceStreamID int64) (int64, error) {
		if _, ok := windowStreamIDs[window]; !ok {
			windowStreamIDs[window] = make(map[string]map[int64]int64)
		}
		if _, ok := windowStreamIDs[window][tenant]; !ok {
			windowStreamIDs[window][tenant] = make(map[int64]int64)
		}
		if newID, ok := windowStreamIDs[window][tenant][sourceStreamID]; ok {
			return newID, nil
		}

		srcStream, ok := data.streams[tenant][sourceStreamID]
		if !ok {
			return 0, nil
		}

		ws, wsErr := wm.getOrCreate(window)
		if wsErr != nil {
			return 0, wsErr
		}
		newID, appendErr := ws.appendStream(tenant, srcStream)
		if appendErr != nil {
			return 0, fmt.Errorf("appending stream to window %s: %w", window.Format(time.RFC3339), appendErr)
		}
		windowStreamIDs[window][tenant][sourceStreamID] = newID
		streamCount++
		return newID, nil
	}

	for tenant, ptrs := range data.pointers {
		for i := range ptrs {
			ptr := &ptrs[i]

			switch ptr.PointerKind {
			case pointers.PointerKindStreamIndex:
				for _, sp := range wm.splitPointer(ptr) {
					newStreamID, streamErr := ensureStream(sp.window, tenant, ptr.StreamID)
					if streamErr != nil {
						return 0, 0, 0, streamErr
					}

					ws, wsErr := wm.getOrCreate(sp.window)
					if wsErr != nil {
						return 0, 0, 0, wsErr
					}
					if appendErr := ws.appendStreamPointer(
						tenant, ptr.Path, ptr.Section,
						newStreamID, ptr.StreamIDRef,
						sp.startTs, sp.endTs,
						sp.lineCount, sp.uncompressedSize,
					); appendErr != nil {
						return 0, 0, 0, fmt.Errorf("appending stream pointer: %w", appendErr)
					}
					streamPtrCount++

					key := sectionKey{ptr.Path, ptr.Section}
					if sectionWindows[key] == nil {
						sectionWindows[key] = make(map[time.Time]struct{})
					}
					sectionWindows[key][sp.window] = struct{}{}
				}

			case pointers.PointerKindColumnIndex:
				key := sectionKey{ptr.Path, ptr.Section}
				windows, ok := sectionWindows[key]
				if !ok || len(windows) == 0 {
					level.Warn(logger).Log("msg", "no window for column pointer section", "path", ptr.Path, "section", ptr.Section)
					continue
				}
				for window := range windows {
					ws, wsErr := wm.getOrCreate(window)
					if wsErr != nil {
						return 0, 0, 0, wsErr
					}
					if appendErr := ws.appendColumnIndex(
						tenant, ptr.Path, ptr.Section,
						ptr.ColumnName, ptr.ColumnIndex, ptr.ValuesBloomFilter,
					); appendErr != nil {
						return 0, 0, 0, fmt.Errorf("appending column index: %w", appendErr)
					}
					colPtrCount++
				}
			}
		}
	}

	return streamCount, streamPtrCount, colPtrCount, nil
}

type tocEntry struct {
	path    string
	tenant  string
	minTime time.Time
	maxTime time.Time
}

type intermediateInfo struct {
	key    string
	window time.Time
	toc    []tocEntry
}

func mergeIndexObjects(
	ctx context.Context,
	logger log.Logger,
	readBkt objstore.BucketReader,
	writeBkt objstore.Bucket,
	paths []string,
	cfg Config,
) error {
	cp, err := loadCheckpoint(ctx, writeBkt)
	if err != nil {
		return fmt.Errorf("loading checkpoint: %w", err)
	}
	if cp == nil {
		cp = newCheckpoint()
		err := saveCheckpoint(ctx, writeBkt, cp)
		if err != nil {
			return fmt.Errorf("saving initial checkpoint: %w", err)
		}
	}

	// Filter out paths already compacted in prior runs or processed in an
	// interrupted current run.
	remaining := filterExcluded(paths, cp.CompactedPaths, cp.ProcessedPaths)

	var intermediates []intermediateInfo

	if cp.ScatterComplete {
		level.Info(logger).Log("msg", "resuming from checkpoint, scatter already complete", "intermediates", len(cp.Intermediates))
		intermediates = checkpointToIntermediates(cp.Intermediates)
	} else {
		level.Info(logger).Log(
			"msg", "starting index merge",
			"sources", len(paths),
			"already_compacted", len(cp.CompactedPaths),
			"in_progress", len(cp.ProcessedPaths),
			"remaining", len(remaining),
			"batch_size", cfg.BatchSize,
			"window_size", cfg.WindowSize,
		)

		if len(remaining) == 0 {
			level.Info(logger).Log("msg", "no new index objects to compact")
			return nil
		}

		if err := saveCheckpoint(ctx, writeBkt, cp); err != nil {
			return fmt.Errorf("saving initial checkpoint: %w", err)
		}

		// Capture intermediates from a prior interrupted run before scatterPhase
		// appends to cp.Intermediates, to avoid double-counting.
		priorIntermediates := checkpointToIntermediates(cp.Intermediates)

		scatteredIntermediates, err := scatterPhase(ctx, logger, readBkt, writeBkt, remaining, cfg.WindowSize, cfg.BatchSize, cfg.BuilderConfig, cp)
		if err != nil {
			return fmt.Errorf("scatter phase: %w", err)
		}

		intermediates = append(priorIntermediates, scatteredIntermediates...)

		cp.ScatterComplete = true
		cp.Intermediates = intermediatesToCheckpoint(intermediates)
		if err := saveCheckpoint(ctx, writeBkt, cp); err != nil {
			return fmt.Errorf("saving scatter-complete checkpoint: %w", err)
		}

		level.Info(logger).Log("msg", "scatter complete", "intermediates", len(intermediates))
	}

	allTocEntries, err := gatherPhase(ctx, logger, readBkt, writeBkt, intermediates, cfg.WindowSize, cfg.BuilderConfig)
	if err != nil {
		return fmt.Errorf("gather phase: %w", err)
	}

	// Identify which of the processed paths are prior compaction outputs being
	// re-compacted. Only those get their TOC entries removed and objects deleted.
	recompacted := intersect(cp.ProcessedPaths, cp.OutputPaths)

	if err := writeTocFiles(ctx, logger, writeBkt, allTocEntries, recompacted, cfg.BuilderConfig); err != nil {
		return fmt.Errorf("writing TOC files: %w", err)
	}

	// Collect output paths produced by this run.
	newOutputPaths := make(map[string]struct{})
	for _, e := range allTocEntries {
		newOutputPaths[e.path] = struct{}{}
	}

	// Delete re-compacted output objects (prior compaction outputs now superseded).
	if err := deleteObjects(ctx, logger, writeBkt, recompacted); err != nil {
		return fmt.Errorf("deleting re-compacted outputs: %w", err)
	}

	// Clean up intermediate objects from the scatter phase.
	if err := deleteIntermediates(ctx, logger, writeBkt, intermediates); err != nil {
		return fmt.Errorf("deleting intermediates: %w", err)
	}

	cp.finalizeRun(newOutputPaths, recompacted)
	if err := saveCheckpoint(ctx, writeBkt, cp); err != nil {
		return fmt.Errorf("saving finalized checkpoint: %w", err)
	}

	level.Info(logger).Log("msg", "compaction run complete",
		"compacted_total", len(cp.CompactedPaths),
		"output_total", len(cp.OutputPaths),
		"recompacted", len(recompacted),
		"output_entries", len(allTocEntries),
	)

	return nil
}

func filterExcluded(paths []string, excludeSets ...map[string]struct{}) []string {
	var remaining []string
	for _, p := range paths {
		excluded := false
		for _, set := range excludeSets {
			if _, ok := set[p]; ok {
				excluded = true
				break
			}
		}
		if !excluded {
			remaining = append(remaining, p)
		}
	}
	return remaining
}

func checkpointToIntermediates(records []IntermediateRecord) []intermediateInfo {
	infos := make([]intermediateInfo, len(records))
	for i, r := range records {
		var entries []tocEntry
		for _, t := range r.TOC {
			entries = append(entries, tocEntry{path: t.Path, tenant: t.Tenant, minTime: t.MinTime, maxTime: t.MaxTime})
		}
		infos[i] = intermediateInfo{key: r.Key, window: r.Window, toc: entries}
	}
	return infos
}

func intermediatesToCheckpoint(infos []intermediateInfo) []IntermediateRecord {
	records := make([]IntermediateRecord, len(infos))
	for i, info := range infos {
		var tocRecords []TOCRecord
		for _, t := range info.toc {
			tocRecords = append(tocRecords, TOCRecord{Path: t.path, Tenant: t.tenant, MinTime: t.minTime, MaxTime: t.maxTime})
		}
		records[i] = IntermediateRecord{Key: info.key, Window: info.window, TOC: tocRecords}
	}
	return records
}

func scatterPhase(
	ctx context.Context,
	logger log.Logger,
	reader objstore.BucketReader,
	bkt objstore.Bucket,
	paths []string,
	windowSize time.Duration,
	batchSize int,
	cfg logsobj.BuilderBaseConfig,
	cp *Checkpoint,
) ([]intermediateInfo, error) {
	var allIntermediates []intermediateInfo
	numWorkers := runtime.GOMAXPROCS(0)

	for batchStart := 0; batchStart < len(paths); batchStart += batchSize {
		batchEnd := batchStart + batchSize
		if batchEnd > len(paths) {
			batchEnd = len(paths)
		}
		batch := paths[batchStart:batchEnd]

		level.Info(logger).Log("msg", "scatter batch", "from", batchStart+1, "to", batchEnd, "total", len(paths))

		merger := newWindowedMerger(cfg, windowSize)

		g, gCtx := errgroup.WithContext(ctx)
		g.SetLimit(numWorkers)

		for i, path := range batch {
			globalIdx := batchStart + i + 1
			g.Go(func() error {
				data, err := downloadObject(gCtx, reader, path)
				if err != nil {
					if bkt.IsObjNotFoundErr(err) {
						level.Warn(logger).Log("msg", "source object not found, skipping", "path", path, "index", globalIdx, "total", len(paths))
						return nil
					}
					return fmt.Errorf("downloading %s: %w", path, err)
				}

				obj, err := dataobj.FromReaderAt(bytes.NewReader(data), int64(len(data)))
				if err != nil {
					return fmt.Errorf("parsing %s: %w", path, err)
				}

				srcData, err := readSourceObject(gCtx, obj)
				if err != nil {
					return fmt.Errorf("reading source object %s: %w", path, err)
				}
				if _, _, _, err := merger.processSourceData(srcData, logger); err != nil {
					return fmt.Errorf("processing source object %s: %w", path, err)
				}
				level.Debug(logger).Log("msg", "processed source object", "path", path, "index", globalIdx, "total", len(paths), "size", humanize.Bytes(uint64(len(data))))
				return nil
			})
		}

		if err := g.Wait(); err != nil {
			return nil, fmt.Errorf("scatter batch %d–%d: %w", batchStart+1, batchEnd, err)
		}

		var batchIntermediates []intermediateInfo
		for window, ws := range merger.windows {
			timeRanges := ws.builder.TimeRanges()

			flushedObj, closer, err := ws.builder.Flush()
			if err != nil {
				return nil, fmt.Errorf("flushing intermediate %s: %w", window.Format(time.RFC3339), err)
			}

			key, err := objectKey(ctx, flushedObj)
			if err != nil {
				closer.Close()
				return nil, fmt.Errorf("computing object key: %w", err)
			}

			if err := writeObject(ctx, bkt, intermediatePrefix+key, flushedObj); err != nil {
				closer.Close()
				return nil, fmt.Errorf("writing intermediate: %w", err)
			}
			closer.Close()

			var entries []tocEntry
			for _, tr := range timeRanges {
				entries = append(entries, tocEntry{path: key, tenant: tr.Tenant, minTime: tr.MinTime, maxTime: tr.MaxTime})
			}

			info := intermediateInfo{key: key, window: window, toc: entries}
			batchIntermediates = append(batchIntermediates, info)

			level.Info(logger).Log(
				"msg", "flushed intermediate window",
				"window", window.Format("2006-01-02 15:04"),
				"streams", ws.streamCount,
				"stream_ptrs", ws.streamPtrCount,
				"col_ptrs", ws.colPtrCount,
				"size", humanize.Bytes(uint64(flushedObj.Size())),
			)
		}

		allIntermediates = append(allIntermediates, batchIntermediates...)

		for _, p := range batch {
			cp.ProcessedPaths[p] = struct{}{}
		}
		cp.Intermediates = append(cp.Intermediates, intermediatesToCheckpoint(batchIntermediates)...)
		if err := saveCheckpoint(ctx, bkt, cp); err != nil {
			return nil, fmt.Errorf("saving checkpoint after batch %d–%d: %w", batchStart+1, batchEnd, err)
		}

		level.Info(logger).Log("msg", "checkpoint saved", "processed", len(cp.ProcessedPaths), "intermediates", len(cp.Intermediates))
	}

	return allIntermediates, nil
}

func gatherPhase(
	ctx context.Context,
	logger log.Logger,
	readBkt objstore.BucketReader,
	writeBkt objstore.Bucket,
	intermediates []intermediateInfo,
	windowSize time.Duration,
	cfg logsobj.BuilderBaseConfig,
) ([]tocEntry, error) {
	byWindow := make(map[time.Time][]intermediateInfo)
	for _, info := range intermediates {
		byWindow[info.window] = append(byWindow[info.window], info)
	}

	windows := make([]time.Time, 0, len(byWindow))
	for w := range byWindow {
		windows = append(windows, w)
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i].Before(windows[j]) })

	if len(windows) == 0 {
		return nil, nil
	}

	globalMin := windows[0].Truncate(windowSize)
	globalMax := windows[len(windows)-1].Add(windowSize)
	numWorkers := runtime.GOMAXPROCS(0)

	var allTocEntries []tocEntry

	for chunkStart := globalMin; chunkStart.Before(globalMax); chunkStart = chunkStart.Add(windowSize) {
		chunkEnd := chunkStart.Add(windowSize)

		var chunkInfos []intermediateInfo
		for _, w := range windows {
			if !w.Before(chunkStart) && w.Before(chunkEnd) {
				chunkInfos = append(chunkInfos, byWindow[w]...)
			}
		}
		if len(chunkInfos) == 0 {
			continue
		}

		level.Info(logger).Log(
			"msg", "gather chunk",
			"chunk_start", chunkStart.Format("2006-01-02 15:04"),
			"chunk_end", chunkEnd.Format("15:04"),
			"intermediates", len(chunkInfos),
		)

		merger := newWindowedMerger(cfg, windowSize)

		g, gCtx := errgroup.WithContext(ctx)
		g.SetLimit(numWorkers)

		for _, info := range chunkInfos {
			g.Go(func() error {
				data, err := downloadObject(gCtx, writeBkt, intermediatePrefix+info.key)
				if err != nil {
					return fmt.Errorf("reading intermediate %s: %w", info.key, err)
				}

				obj, err := dataobj.FromReaderAt(bytes.NewReader(data), int64(len(data)))
				if err != nil {
					return fmt.Errorf("parsing intermediate %s: %w", info.key, err)
				}

				srcData, err := readSourceObject(gCtx, obj)
				if err != nil {
					return fmt.Errorf("reading intermediate source %s: %w", info.key, err)
				}
				if _, _, _, err := merger.processSourceData(srcData, logger); err != nil {
					return fmt.Errorf("processing intermediate %s: %w", info.key, err)
				}
				return nil
			})
		}

		if err := g.Wait(); err != nil {
			return nil, fmt.Errorf("gather chunk %s–%s: %w", chunkStart.Format("2006-01-02 15:04"), chunkEnd.Format("15:04"), err)
		}

		for window, ws := range merger.windows {
			timeRanges := ws.builder.TimeRanges()

			flushedObj, closer, err := ws.builder.Flush()
			if err != nil {
				return nil, fmt.Errorf("flushing final %s: %w", window.Format(time.RFC3339), err)
			}

			key, err := objectKey(ctx, flushedObj)
			if err != nil {
				closer.Close()
				return nil, fmt.Errorf("computing object key: %w", err)
			}

			if err := writeObject(ctx, writeBkt, key, flushedObj); err != nil {
				closer.Close()
				return nil, fmt.Errorf("writing final object: %w", err)
			}
			closer.Close()

			for _, tr := range timeRanges {
				allTocEntries = append(allTocEntries, tocEntry{
					path: key, tenant: tr.Tenant, minTime: tr.MinTime, maxTime: tr.MaxTime,
				})
			}

			level.Info(logger).Log(
				"msg", "flushed final window",
				"window", window.Format("2006-01-02 15:04"),
				"streams", ws.streamCount,
				"stream_ptrs", ws.streamPtrCount,
				"col_ptrs", ws.colPtrCount,
				"size", humanize.Bytes(uint64(flushedObj.Size())),
				"key", key,
			)
		}
	}

	return allTocEntries, nil
}

func tocPath(prefix string, window time.Time) string {
	name := strings.ReplaceAll(window.UTC().Format(time.RFC3339), ":", "_")
	return prefix + name + ".toc"
}

func writeTocFiles(ctx context.Context, logger log.Logger, bkt objstore.Bucket, allTocEntries []tocEntry, removePaths map[string]struct{}, cfg logsobj.BuilderBaseConfig) error {
	tocWindowEntries := make(map[time.Time][]tocEntry)
	for _, entry := range allTocEntries {
		minW := entry.minTime.Truncate(tocWindowSize).UTC()
		maxW := entry.maxTime.Truncate(tocWindowSize).UTC()
		for w := minW; !w.After(maxW); w = w.Add(tocWindowSize) {
			tocWindowEntries[w] = append(tocWindowEntries[w], entry)
		}
	}

	for window, entries := range tocWindowEntries {
		tocRelPath := tocPath(metastore.TocPrefix, window)

		err := bkt.GetAndReplace(ctx, tocRelPath, func(existing io.ReadCloser) (io.ReadCloser, error) {
			if existing != nil {
				defer existing.Close()
			}

			tocBuilder, err := indexobj.NewBuilder(cfg, scratch.NewMemory())
			if err != nil {
				return nil, fmt.Errorf("creating TOC builder: %w", err)
			}

			if existing != nil {
				data, readErr := io.ReadAll(existing)
				if readErr != nil {
					return nil, fmt.Errorf("reading existing TOC: %w", readErr)
				}
				obj, parseErr := dataobj.FromReaderAt(bytes.NewReader(data), int64(len(data)))
				if parseErr != nil {
					return nil, fmt.Errorf("parsing existing TOC: %w", parseErr)
				}
				if replayErr := replayTocEntries(ctx, obj, tocBuilder, removePaths); replayErr != nil {
					return nil, fmt.Errorf("replaying existing TOC: %w", replayErr)
				}
			}

			for _, entry := range entries {
				if appendErr := tocBuilder.AppendIndexPointer(entry.tenant, entry.path, entry.minTime, entry.maxTime); appendErr != nil {
					return nil, fmt.Errorf("appending index pointer: %w", appendErr)
				}
			}

			tocObj, closer, flushErr := tocBuilder.Flush()
			if flushErr != nil {
				return nil, fmt.Errorf("flushing TOC builder: %w", flushErr)
			}

			reader, readerErr := tocObj.Reader(ctx)
			if readerErr != nil {
				closer.Close()
				return nil, fmt.Errorf("getting TOC reader: %w", readerErr)
			}

			return &mergeReadCloser{
				ReadCloser: reader,
				extra:      closer,
			}, nil
		})
		if err != nil {
			return fmt.Errorf("merging TOC %s: %w", tocRelPath, err)
		}

		level.Info(logger).Log(
			"msg", "merged TOC file",
			"key", tocRelPath,
			"new_entries", len(entries),
		)
	}

	return nil
}

// replayTocEntries copies index pointers from an existing TOC object into the
// builder, skipping any whose path is in the exclude set.
func replayTocEntries(ctx context.Context, obj *dataobj.Object, builder *indexobj.Builder, exclude map[string]struct{}) error {
	var reader indexpointers.RowReader
	defer reader.Close()

	buf := make([]indexpointers.IndexPointer, 256)

	for _, section := range obj.Sections().Filter(indexpointers.CheckSection) {
		sec, err := indexpointers.Open(ctx, section)
		if err != nil {
			return fmt.Errorf("opening section: %w", err)
		}
		tenantID := section.Tenant
		reader.Reset(sec)
		if err := reader.Open(ctx); err != nil {
			return fmt.Errorf("opening index pointers reader: %w", err)
		}
		for {
			n, readErr := reader.Read(ctx, buf)
			for _, ptr := range buf[:n] {
				if _, skip := exclude[ptr.Path]; skip {
					continue
				}
				if appendErr := builder.AppendIndexPointer(tenantID, ptr.Path, ptr.StartTs, ptr.EndTs); appendErr != nil {
					return fmt.Errorf("replaying index pointer: %w", appendErr)
				}
			}
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					break
				}
				return fmt.Errorf("reading index pointers: %w", readErr)
			}
		}
	}
	return nil
}

// mergeReadCloser wraps an io.ReadCloser and also closes an extra io.Closer
// (the dataobj closer) when done.
type mergeReadCloser struct {
	io.ReadCloser
	extra io.Closer
}

func (m *mergeReadCloser) Close() error {
	return errors.Join(m.ReadCloser.Close(), m.extra.Close())
}

func writeObject(ctx context.Context, bucket objstore.Bucket, key string, obj *dataobj.Object) error {
	reader, err := obj.Reader(ctx)
	if err != nil {
		return fmt.Errorf("reading object: %w", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("reading object bytes: %w", err)
	}

	if err := bucket.Upload(ctx, key, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("uploading %s: %w", key, err)
	}
	return nil
}

func downloadObject(ctx context.Context, bucket objstore.BucketReader, path string) ([]byte, error) {
	r, err := bucket.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func intersect(a, b map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{})
	for k := range a {
		if _, ok := b[k]; ok {
			result[k] = struct{}{}
		}
	}
	return result
}

func deleteObjects(ctx context.Context, logger log.Logger, bucket objstore.Bucket, paths map[string]struct{}) error {
	for p := range paths {
		if err := bucket.Delete(ctx, p); err != nil {
			if bucket.IsObjNotFoundErr(err) {
				level.Debug(logger).Log("msg", "object already deleted", "path", p)
				continue
			}
			return fmt.Errorf("deleting %s: %w", p, err)
		}
		level.Debug(logger).Log("msg", "deleted object", "path", p)
	}
	return nil
}

func deleteIntermediates(ctx context.Context, logger log.Logger, bucket objstore.Bucket, intermediates []intermediateInfo) error {
	seen := make(map[string]struct{})
	for _, info := range intermediates {
		if _, ok := seen[info.key]; ok {
			continue
		}
		seen[info.key] = struct{}{}
		if err := bucket.Delete(ctx, intermediatePrefix+info.key); err != nil {
			if bucket.IsObjNotFoundErr(err) {
				continue
			}
			return fmt.Errorf("deleting intermediate %s: %w", info.key, err)
		}
		level.Debug(logger).Log("msg", "deleted intermediate", "key", info.key)
	}
	return nil
}
