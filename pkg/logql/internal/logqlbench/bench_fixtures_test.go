package logqlbench

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/objtest"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// tenant must be objtest.Tenant: objtest.Builder.Append always writes for that fixed tenant, so
// the chunk store has to use the same one for both backends to hold the same data.
const tenant = objtest.Tenant

// storageDirName holds both backends: chunks at <tenant>/..., data objects at dataobj/....
const storageDirName = "storage"

const (
	// Bump fixtureVersion when the generation logic below changes, so stale caches regenerate.
	fixtureVersion = 4
	fixtureSeed    = 1
	numStreams     = 2000
	linesPerStream = 5000
	lineBytes      = 100 // ~2000*5000*100 ≈ 1 GB uncompressed
	day            = 24 * time.Hour
	daySecs        = 24 * 60 * 60

	// Label schema: 10 names on every stream, values padded to ~20 B, with a cardinality mix.
	labelAllName         = "cluster" // cardinality 1: matches all streams (high input)
	labelAllValue        = "prod-us-central1-01"
	labelMediumCardName  = "namespace" // sum by(...) grouping (medium output)
	numMediumCardValues  = 30
	labelSubsetName      = "team" // labelSubsetValue matches ~40 streams (low input)
	labelSubsetValue     = "team-canary-00000001"
	labelSubsetPeriod    = 50        // 1 in 50 streams
	labelMediumInputName = "service" // cardinality 8: one value matches 250 streams (medium input)
	labelUniqueName      = "pod"

	// estimatedUncompressedBytes is the fixture's total raw log-line volume.
	estimatedUncompressedBytes = numStreams * linesPerStream * lineBytes

	// estimatedCompressionRatio is this fixture's measured compressed:uncompressed ratio.
	estimatedCompressionRatio = 0.1

	// targetObjectCount is how many data objects the fixture's dataobj backend should split into,
	// so the benchmark exercises more than the single object a small corpus would otherwise fit.
	targetObjectCount = 10
	targetObjectSize  = estimatedUncompressedBytes * estimatedCompressionRatio / targetObjectCount

	// targetSectionsPerObject is how many logs sections each data object should split into.
	targetSectionsPerObject = 10

	// targetSectionSize is the uncompressed size.
	targetSectionSize = estimatedUncompressedBytes / targetObjectCount / targetSectionsPerObject
)

var (
	// fixtureStart is 1 second after the Unix epoch, not the epoch itself: a data object's ToC
	// entry must start strictly after the epoch, which the on-disk format reserves as "no value".
	fixtureStart = time.Unix(1, 0).UTC()
	fixtureEnd   = fixtureStart.Add(day)
)

// ensureFixtures builds the fixtures once, into a content-addressed directory under os.TempDir
// reused across runs and processes, and returns its path.
func ensureFixtures(t testing.TB) (string, error) {
	final := filepath.Join(os.TempDir(), "loki-logqlbench-"+fixtureHash())
	if _, err := os.Stat(filepath.Join(final, ".done")); err == nil {
		return final, nil
	}

	tmp, err := os.MkdirTemp(os.TempDir(), "loki-logqlbench-gen-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	if err := buildFixtures(t, tmp); err != nil {
		return "", err
	}

	if err := os.WriteFile(filepath.Join(tmp, ".done"), []byte(fixtureHash()), 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, final); err != nil {
		// A concurrent generator won the race; reuse the final dir if it now exists.
		if _, statErr := os.Stat(filepath.Join(final, ".done")); statErr == nil {
			return final, nil
		}
		return "", err
	}
	return final, nil
}

// tbWithDir overrides TempDir so objtest.NewBuilder writes under dir instead of a fresh temp
// directory, keeping the data objects inside ensureFixtures' own persistent, content-addressed
// cache rather than one objtest cleans up when t ends.
type tbWithDir struct {
	testing.TB
	dir string
}

func (o *tbWithDir) TempDir() string { return o.dir }

// buildFixtures writes the same corpus into both backends, one stream at a time, so it is never
// fully resident. A dataobj write failure fails t directly (see objtest), rather than returning
// an error like the rest of this function.
func buildFixtures(t testing.TB, dir string) error {
	chunks, err := newChunkStore(dir, tenant, nil)
	if err != nil {
		return fmt.Errorf("opening chunk store for fixture generation: %w", err)
	}

	dataObjDir := filepath.Join(dir, storageDirName, "dataobj")
	dataObjs := objtest.NewBuilder(&tbWithDir{TB: t, dir: dataObjDir},
		objtest.WithTargetPageSize(1<<20), // 1MB
		// TargetSectionSize must stay <= TargetObjectSize (uncompressed vs compressed bytes, but
		// the builder compares the raw numbers regardless).
		objtest.WithTargetObjectSize(targetObjectSize),
		objtest.WithTargetSectionSize(targetSectionSize),
		objtest.WithBufferSize(16<<20), // 16MB
		objtest.WithMaxPageRows(10000), // matches logsobj.BuilderBaseConfig's MaxPageRows default
	)

	rng := fixtureRNG()
	ctx := context.Background()
	for i := 0; i < numStreams; i++ {
		stream := generateStream(i, rng)
		if err := chunks.Write(ctx, []logproto.Stream{stream}); err != nil {
			return fmt.Errorf("writing stream %d to chunk store: %w", i, err)
		}
		dataObjs.Append(ctx, stream)
	}

	if err := chunks.Close(); err != nil {
		return fmt.Errorf("closing chunk store: %w", err)
	}
	dataObjs.Close()
	return nil
}

