// toc-graph is a throwaway tool to read Table of Contents (TOC) files from GCS,
// extract referenced objects and their time ranges, and emit an interactive HTML timeline.
//
// Usage:
//
//	go run ./tools/toc-graph -bucket=my-bucket [-start=...] [-end=...] -output=toc-graph.html
//
// Then open toc-graph.html in a browser.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/dustin/go-humanize"
	glog "github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/gcs"
	"github.com/thanos-io/objstore/providers/s3"
)

const (
	tocWindowSize = 12 * time.Hour
	parallelism   = 32
)

// rangeEntry holds a time range and the TOC file that references it.
type rangeEntry struct {
	start, end time.Time
	toc        string
}

// tocPath computes the well-known path for a TOC file at the given window start.
// Mirrors metastore.tableOfContentsPath.
func tocPath(prefix string, window time.Time) string {
	name := strings.ReplaceAll(window.UTC().Format(time.RFC3339), ":", "_")
	return prefix + name + ".toc"
}

// tocPathsForRange returns the deterministic TOC file paths covering [start, end].
func tocPathsForRange(prefix string, start, end time.Time) []string {
	minWindow := start.Truncate(tocWindowSize).UTC()
	maxWindow := end.Truncate(tocWindowSize).UTC()

	var paths []string
	for w := minWindow; !w.After(maxWindow); w = w.Add(tocWindowSize) {
		paths = append(paths, tocPath(prefix, w))
	}
	return paths
}

func main() {
	bucketName := flag.String("bucket", "", "Bucket name")
	provider := flag.String("provider", "gcs", "Object storage provider: 'gcs' or 's3'")
	s3Endpoint := flag.String("s3-endpoint", "", "S3 endpoint (default: derived from -s3-region)")
	s3Region := flag.String("s3-region", "", "S3 region (required for s3 provider)")
	indexPrefix := flag.String("index-prefix", "", "Index storage prefix (e.g. index/). TOC path will be <prefix>tocs/")
	now := time.Now().UTC()
	startFlag := flag.String("start", now.Add(-30*24*time.Hour).Format(time.RFC3339), "Start time (RFC3339)")
	endFlag := flag.String("end", now.Format(time.RFC3339), "End time (RFC3339)")
	outputPath := flag.String("output", "toc-graph.html", "Output HTML file path")
	mode := flag.String("mode", "toc", "Mode: 'toc' | 'streams' | 'long-columns'")
	flag.Parse()

	if *bucketName == "" {
		log.Fatal("-bucket is required")
	}

	start, err := time.Parse(time.RFC3339, *startFlag)
	if err != nil {
		log.Fatalf("bad -start: %v", err)
	}
	end, err := time.Parse(time.RFC3339, *endFlag)
	if err != nil {
		log.Fatalf("bad -end: %v", err)
	}

	ctx := context.Background()

	bkt, err := newBucket(ctx, *provider, *bucketName, *s3Endpoint, *s3Region)
	if err != nil {
		log.Fatalf("bucket: %v", err)
	}
	prefixedBkt := objstore.NewPrefixedBucket(bkt, "dataobj/index/v0")
	var reader objstore.BucketReader = prefixedBkt

	prefix := metastore.TocPrefix
	if *indexPrefix != "" {
		prefix = strings.TrimSuffix(*indexPrefix, "/") + "/" + prefix
	}

	byPath := readTOCEntries(ctx, reader, prefixedBkt, prefix, start, end)

	var html string
	switch *mode {
	case "toc":
		html = buildHTML("TOC Object Time Ranges", byPath)
	case "streams":
		streamRanges := readStreamHourlyBuckets(ctx, reader, prefixedBkt, byPath)
		html = buildHTML("Stream Hourly Coverage", streamRanges)
	case "long-columns":
		scanLongColumnNames(ctx, reader, prefixedBkt, byPath)
		return
	case "stream-labels":
		scanStreamLabels(ctx, reader, prefixedBkt, byPath)
		return
	default:
		log.Fatalf("unknown -mode %q (use 'toc', 'streams', 'long-columns', or 'stream-labels')", *mode)
	}

	if err := os.WriteFile(*outputPath, []byte(html), 0o644); err != nil {
		log.Fatalf("Write %s: %v", *outputPath, err)
	}
	log.Printf("Wrote %s (%d entries)", *outputPath, len(byPath))
}

