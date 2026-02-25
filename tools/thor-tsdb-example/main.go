package thortsdbexample

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"
)

var parallelism = 32

func main() {
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

func sortedKeys[M ~map[K]V, K string, V any](m M) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
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
