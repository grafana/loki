package postings_test

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// BenchmarkScanner measures the scans of the postings sections of one index
// object. It scans for a stream selector, and it scans every row.
//
// The index object holds the postings of many logs sections. Each logs section
// has many streams, and each stream has several labels. The pod label is unique
// within a section. Each logs section also has the bloom filters of some
// metadata columns.
//
// Every scan must find something. A broken fixture then fails the benchmark
// instead of giving fast numbers.
func BenchmarkScanner(b *testing.B) {
	secs, bloomValue := benchSections(b)

	b.Run("MatchLabels/regex_app_and_cluster", func(b *testing.B) {
		matchers := compileMatchers(b,
			labels.MustNewMatcher(labels.MatchRegexp, "app", "app-1.*"),
			labels.MustNewMatcher(labels.MatchEqual, "cluster", "cluster-1"),
		)
		scanSections(b, secs, matchers, nil, nil, func(ctx context.Context, scanner *postings.Scanner) (int, error) {
			got, err := scanner.MatchLabels(ctx, nil, matchers)
			return len(got), err
		})
	})

	b.Run("LabelStreams/pod_filter", func(b *testing.B) {
		filters := compileMatchers(b, labels.MustNewMatcher(labels.MatchNotEqual, "pod", "pod-0"))
		scanSections(b, secs, nil, filters, nil, func(ctx context.Context, scanner *postings.Scanner) (int, error) {
			got, err := scanner.LabelStreams(ctx, nil, filters)
			return len(got), err
		})
	})

	b.Run("MatcherHits/bloom_and_label_names", func(b *testing.B) {
		predicates := []*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "trace_id", bloomValue),
			labels.MustNewMatcher(labels.MatchEqual, "app", "app-3"),
		}
		scanSections(b, secs, nil, nil, predicates, func(ctx context.Context, scanner *postings.Scanner) (int, error) {
			bloomHits, labelNames, err := scanner.MatcherHits(ctx, predicates)
			return len(bloomHits) + len(labelNames), err
		})
	})

	b.Run("RowReader/full_scan", func(b *testing.B) {
		ctx := b.Context()
		b.ReportAllocs()

		var rows int
		for b.Loop() {
			for _, sec := range secs {
				reader := postings.NewReader(postings.ReaderOptions{Columns: sec.Columns()})
				require.NoError(b, reader.Open(ctx))

				rowReader := postings.NewRowReader(ctx, reader)
				for rowReader.Next() {
					rows++
				}
				require.NoError(b, rowReader.Err())
				require.NoError(b, rowReader.Close())
			}
		}
		require.Positive(b, rows)
		b.ReportMetric(float64(rows)/float64(b.N), "rows/op")
	})
}

// scanSections scans every section once for each iteration of the benchmark. It
// opens the scanner readers of a section, runs scan with them, and closes them.
// scan returns the number of results that it found in the section.
func scanSections(
	b *testing.B,
	secs []*postings.Section,
	matchers, filters []postings.CompiledMatcher,
	predicates []*labels.Matcher,
	scan func(context.Context, *postings.Scanner) (int, error),
) {
	ctx := b.Context()
	b.ReportAllocs()

	for b.Loop() {
		var found int
		for _, sec := range secs {
			readers, err := postings.NewScannerReaders(sec, matchers, filters, predicates, nil, nil, nil)
			require.NoError(b, err)
			require.NoError(b, readers.Open(ctx))

			n, err := scan(ctx, postings.NewScanner(sec, readers))
			require.NoError(b, err)
			found += n

			require.NoError(b, readers.Close())
		}
		require.Positive(b, found)
	}
}

func compileMatchers(b *testing.B, matchers ...*labels.Matcher) []postings.CompiledMatcher {
	b.Helper()

	compiled := make([]postings.CompiledMatcher, 0, len(matchers))
	for _, m := range matchers {
		cm, err := postings.CompileMatcher(m)
		require.NoError(b, err)
		compiled = append(compiled, cm)
	}
	return compiled
}

// benchSections builds the index object of [BenchmarkScanner]. It returns the
// postings sections, and a value of the trace_id bloom filter of the first logs
// section.
func benchSections(b *testing.B) ([]*postings.Section, string) {
	b.Helper()

	const (
		logsObjects        = 50
		sectionsPerObject  = 4
		streamsPerSection  = 500
		bloomCardinality   = 20000
		bloomValueVariants = 1 << 20
	)
	type label struct {
		name        string
		cardinality int // The number of values. Zero means one value for each stream.
	}
	labelDefs := []label{{"cluster", 5}, {"namespace", 20}, {"app", 50}, {"pod", 0}, {"job", 30}, {"env", 3}}

	rnd := rand.New(rand.NewSource(1))
	builder := postings.NewBuilder(nil, 0, 0, 1<<20)
	base := time.Unix(1_700_000_000, 0).UTC()
	var bloomValue string

	for o := range logsObjects {
		path := fmt.Sprintf("objects/ab/cdef0123456789%04d", o)
		for s := range sectionsPerObject {
			for stream := range streamsPerSection {
				ts := base.Add(time.Duration(rnd.Intn(3600)) * time.Second)
				for _, def := range labelDefs {
					value := fmt.Sprintf("%s-%d", def.name, stream)
					if def.cardinality > 0 {
						value = fmt.Sprintf("%s-%d", def.name, rnd.Intn(def.cardinality))
					}
					builder.ObserveLabelPosting(postings.LabelObservation{
						ObjectPath:       path,
						SectionIndex:     int64(s),
						ColumnName:       def.name,
						LabelValue:       value,
						StreamID:         int64(stream),
						Time:             postings.At(ts).Extend(ts.Add(time.Minute)),
						UncompressedSize: 1000,
					})
				}
			}

			for _, column := range []string{"trace_id", "user_id", "request_id"} {
				builder.PrepareBloomColumn(path, int64(s), column, bloomCardinality, 0)
				for stream := range streamsPerSection {
					value := fmt.Sprintf("%s-%d-%d", column, s, rnd.Intn(bloomValueVariants))
					if o == 0 && s == 0 && column == "trace_id" && stream == 0 {
						bloomValue = value
					}
					require.NoError(b, builder.ObserveBloomPosting(postings.BloomObservation{
						ObjectPath:   path,
						SectionIndex: int64(s),
						ColumnName:   column,
						Value:        value,
						StreamID:     int64(stream),
						Timestamp:    base,
					}))
				}
			}
		}
	}

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(b, objBuilder.Append(builder))
	obj, closer, err := objBuilder.Flush()
	require.NoError(b, err)
	b.Cleanup(func() { require.NoError(b, closer.Close()) })

	var secs []*postings.Section
	for _, section := range obj.Sections() {
		if !postings.CheckSection(section) {
			continue
		}
		sec, err := postings.Open(b.Context(), section)
		require.NoError(b, err)
		secs = append(secs, sec)
	}
	return secs, bloomValue
}