func newBucket(ctx context.Context, provider, bucket, s3Endpoint, s3Region string) (objstore.Bucket, error) {
	logger := glog.NewNopLogger()
	switch provider {
	case "gcs":
		return gcs.NewBucketWithConfig(ctx, logger, gcs.Config{Bucket: bucket}, "toc-graph", nil)
	case "s3":
		if s3Region == "" {
			return nil, fmt.Errorf("-s3-region is required for s3 provider")
		}
		endpoint := s3Endpoint
		if endpoint == "" {
			endpoint = fmt.Sprintf("s3.%s.amazonaws.com", s3Region)
		}
		return s3.NewBucketWithConfig(logger, s3.Config{
			Bucket:     bucket,
			Endpoint:   endpoint,
			Region:     s3Region,
			AWSSDKAuth: true,
		}, "toc-graph", nil)
	default:
		return nil, fmt.Errorf("unknown provider %q (use 'gcs' or 's3')", provider)
	}
}

// readTOCEntries reads all TOC files in [start, end] and returns index pointer ranges grouped by path.
func readTOCEntries(ctx context.Context, reader objstore.BucketReader, prefixedBkt objstore.Bucket, prefix string, start, end time.Time) map[string][]rangeEntry {
	tocPaths := tocPathsForRange(prefix, start, end)
	log.Printf("Computed %d TOC path(s) for %s – %s", len(tocPaths), start.Format(time.RFC3339), end.Format(time.RFC3339))

	byPath := make(map[string][]rangeEntry)

	for _, tocPath := range tocPaths {
		obj, err := readSmallObject(ctx, reader, tocPath)
		if err != nil {
			if prefixedBkt.IsObjNotFoundErr(err) {
				log.Printf("SKIP: %s (not found)", tocPath)
				continue
			}
			log.Fatalf("ERROR reading %s: %v", tocPath, err)
		}

		for result := range indexpointers.Iter(ctx, obj) {
			ptr, err := result.Value()
			if err != nil {
				log.Fatalf("ERROR iterating %s: %v", tocPath, err)
			}
			key := ptr.Path
			if end.Before(ptr.StartTs.UTC()) || start.After(ptr.EndTs.UTC()) {
				continue
			}
			byPath[key] = append(byPath[key], rangeEntry{
				start: ptr.StartTs.UTC(),
				end:   ptr.EndTs.UTC(),
				toc:   tocPath,
			})
		}
	}
	return byPath
}