// fixtureHash is the content-addressed cache key derived from the fixture parameters: changing
// any of them regenerates both backends' fixtures.
func fixtureHash() string {
	return fmt.Sprintf("v%d-s%d-str%d-lin%d-b%d", fixtureVersion, fixtureSeed, numStreams, linesPerStream, lineBytes)
}

func fixtureRNG() *rand.Rand {
	return rand.New(rand.NewSource(fixtureSeed)) //nolint:gosec // determinism, not security
}

// padValue right-pads v toward ~20 B so label values average ~20 B.
func padValue(v string) string {
	const target = 20
	if len(v) >= target {
		return v
	}
	return v + strings.Repeat("x", target-len(v))
}

// streamLabels builds the 10-label set for stream i.
func streamLabels(i int) labels.Labels {
	b := labels.NewBuilder(labels.EmptyLabels())
	b.Set(labelAllName, labelAllValue)
	b.Set("region", padValue(fmt.Sprintf("region-%d", i%2)))
	b.Set("tier", padValue(fmt.Sprintf("tier-%d", i%3)))
	b.Set(labelMediumInputName, padValue(fmt.Sprintf("svc-%d", i%8)))
	b.Set(labelMediumCardName, padValue(fmt.Sprintf("ns-%02d", i%numMediumCardValues)))
	b.Set("job", padValue(fmt.Sprintf("job-%02d", i%50)))
	b.Set("version", padValue(fmt.Sprintf("v-%d", i%3)))
	if i%labelSubsetPeriod == 0 {
		b.Set(labelSubsetName, labelSubsetValue)
	} else {
		b.Set(labelSubsetName, padValue(fmt.Sprintf("team-%02d", i%13)))
	}
	b.Set("zone", padValue(fmt.Sprintf("zone-%d", i%4)))
	b.Set(labelUniqueName, padValue(fmt.Sprintf("pod-%06d", i)))
	return b.Labels()
}

// streamWindow returns the [offset, offset+dur) window stream i's lines span: 50% of streams
// cover the full 24h, 30% cover half, 20% cover a tenth.
func streamWindow(i int, rng *rand.Rand) (offsetSecs, durSecs int64) {
	switch {
	case i%10 < 5: // 50%
		return 0, daySecs
	case i%10 < 8: // 30%
		durSecs = daySecs / 2
	default: // 20%
		durSecs = daySecs / 10
	}
	offsetSecs = rng.Int63n(daySecs - durSecs + 1) // whole-second offset in [0, day-dur]
	return offsetSecs, durSecs
}

// line builds a ~lineBytes line, unique per (stream, line) so chunk dedup never drops it.
func line(streamIdx, lineIdx int) string {
	prefix := fmt.Sprintf("level=info stream=%d line=%d msg=\"request served\" ", streamIdx, lineIdx)
	if len(prefix) >= lineBytes {
		return prefix[:lineBytes]
	}
	return prefix + strings.Repeat("=", lineBytes-len(prefix))
}

// generateStream builds stream i: its labels and linesPerStream entries, evenly spaced across its
// window.
func generateStream(i int, rng *rand.Rand) logproto.Stream {
	lbls := streamLabels(i)
	offsetSecs, durSecs := streamWindow(i, rng)
	stepSecs := durSecs / int64(linesPerStream)
	if stepSecs < 1 {
		stepSecs = 1 // keep timestamps distinct even if lines > window-seconds
	}

	entries := make([]logproto.Entry, linesPerStream)
	for j := 0; j < linesPerStream; j++ {
		ts := fixtureStart.Add(time.Duration(offsetSecs+int64(j)*stepSecs) * time.Second)
		entries[j] = logproto.Entry{
			Timestamp: ts,
			Line:      line(i, j),
			StructuredMetadata: []logproto.LabelAdapter{
				{Name: "trace_id", Value: fmt.Sprintf("trace-%06d-%06d", i, j)}, // unique per line
				{Name: "span_id", Value: fmt.Sprintf("span-%06d-%06d", i, j)},   // unique per line
				{Name: "shard", Value: fmt.Sprintf("shard-%02d", j%50)},         // 50 unique values
			},
		}
	}
	return logproto.Stream{Labels: lbls.String(), Entries: entries}
}
