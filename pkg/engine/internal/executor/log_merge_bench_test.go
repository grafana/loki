package executor

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj/builder"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// BenchmarkLogObjectMerge merges 10 source objects into one. Every source holds
// the same 400 streams with interleaved timestamps, so all sources overlap.
//
// Each record carries every key in benchMetadata, so a logs section has 20
// columns: 17 metadata columns plus stream ID, timestamp and message. This
// matches the average column count of production logs sections.
//
// MB/s counts the line bytes and metadata value bytes of the input records.
func BenchmarkLogObjectMerge(b *testing.B) {
	const (
		tenant           = "acme"
		sourceCount      = 10
		streamCount      = 400
		recordsPerStream = 10
	)
	sortSchema := []string{"label:app"}
	base := time.Date(2026, 5, 14, 1, 0, 0, 0, time.UTC)
	rng := rand.New(rand.NewPCG(1, 2))

	// The dataobj builder registers the same logs builder defaults as the compactor.
	// The benchmark cannot use the compactor config, because the compactor package
	// imports this one.
	var defaults builder.Config
	flagext.DefaultValues(&defaults)
	cfg := defaults.LogsobjBuilder

	dataBucket := objstore.NewInMemBucket()
	paths := make([]string, 0, sourceCount)
	var inputBytes int64
	for file := range sourceCount {
		var streamSet []testStream
		for stream := range streamCount {
			entries := make([]push.Entry, 0, recordsPerStream)
			for record := range recordsPerStream {
				entry := benchEntry(rng, base.Add(time.Duration(record*sourceCount+file)*time.Second))
				inputBytes += int64(len(entry.Line))
				for _, kv := range entry.StructuredMetadata {
					inputBytes += int64(len(kv.Value))
				}
				entries = append(entries, entry)
			}
			streamSet = append(streamSet, testStream{labels: fmt.Sprintf(`{app="app-%04d"}`, stream), entries: entries})
		}
		path := fmt.Sprintf("objects/source-%d", file)
		buildSourceLogObjectWithConfig(b, dataBucket, path, cfg, sortSchema, map[string][]testStream{tenant: streamSet})
		paths = append(paths, path)
	}

	node := &physical.LogMerge{
		Tenant: tenant,
		Runs:   sourceLogRuns(b, dataBucket, tenant, paths...),
	}

	b.SetBytes(inputBytes)
	b.ReportAllocs()
	for b.Loop() {
		c := newTestExecutorContext(b, objstore.NewInMemBucket())
		c.logsobjCfg = cfg
		c.indexobjCfg = defaults.IndexobjBuilder
		c.dataBucket = dataBucket
		_, err := c.doLogObjectMerge(b.Context(), node)
		if err != nil {
			require.NoError(b, err)
		}
	}
}

// benchEntry returns an entry at ts with a random line and a value for every
// key in benchMetadata.
func benchEntry(rng *rand.Rand, ts time.Time) push.Entry {
	const lineSize = 480 // average line size sampled from a production environment
	return push.Entry{
		Timestamp:          ts,
		Line:               benchLogLine(rng, lineSize),
		StructuredMetadata: benchRecordMetadata(rng),
	}
}

var (
	benchLevels  = []string{"debug", "info", "info", "info", "warn", "error"}
	benchMethods = []string{"GET", "GET", "GET", "POST", "PUT", "DELETE"}
	benchStatus  = []int{200, 200, 200, 204, 400, 404, 500}
	benchPaths   = []string{"/api/v1/push", "/api/v1/query_range", "/api/v1/labels", "/loki/api/v1/tail", "/ready", "/metrics"}
	benchWords   = strings.Fields(`request completed failed retrying upstream connection timeout
		cache miss hit flushed chunk stream tenant series querier ingester batch
		received sent bytes rows section object compacted index token ring member
		healthy unhealthy shard partition offset commit lag consumer producer`)
)

// benchMetadata lists the structured metadata keys of every benchmark record,
// sorted by name. Cardinality is the number of distinct values of the key. A
// cardinality of 0 gives each record a random 64-bit hex value.
var benchMetadata = []struct {
	name        string
	cardinality int
}{
	{"cluster", 4},
	{"container", 30},
	{"detected_level", 5},
	{"env", 3},
	{"host", 200},
	{"http_method", 6},
	{"http_status", 12},
	{"namespace", 25},
	{"node", 150},
	{"pod", 1000},
	{"region", 6},
	{"service_version", 40},
	{"span_id", 0},
	{"team", 15},
	{"trace_id", 0},
	{"user_agent", 50},
	{"zone", 12},
}

// benchRecordMetadata returns a value for every key in benchMetadata.
func benchRecordMetadata(rng *rand.Rand) push.LabelsAdapter {
	md := make(push.LabelsAdapter, len(benchMetadata))
	for i, key := range benchMetadata {
		var value string
		if key.cardinality == 0 {
			value = fmt.Sprintf("%016x", rng.Uint64())
		} else {
			value = fmt.Sprintf("%s-%d", key.name, rng.IntN(key.cardinality))
		}
		md[i] = push.LabelAdapter{Name: key.name, Value: value}
	}
	return md
}

// benchLogLine returns a logfmt line between size/2 and 3*size/2 bytes long,
// so lines average size bytes. Fixed keys and low-cardinality values sit next
// to random IDs and numbers, so the line compresses more like real logs than
// repeated bytes do.
func benchLogLine(rng *rand.Rand, size int) string {
	var sb strings.Builder
	sb.Grow(size + 16)
	fmt.Fprintf(&sb, "level=%s method=%s path=%s status=%d duration=%dms trace_id=%016x span_id=%08x user_id=%d msg=\"",
		benchLevels[rng.IntN(len(benchLevels))],
		benchMethods[rng.IntN(len(benchMethods))],
		benchPaths[rng.IntN(len(benchPaths))],
		benchStatus[rng.IntN(len(benchStatus))],
		rng.IntN(5000),
		rng.Uint64(),
		rng.Uint32(),
		rng.IntN(100000),
	)
	target := size/2 + rng.IntN(size)
	for sb.Len() < target {
		sb.WriteString(benchWords[rng.IntN(len(benchWords))])
		sb.WriteByte(' ')
	}
	sb.WriteByte('"')
	return sb.String()
}