// readStreamHourlyBuckets fetches each referenced object in parallel, parses its
// streams section, and builds hourly-bucketed time ranges. Hours without data produce gaps.
func readStreamHourlyBuckets(ctx context.Context, reader objstore.BucketReader, prefixedBkt objstore.Bucket, tocEntries map[string][]rangeEntry) map[string][]rangeEntry {
	paths := sortedKeys(tocEntries)
	log.Printf("Fetching streams from %d objects (parallel=%d)", len(paths), parallelism)

	var mu sync.Mutex
	results := make(map[string][]rangeEntry, len(paths))
	done := int64(0)

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(parallelism)

	for _, path := range paths {
		g.Go(func() error {
			obj, err := dataobj.FromBucket(ctx, reader, path)
			if err != nil {
				return fmt.Errorf("parsing %s: %w", path, err)
			}

			var hours map[time.Time]struct{}

			for res := range streams.Iter(ctx, obj) {
				stream, err := res.Value()
				if err != nil {
					return fmt.Errorf("iterating streams in %s: %w", path, err)
				}
				h := stream.MinTimestamp.UTC().Truncate(time.Hour)
				end := stream.MaxTimestamp.UTC()
				for !h.After(end) {
					hours[h] = struct{}{}
					h = h.Add(time.Hour)
				}
			}

			ranges := hourSetToRanges(hours, path)

			mu.Lock()
			results[path] = ranges
			n := atomic.AddInt64(&done, 1)
			mu.Unlock()

			if n%50 == 0 || n == int64(len(paths)) {
				log.Printf("  progress: %d/%d objects", n, len(paths))
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		log.Fatalf("ERROR: %v", err)
	}
	return results
}

const longColumnThreshold = 100

// scanLongColumnNames fetches each referenced object in parallel, reads its pointers
// sections, and reports column names longer than longColumnThreshold characters.
func scanLongColumnNames(ctx context.Context, reader objstore.BucketReader, prefixedBkt objstore.Bucket, tocEntries map[string][]rangeEntry) {
	paths := sortedKeys(tocEntries)
	paths = paths[:10]
	log.Printf("Scanning pointers in %d objects for long column names (>%d chars, parallel=%d)", len(paths), longColumnThreshold, parallelism)

	type hit struct {
		objPath    string
		columnName string
	}

	type perObjectStats struct {
		path       string
		uniqueCols int
		longCols   int
	}

	var (
		mu       sync.Mutex
		hits     []hit
		total    int64
		longCnt  int64
		done     int64
		objStats []perObjectStats
	)

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(parallelism)

	for _, path := range paths {
		g.Go(func() error {
			data, err := downloadObject(ctx, reader, path)
			if err != nil {
				if prefixedBkt.IsObjNotFoundErr(err) {
					log.Printf("  SKIP: %s (not found)", path)
					return nil
				}
				return fmt.Errorf("downloading %s: %w", path, err)
			}

			obj, err := dataobj.FromReaderAt(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				return fmt.Errorf("parsing %s: %w", path, err)
			}

			var localTotal, localLong int64
			var localHits []hit
			uniqueNames := make(map[string]struct{})

			for res := range pointers.Iter(ctx, obj) {
				ptr, err := res.Value()
				if err != nil {
					return fmt.Errorf("iterating pointers in %s: %w", path, err)
				}
				if ptr.ColumnName == "" {
					continue
				}
				localTotal++
				uniqueNames[ptr.ColumnName] = struct{}{}
				if len(ptr.ColumnName) > longColumnThreshold {
					localLong++
					if len(localHits) < 5 {
						localHits = append(localHits, hit{objPath: path, columnName: ptr.ColumnName})
					}
				}
			}

			mu.Lock()
			total += localTotal
			longCnt += localLong
			hits = append(hits, localHits...)
			objStats = append(objStats, perObjectStats{
				path:       path,
				uniqueCols: len(uniqueNames),
				longCols:   int(localLong),
			})
			n := atomic.AddInt64(&done, 1)
			mu.Unlock()

			if n%50 == 0 || n == int64(len(paths)) {
				log.Printf("  progress: %d/%d objects", n, len(paths))
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		log.Fatalf("ERROR: %v", err)
	}

	// Sort per-object stats by unique column count descending.
	sort.Slice(objStats, func(i, j int) bool {
		return objStats[i].uniqueCols > objStats[j].uniqueCols
	})

	log.Printf("Results:")
	log.Printf("  Total column name entries: %d", total)
	log.Printf("  Column names > %d chars:   %d", longColumnThreshold, longCnt)
	if longCnt > 0 {
		log.Printf("  Percentage:                %.2f%%", float64(longCnt)/float64(total)*100)
	}
	log.Printf("  Objects scanned:           %d", len(objStats))

	// Per-object unique column counts.
	log.Printf("")
	log.Printf("  Unique columns per object (top 20):")
	for i, s := range objStats {
		if i >= 20 {
			break
		}
		log.Printf("    %5d unique (%3d long)  %s", s.uniqueCols, s.longCols, s.path)
	}

	if len(objStats) > 0 {
		var totalUnique int
		for _, s := range objStats {
			totalUnique += s.uniqueCols
		}
		log.Printf("  Avg unique columns/object: %d", totalUnique/len(objStats))
		log.Printf("  Min: %d  Max: %d", objStats[len(objStats)-1].uniqueCols, objStats[0].uniqueCols)
	}

	// Deduplicate samples by column name.
	seen := make(map[string]struct{})
	log.Printf("")
	log.Printf("  Sample long column names:")
	for _, h := range hits {
		if _, ok := seen[h.columnName]; ok {
			continue
		}
		seen[h.columnName] = struct{}{}
		log.Printf("    [%d chars] %s", len(h.columnName), h.columnName)
		if len(seen) >= 20 {
			break
		}
	}
}

// scanStreamLabels fetches each referenced object in parallel, iterates its
// streams sections, and counts unique key=value label pairs across all objects.
// streamRef holds the data needed to build a TSDB entry for one stream in one object.
type streamRef struct {
	labels  labels.Labels
	path    string
	section int
	minTime int64
	maxTime int64
	rows    int
	KB      int
}

func scanStreamLabels(ctx context.Context, reader objstore.BucketReader, prefixedBkt objstore.Bucket, tocEntries map[string][]rangeEntry) {
	paths := sortedKeys(tocEntries)
	log.Printf("Scanning stream labels in %d objects (parallel=%d)", len(paths), parallelism)

	type perObjectStats struct {
		path        string
		uniquePairs int
		streamCount int
	}

	var (
		mu            sync.Mutex
		globalPairs   = make(map[string]struct{})
		globalKeyVals = make(map[string]map[string]struct{})
		objStats      []perObjectStats
		allRefs       []streamRef
		done          int64
	)

	g, _ := errgroup.WithContext(ctx)
	g.SetLimit(parallelism)

	var totalSize atomic.Int64

	for _, path := range paths[:40] {
		g.Go(func() error {
			obj, err := dataobj.FromBucket(ctx, reader, path)
			if err != nil {
				if prefixedBkt.IsObjNotFoundErr(err) {
					log.Printf("  SKIP: %s (not found)", path)
					return nil
				}
				return fmt.Errorf("opening %s: %w", path, err)
			}

			localPairs := make(map[string]struct{})
			localKeyVals := make(map[string]map[string]struct{})
			var localRefs []streamRef
			var streamCount int

			for secIdx, sec := range obj.Sections().Filter(streams.CheckSection) {
				totalSize.Add(sec.Reader.DataSize())

				streamsSection, err := streams.Open(ctx, sec)
				if err != nil {
					return fmt.Errorf("opening streams section %d in %s: %w", secIdx, path, err)
				}

				for res := range streams.IterSection(ctx, streamsSection) {
					stream, err := res.Value()
					if err != nil {
						return fmt.Errorf("iterating streams in %s section %d: %w", path, secIdx, err)
					}
					streamCount++

					localRefs = append(localRefs, streamRef{
						labels:  stream.Labels.Copy(),
						path:    path,
						section: secIdx,
						minTime: stream.MinTimestamp.UnixMilli(),
						maxTime: stream.MaxTimestamp.UnixMilli(),
						rows:    stream.Rows,
						KB:      int(stream.UncompressedSize / 1024),
					})

					stream.Labels.Range(func(l labels.Label) {
						pair := l.Name + "=" + l.Value
						localPairs[pair] = struct{}{}
						if _, ok := localKeyVals[l.Name]; !ok {
							localKeyVals[l.Name] = make(map[string]struct{})
						}
						localKeyVals[l.Name][l.Value] = struct{}{}
					})
				}
			}

			mu.Lock()
			allRefs = append(allRefs, localRefs...)
			for pair := range localPairs {
				globalPairs[pair] = struct{}{}
			}
			for key, vals := range localKeyVals {
				if _, ok := globalKeyVals[key]; !ok {
					globalKeyVals[key] = make(map[string]struct{})
				}
				for v := range vals {
					globalKeyVals[key][v] = struct{}{}
				}
			}
			objStats = append(objStats, perObjectStats{
				path:        path,
				uniquePairs: len(localPairs),
				streamCount: streamCount,
			})
			n := atomic.AddInt64(&done, 1)
			mu.Unlock()

			if n%50 == 0 || n == int64(len(paths)) {
				log.Printf("  progress: %d/%d objects", n, len(paths))
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		log.Fatalf("ERROR: %v", err)
	}

	// Sort keys by distinct-value count descending.
	type keyCount struct {
		key   string
		count int
	}
	keyCounts := make([]keyCount, 0, len(globalKeyVals))
	for k, vals := range globalKeyVals {
		keyCounts = append(keyCounts, keyCount{k, len(vals)})
	}
	sort.Slice(keyCounts, func(i, j int) bool { return keyCounts[i].count > keyCounts[j].count })

	// Sort per-object stats by unique pairs descending.
	sort.Slice(objStats, func(i, j int) bool { return objStats[i].uniquePairs > objStats[j].uniquePairs })

	log.Printf("Results:")
	log.Printf("  Objects scanned:               %d", len(objStats))
	log.Printf("  Total streams collected:       %d", len(allRefs))
	log.Printf("  Global unique key=value pairs: %d", len(globalPairs))
	log.Printf("  Distinct label keys:           %d", len(globalKeyVals))
	log.Printf("  Total streams size:             %s", humanize.Bytes(uint64(totalSize.Load())))

	log.Printf("")
	log.Printf("  Label keys by distinct value count (top 50):")
	for i, kc := range keyCounts {
		if i >= 50 {
			break
		}
		log.Printf("    %8d values  %s", kc.count, kc.key)
	}

	log.Printf("")
	log.Printf("  Unique key=value pairs per object (top 20):")
	for i, s := range objStats {
		if i >= 20 {
			break
		}
		log.Printf("    %6d pairs (%5d streams)  %s", s.uniquePairs, s.streamCount, s.path)
	}

	if len(objStats) > 0 {
		var totalPairs, totalStreams int
		for _, s := range objStats {
			totalPairs += s.uniquePairs
			totalStreams += s.streamCount
		}
		log.Printf("")
		log.Printf("  Avg unique pairs/object:   %d", totalPairs/len(objStats))
		log.Printf("  Avg streams/object:        %d", totalStreams/len(objStats))
		log.Printf("  Min pairs: %d  Max pairs: %d", objStats[len(objStats)-1].uniquePairs, objStats[0].uniquePairs)
	}

	// Build a TSDB index from all collected streams.
	buildTSDBFromStreams(ctx, allRefs)
}

// chunkRef identifies the object and section that a ChunkMeta points to.
// Stored in the lookup table alongside the TSDB index.
type chunkRef struct {
	Path      string
	SectionID int
}

// buildTSDBFromStreams partitions the collected streams into per-day buckets,
// builds a separate TSDB index for each day, and reports sizes individually
// plus the total. Streams that span a day boundary are included in every day
// they overlap.
func buildTSDBFromStreams(ctx context.Context, refs []streamRef) {
	if len(refs) == 0 {
		log.Printf("\n  No streams to build TSDB from.")
		return
	}

	// Bucket streams by day. A stream overlapping multiple days goes into each.
	dayBuckets := make(map[time.Time][]streamRef) // key: day truncated to 00:00 UTC
	for _, ref := range refs {
		dayStart := time.UnixMilli(ref.minTime).UTC().Truncate(24 * time.Hour)
		dayEnd := time.UnixMilli(ref.maxTime).UTC()
		for d := dayStart; !d.After(dayEnd); d = d.Add(24 * time.Hour) {
			dayBuckets[d] = append(dayBuckets[d], ref)
		}
	}

	days := make([]time.Time, 0, len(dayBuckets))
	for d := range dayBuckets {
		days = append(days, d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })

	log.Printf("")
	log.Printf("Building %d daily TSDB indices from %d stream entries...", len(days), len(refs))

	var grandTotalTSDB, grandTotalLookup int

	for _, day := range days {
		dayRefs := dayBuckets[day]
		tsdbData, lookupData := buildOneTSDB(ctx, dayRefs)

		grandTotalTSDB += len(tsdbData)
		grandTotalLookup += len(lookupData)
		dayTotal := len(tsdbData) + len(lookupData)

		log.Printf("")
		log.Printf("  %s  (%d streams)", day.Format("2006-01-02"), len(dayRefs))
		log.Printf("    TSDB index:     %s (%d bytes)", humanize.Bytes(uint64(len(tsdbData))), len(tsdbData))
		log.Printf("    Lookup table:   %s (%d bytes)", humanize.Bytes(uint64(len(lookupData))), len(lookupData))
		log.Printf("    Day total:      %s (%d bytes)", humanize.Bytes(uint64(dayTotal)), dayTotal)
	}

	grandTotal := grandTotalTSDB + grandTotalLookup
	log.Printf("")
	log.Printf("  All days combined:")
	log.Printf("    TSDB indices:   %s (%d bytes)", humanize.Bytes(uint64(grandTotalTSDB)), grandTotalTSDB)
	log.Printf("    Lookup tables:  %s (%d bytes)", humanize.Bytes(uint64(grandTotalLookup)), grandTotalLookup)
	log.Printf("    Grand total:    %s (%d bytes)", humanize.Bytes(uint64(grandTotal)), grandTotal)
}

// buildOneTSDB builds a single TSDB index + lookup table from the given refs
// and returns their serialized bytes.
func buildOneTSDB(ctx context.Context, refs []streamRef) (tsdbData, lookupData []byte) {
	builder := tsdb.NewBuilder(index.FormatV3)

	type refKey struct {
		path    string
		section int
	}
	refIndex := make(map[refKey]uint32)
	var chunkRefs []chunkRef

	for _, ref := range refs {
		key := refKey{path: ref.path, section: ref.section}
		idx, ok := refIndex[key]
		if !ok {
			idx = uint32(len(chunkRefs))
			chunkRefs = append(chunkRefs, chunkRef{Path: ref.path, SectionID: ref.section})
			refIndex[key] = idx
		}

		fp := model.Fingerprint(ref.labels.Hash())
		builder.AddSeries(ref.labels, fp, []index.ChunkMeta{
			{
				Checksum: idx,
				MinTime:  ref.minTime,
				MaxTime:  ref.maxTime,
				Entries:  uint32(ref.rows),
				KB:       uint32(ref.KB),
			},
		})
	}

	var err error
	_, tsdbData, err = builder.BuildInMemory(
		ctx,
		func(from, through model.Time, checksum uint32) tsdb.Identifier {
			return tsdb.SingleTenantTSDBIdentifier{
				TS:       time.Now(),
				From:     from,
				Through:  through,
				Checksum: checksum,
			}
		},
	)
	if err != nil {
		log.Fatalf("ERROR building TSDB: %v", err)
	}

	lookupData = encodeChunkRefTable(chunkRefs)
	return tsdbData, lookupData
}

// encodeChunkRefTable serializes a chunkRef slice into a compact binary format
// with an interned string table for paths.
//
// Wire format:
//
//	[uint32 path_count]             -- interned string table
//	for each path:
//	  [uint16 len][path bytes]
//	[uint32 entry_count]            -- entries indexed by checksum
//	for each entry:
//	  [uint32 path_string_index][uint32 section_id]
func encodeChunkRefTable(refs []chunkRef) []byte {
	pathIdx := make(map[string]uint32)
	var pathStrings []string
	for _, ref := range refs {
		if _, ok := pathIdx[ref.Path]; !ok {
			pathIdx[ref.Path] = uint32(len(pathStrings))
			pathStrings = append(pathStrings, ref.Path)
		}
	}

	var buf bytes.Buffer

	// String table.
	binary.Write(&buf, binary.LittleEndian, uint32(len(pathStrings)))
	for _, s := range pathStrings {
		binary.Write(&buf, binary.LittleEndian, uint16(len(s)))
		buf.WriteString(s)
	}

	// Entries.
	binary.Write(&buf, binary.LittleEndian, uint32(len(refs)))
	for _, ref := range refs {
		binary.Write(&buf, binary.LittleEndian, pathIdx[ref.Path])
		binary.Write(&buf, binary.LittleEndian, uint32(ref.SectionID))
	}

	return buf.Bytes()
}

// decodeChunkRefTable deserializes the binary table produced by encodeChunkRefTable.
// The returned slice is indexed by checksum: table[chunkMeta.Checksum] == chunkRef.
func decodeChunkRefTable(data []byte) ([]chunkRef, error) {
	r := bytes.NewReader(data)

	// String table.
	var pathCount uint32
	if err := binary.Read(r, binary.LittleEndian, &pathCount); err != nil {
		return nil, fmt.Errorf("reading path count: %w", err)
	}
	pathStrings := make([]string, pathCount)
	for i := range pathStrings {
		var slen uint16
		if err := binary.Read(r, binary.LittleEndian, &slen); err != nil {
			return nil, fmt.Errorf("reading path string length: %w", err)
		}
		buf := make([]byte, slen)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, fmt.Errorf("reading path string: %w", err)
		}
		pathStrings[i] = string(buf)
	}

	// Entries.
	var entryCount uint32
	if err := binary.Read(r, binary.LittleEndian, &entryCount); err != nil {
		return nil, fmt.Errorf("reading entry count: %w", err)
	}
	entries := make([]chunkRef, entryCount)
	for i := range entries {
		var pIdx, secID uint32
		if err := binary.Read(r, binary.LittleEndian, &pIdx); err != nil {
			return nil, fmt.Errorf("reading path index: %w", err)
		}
		if err := binary.Read(r, binary.LittleEndian, &secID); err != nil {
			return nil, fmt.Errorf("reading section ID: %w", err)
		}
		entries[i] = chunkRef{Path: pathStrings[pIdx], SectionID: int(secID)}
	}
	return entries, nil
}

func downloadObject(ctx context.Context, bucket objstore.BucketReader, path string) ([]byte, error) {
	r, err := bucket.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// hourSetToRanges converts a set of hourly buckets into merged contiguous time ranges.
func hourSetToRanges(hours map[time.Time]struct{}, source string) []rangeEntry {
	if len(hours) == 0 {
		return nil
	}
	sorted := make([]time.Time, 0, len(hours))
	for h := range hours {
		sorted = append(sorted, h)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })

	var ranges []rangeEntry
	start := sorted[0]
	prev := sorted[0]
	for _, h := range sorted[1:] {
		if h.Sub(prev) > time.Hour {
			ranges = append(ranges, rangeEntry{start: start, end: prev.Add(time.Hour), toc: source})
			start = h
		}
		prev = h
	}
	ranges = append(ranges, rangeEntry{start: start, end: prev.Add(time.Hour), toc: source})
	return ranges
}

// readSmallObject downloads the full object into memory. Use for small objects like TOC files.
func readSmallObject(ctx context.Context, bucket objstore.BucketReader, path string) (*dataobj.Object, error) {
	r, err := bucket.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return dataobj.FromReaderAt(bytes.NewReader(data), int64(len(data)))
}

// objectRow is a template-friendly representation of one object's time ranges.
type objectRow struct {
	Path   string
	Ranges []timeRange
}

type timeRange struct {
	Start, End           time.Time
	StartPct, WidthPct   float64
	StartLabel, EndLabel string
	TOC                  string
}

func buildHTML(title string, byPath map[string][]rangeEntry) string {
	// Compute global min/max across all entries.
	var globalMin, globalMax time.Time
	for _, entries := range byPath {
		for _, e := range entries {
			if globalMin.IsZero() || e.start.Before(globalMin) {
				globalMin = e.start
			}
			if globalMax.IsZero() || e.end.After(globalMax) {
				globalMax = e.end
			}
		}
	}
	totalDur := globalMax.Sub(globalMin)
	if totalDur <= 0 {
		totalDur = time.Hour
	}

	pct := func(t time.Time) float64 {
		return float64(t.Sub(globalMin)) / float64(totalDur) * 100
	}

	// Build sorted rows.
	paths := sortedKeys(byPath)
	rows := make([]objectRow, 0, len(paths))
	for _, path := range paths {
		entries := byPath[path]
		var ranges []timeRange
		seen := make(map[string]struct{})
		for _, e := range entries {
			key := e.start.Format(time.RFC3339) + e.end.Format(time.RFC3339)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			sp := pct(e.start)
			ep := pct(e.end)
			w := ep - sp
			if w < 0.2 {
				w = 0.2
			}
			ranges = append(ranges, timeRange{
				Start:      e.start,
				End:        e.end,
				StartPct:   sp,
				WidthPct:   w,
				StartLabel: e.start.Format("2006-01-02 15:04"),
				EndLabel:   e.end.Format("2006-01-02 15:04"),
				TOC:        e.toc,
			})
		}
		sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start.Before(ranges[j].Start) })
		rows = append(rows, objectRow{Path: path, Ranges: ranges})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Ranges[0].Start.Before(rows[j].Ranges[0].Start)
	})

	data := struct {
		Title                string
		GlobalMin, GlobalMax string
		GlobalMinEpoch       int64
		GlobalMaxEpoch       int64
		Rows                 []objectRow
	}{
		Title:          title,
		GlobalMin:      globalMin.Format("2006-01-02 15:04"),
		GlobalMax:      globalMax.Format("2006-01-02 15:04"),
		GlobalMinEpoch: globalMin.UnixMilli(),
		GlobalMaxEpoch: globalMax.UnixMilli(),
		Rows:           rows,
	}

	var buf bytes.Buffer
	if err := htmlTmpl.Execute(&buf, data); err != nil {
		log.Fatalf("template: %v", err)
	}
	return buf.String()
}

var htmlTmpl = template.Must(template.New("page").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>{{.Title}}</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: "SF Mono", "Menlo", "Monaco", monospace; font-size: 13px; background: #0d1117; color: #c9d1d9; padding: 24px; }
  h1 { font-size: 18px; margin-bottom: 12px; color: #58a6ff; }
  .axis { display: flex; justify-content: space-between; color: #8b949e; font-size: 11px; margin-bottom: 8px; padding: 0 200px 0 0; }
  .row { display: flex; align-items: center; margin-bottom: 2px; min-height: 22px; }
  .label { width: 200px; min-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; padding-right: 8px; text-align: right; font-size: 11px; color: #8b949e; cursor: default; }
  .label:hover { color: #c9d1d9; }
  .track { flex: 1; position: relative; height: 18px; background: #161b22; border-radius: 3px; }
  .bar { position: absolute; height: 100%; border-radius: 3px; background: #1f6feb; opacity: 0.85; cursor: pointer; min-width: 2px; transition: opacity 0.15s; }
  .bar:hover { opacity: 1; z-index: 1; }
  .tooltip { display: none; position: absolute; bottom: 24px; left: 50%; transform: translateX(-50%); background: #30363d; color: #c9d1d9; padding: 6px 10px; border-radius: 6px; font-size: 11px; white-space: nowrap; z-index: 10; pointer-events: none; box-shadow: 0 2px 8px rgba(0,0,0,.4); }
  .bar:hover .tooltip { display: block; }
  .summary { margin-bottom: 16px; color: #8b949e; font-size: 12px; }
  #sel-range { position: fixed; top: 0; bottom: 0; background: rgba(88,166,255,0.15); border-left: 1px solid rgba(88,166,255,0.6); border-right: 1px solid rgba(88,166,255,0.6); pointer-events: none; z-index: 100; display: none; }
  #cursor-line { position: fixed; top: 0; bottom: 0; width: 1px; background: rgba(88,166,255,0.45); pointer-events: none; z-index: 100; display: none; }
  #sel-info { position: fixed; top: 8px; right: 24px; background: #30363d; color: #c9d1d9; padding: 8px 14px; border-radius: 6px; font-size: 12px; z-index: 101; display: none; box-shadow: 0 2px 8px rgba(0,0,0,.4); line-height: 1.6; }
  #sel-info strong { color: #58a6ff; }
</style>
</head>
<body>
<h1>{{.Title}}</h1>
<div class="summary">{{len .Rows}} objects &middot; {{.GlobalMin}} &ndash; {{.GlobalMax}} UTC</div>
<div class="axis"><span>{{.GlobalMin}}</span><span>{{.GlobalMax}}</span></div>
{{range .Rows}}
<div class="row">
  <div class="label" title="{{.Path}}">{{.Path}}</div>
  <div class="track">
    {{range .Ranges}}<div class="bar" style="left:{{printf "%.4f" .StartPct}}%; width:{{printf "%.4f" .WidthPct}}%;">
      <div class="tooltip">{{.StartLabel}} &ndash; {{.EndLabel}}<br>toc: {{.TOC}}</div>
    </div>{{end}}
  </div>
</div>
{{end}}
<div id="sel-range"></div>
<div id="cursor-line"></div>
<div id="sel-info"></div>
<script>
(function() {
  var MIN = {{.GlobalMinEpoch}}, MAX = {{.GlobalMaxEpoch}};
  var range = document.getElementById('sel-range');
  var cursor = document.getElementById('cursor-line');
  var info = document.getElementById('sel-info');
  var rowData = [];
  document.querySelectorAll('.row').forEach(function(row) {
    var intervals = [];
    row.querySelectorAll('.bar').forEach(function(bar) {
      var l = parseFloat(bar.style.left);
      intervals.push([l, l + parseFloat(bar.style.width)]);
    });
    rowData.push(intervals);
  });
  function trackBounds() {
    var t = document.querySelector('.track');
    if (!t) return null;
    var r = t.getBoundingClientRect();
    return {left: r.left, width: r.width};
  }
  function pctToTime(p) {
    var ms = MIN + (p / 100) * (MAX - MIN);
    return new Date(ms).toISOString().replace('T', ' ').slice(0, 16) + ' UTC';
  }
  function countOverlap(lo, hi) {
    var c = 0;
    for (var i = 0; i < rowData.length; i++) {
      for (var j = 0; j < rowData[i].length; j++) {
        if (rowData[i][j][1] >= lo && rowData[i][j][0] <= hi) { c++; break; }
      }
    }
    return c;
  }
  var dragging = false, pinned = false, anchorX = 0, anchorPct = 0;
  function showRange(lo, hi) {
    var c = countOverlap(lo, hi);
    info.style.display = 'block';
    info.innerHTML = '<strong>' + c + '</strong> / ' + rowData.length + ' objects<br>' +
      pctToTime(lo) + ' &ndash; ' + pctToTime(hi);
  }
  function clearPin() {
    pinned = false;
    range.style.display = 'none';
  }
  document.addEventListener('mousedown', function(e) {
    if (pinned) { clearPin(); return; }
    var b = trackBounds();
    if (!b || b.width === 0) return;
    var pct = (e.clientX - b.left) / b.width * 100;
    if (pct < 0 || pct > 100) return;
    dragging = true;
    anchorX = e.clientX;
    anchorPct = pct;
    range.style.display = 'block';
    range.style.left = e.clientX + 'px';
    range.style.width = '0px';
  });
  document.addEventListener('mousemove', function(e) {
    var b = trackBounds();
    if (!b || b.width === 0) return;
    var pct = (e.clientX - b.left) / b.width * 100;
    pct = Math.max(0, Math.min(100, pct));
    cursor.style.display = 'block';
    cursor.style.left = e.clientX + 'px';
    if (dragging) {
      var left = Math.min(anchorX, e.clientX);
      var w = Math.abs(e.clientX - anchorX);
      range.style.left = left + 'px';
      range.style.width = w + 'px';
      showRange(Math.min(anchorPct, pct), Math.max(anchorPct, pct));
    } else if (!pinned) {
      var c = countOverlap(pct, pct);
      info.style.display = 'block';
      info.innerHTML = '<strong>' + c + '</strong> / ' + rowData.length + ' objects at ' + pctToTime(pct);
    }
  });
  document.addEventListener('mouseup', function(e) {
    if (!dragging) return;
    dragging = false;
    var b = trackBounds();
    if (!b || b.width === 0) return;
    var pct = (e.clientX - b.left) / b.width * 100;
    pct = Math.max(0, Math.min(100, pct));
    if (Math.abs(pct - anchorPct) > 0.1) {
      pinned = true;
      showRange(Math.min(anchorPct, pct), Math.max(anchorPct, pct));
    }
  });
  document.addEventListener('mouseleave', function() {
    if (!pinned) {
      cursor.style.display = 'none';
      range.style.display = 'none';
      info.style.display = 'none';
    }
    dragging = false;
  });
})();
</script>
</body>
</html>
`))

func sortedKeys[M ~map[K]V, K string, V any](m M) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
