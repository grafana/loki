package syntax

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

func Test_Extractor(t *testing.T) {
	t.Parallel()
	for _, tc := range []string{
		`rate( ( {job="mysql"} |="error" !="timeout" ) [10s] )`,
		`absent_over_time( ( {job="mysql"} |="error" !="timeout" ) [10s] )`,
		`absent_over_time( ( {job="mysql"} |="error" !="timeout" ) [10s] offset 30s )`,
		`sum without(a) ( rate ( ( {job="mysql"} |="error" !="timeout" ) [10s] ) )`,
		`sum by(a) (rate( ( {job="mysql"} |="error" !="timeout" ) [10s] ) )`,
		`sum(count_over_time({job="mysql"}[5m]))`,
		`sum(count_over_time({job="mysql"} | json [5m]))`,
		`sum(count_over_time({job="mysql"} | logfmt [5m]))`,
		`sum(count_over_time({job="mysql"} | pattern "<foo> bar <buzz>" [5m]))`,
		`sum(count_over_time({job="mysql"} | regexp "(?P<foo>foo|bar)" [5m]))`,
		`sum(count_over_time({job="mysql"} | regexp "(?P<foo>foo|bar)" [5m] offset 1h))`,
		`topk(10,sum(rate({region="us-east1"}[5m])) by (name))`,
		`topk by (name)(10,sum(rate({region="us-east1"}[5m])))`,
		`avg( rate( ( {job="nginx"} |= "GET" ) [10s] ) ) by (region)`,
		`avg(min_over_time({job="nginx"} |= "GET" | unwrap foo[10s])) by (region)`,
		`sum by (cluster) (count_over_time({job="mysql"}[5m]))`,
		`sum by (cluster) (count_over_time({job="mysql"}[5m])) / sum by (cluster) (count_over_time({job="postgres"}[5m])) `,
		`
			sum by (cluster) (count_over_time({job="postgres"}[5m])) /
			sum by (cluster) (count_over_time({job="postgres"}[5m])) /
			sum by (cluster) (count_over_time({job="postgres"}[5m]))
			`,
		`sum by (cluster) (count_over_time({job="mysql"}[5m])) / min(count_over_time({job="mysql"}[5m])) `,
		`sum by (job) (
				count_over_time({namespace="tns"} |= "level=error"[5m])
			/
				count_over_time({namespace="tns"}[5m])
			)`,
		`stdvar_over_time({app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m])`,
		`sum_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms|unwrap latency [5m])`,
		`sum by (job) (
				sum_over_time({namespace="tns"} |= "level=error" | json | foo=5 and bar<25ms | unwrap latency[5m])
			/
				count_over_time({namespace="tns"} | logfmt | label_format foo=bar[5m])
			)`,
		`sum by (job) (
				sum_over_time({namespace="tns"} |= "level=error" | json | foo=5 and bar<25ms | unwrap bytes(latency)[5m])
			/
				count_over_time({namespace="tns"} | logfmt | label_format foo=bar[5m])
			)`,
		`sum by (job) (
				sum_over_time(
					{namespace="tns"} |= "level=error" | json | avg=5 and bar<25ms | unwrap duration(latency) [5m]
				)
			/
				count_over_time({namespace="tns"} | logfmt | label_format foo=bar[5m])
			)`,
		`sum_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms | unwrap latency | __error__!~".*" | foo >5[5m])`,
		`absent_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms | unwrap latency | __error__!~".*" | foo >5[5m])`,
		`absent_over_time({namespace="tns"} |= "level=error" | json [5m])`,
		`sum by (job) (
				sum_over_time(
					{namespace="tns"} |= "level=error" | json | avg=5 and bar<25ms | unwrap duration(latency)  | __error__!~".*" [5m]
				)
			/
				count_over_time({namespace="tns"} | logfmt | label_format foo=bar[5m])
			)`,
		`label_replace(
				sum by (job) (
					sum_over_time(
						{namespace="tns"} |= "level=error" | json | avg=5 and bar<25ms | unwrap duration(latency)  | __error__!~".*" [5m]
					)
				/
					count_over_time({namespace="tns"} | logfmt | label_format foo=bar[5m])
				),
				"foo",
				"$1",
				"service",
				"(.*):.*"
			)
			`,
		`label_replace(
				sum by (job) (
					sum_over_time(
						{namespace="tns"} |= "level=error" | json | avg=5 and bar<25ms | unwrap duration(latency)  | __error__!~".*" [5m] offset 1h
					)
				/
					count_over_time({namespace="tns"} | logfmt | label_format foo=bar[5m] offset 1h)
				),
				"foo",
				"$1",
				"service",
				"(.*):.*"
			)
			`,
	} {
		t.Run(tc, func(t *testing.T) {
			expr, err := ParseSampleExpr(tc)
			require.Nil(t, err)
			extractors, err := expr.Extractors()
			require.Nil(t, err)
			require.Len(t, extractors, 1)
		})
	}
}

func Test_MultiVariantExpr_Extractors(t *testing.T) {
	t.Parallel()

	type sample struct {
		value  int
		labels labels.Labels
	}

	t.Run("single variant is equivalent to non-variant", func(t *testing.T) {
		for _, tc := range []struct {
			query        string
			variantQuery string
			testLine     string
		}{
			{
				query:        `count_over_time({app="foo"} | json [5m])`,
				variantQuery: `variants(count_over_time({app="foo"} | json [5m])) of ({app="foo"} | json [5m])`,
				testLine:     `{"error": true, "method": "GET", "size": "1024", "latency": "250s"}`,
			},
			{
				query:        `sum by (method) (count_over_time({app="foo"} | json [5m]))`,
				variantQuery: `variants(sum by (method) (count_over_time({app="foo"} | json [5m]))) of ({app="foo"} | json [5m])`,
				testLine:     `{"error": true, "method": "GET", "size": "1024", "latency": "250s"}`,
			},
			{
				query:        `sum by (method) (sum_over_time({app="foo"} | logfmt | unwrap duration(latency) [5m]))`,
				variantQuery: `variants(sum by (method) (sum_over_time({app="foo"} | logfmt | unwrap duration(latency) [5m]))) of ({app="foo"} | json [5m])`,
				testLine:     `error=true method="GET" size=1024 latency="250s"`,
			},
		} {
			t.Run(tc.query, func(t *testing.T) {
				now := time.Now()
				expr, err := ParseSampleExpr(tc.query)
				require.NoError(t, err)

				extractors, err := expr.Extractors()
				require.NoError(t, err)
				require.Len(t, extractors, 1, "should return a single consolidated extractor")

				// Test that the extractor actually works with a mock stream
				lbls := labels.FromStrings("app", "foo")

				streamExtractor := extractors[0].ForStream(lbls)
				require.NotNil(t, streamExtractor, "stream extractor should not be nil")

				samples, ok := streamExtractor.Process(now.UnixNano(), []byte(tc.testLine), labels.EmptyLabels())
				require.True(t, ok)

				seen := make(map[string]float64, len(samples))
				for _, s := range samples {
					lbls := s.Labels.Labels()
					seen[lbls.String()] = s.Value
				}

				mvExpr, err := ParseSampleExpr(tc.variantQuery)
				require.NoError(t, err)

				extractors, err = mvExpr.Extractors()
				require.NoError(t, err)
				require.Len(t, extractors, 1, "should return a single consolidated extractor")

				streamExtractor = extractors[0].ForStream(lbls)
				require.NotNil(t, streamExtractor, "multi-variant stream extractor should not be nil")

				mvSamples, ok := streamExtractor.Process(now.UnixNano(), []byte(tc.testLine), labels.EmptyLabels())
				require.True(t, ok)

				// remove variant label
				mvSeen := make(map[string]float64, len(mvSamples))
				for _, s := range mvSamples {
					lbls := s.Labels.Labels()
					newLbls := labels.NewScratchBuilder(lbls.Len())
					lbls.Range(func(lbl labels.Label) {
						if lbl.Name == "__variant__" {
							return
						}
						newLbls.Add(lbl.Name, lbl.Value)
					})
					mvSeen[newLbls.Labels().String()] = s.Value
				}

				require.Equal(t, seen, mvSeen)
			})
		}
	})

	t.Run("multiple extractors", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			query    string
			testLine string
			expected []sample
		}{
			{
				name: "two logfmt variants with common filter and parser",
				query: `variants(
					count_over_time({app="foo"} |= "error" | logfmt [5m]),
					bytes_over_time({app="foo"} |= "error" | logfmt [5m])
				) of ({app="foo"} |= "error" | logfmt [5m])`,
				testLine: "error=true method=GET status=500 size=1024 latency=250ms",
				expected: []sample{
					{
						1,
						labels.FromStrings(
							constants.VariantLabel, "0",
							"app", "foo",
							"error", "true",
							"latency", "250ms",
							"method", "GET",
							"size", "1024",
							"status", "500",
						),
					},
					{
						len("error=true method=GET status=500 size=1024 latency=250ms"),
						labels.FromStrings(
							constants.VariantLabel, "1",
							"app", "foo",
							"error", "true",
							"latency", "250ms",
							"method", "GET",
							"size", "1024",
							"status", "500",
						),
					},
				},
			},
			{
				name: "two variants with different unwraps",
				query: `variants(
					sum_over_time({app="foo"} |= "error" | json | unwrap status [5m]),
					sum_over_time({app="foo"} |= "error" | json | unwrap duration(latency) [5m])
				) of ({app="foo"} |= "error" | json [5m])`,
				testLine: `{"error": true, "method": "GET", "status": 500, "latency": "250s"}`,
				expected: []sample{
					{
						500,
						labels.FromStrings(
							constants.VariantLabel, "0",
							"app", "foo",
							"error", "true",
							"latency", "250s",
							"method", "GET",
						),
					},
					{
						250,
						labels.FromStrings(
							constants.VariantLabel, "1",
							"app", "foo",
							"error", "true",
							"method", "GET",
							"status", "500",
						),
					},
				},
			},
			{
				name: "three json variants with different label extractors",
				query: `variants(
					sum_over_time({app="foo"} |= "error" | json | unwrap bytes(size) [5m]),
					sum_over_time({app="foo"} |= "error" | json | unwrap duration(latency) [5m]),
					count_over_time({app="foo"} |= "error" | json [5m])
				) of ({app="foo"} |= "error" | json [5m])`,
				testLine: `{"error": true, "method": "GET", "size": "1024", "latency": "250s"}`,
				expected: []sample{
					{
						1024,
						labels.FromStrings(
							constants.VariantLabel, "0",
							"app", "foo",
							"error", "true",
							"latency", "250s",
							"method", "GET",
						),
					},
					{
						250,
						labels.FromStrings(
							constants.VariantLabel, "1",
							"app", "foo",
							"error", "true",
							"method", "GET",
							"size", "1024",
						),
					},
					{
						1,
						labels.FromStrings(
							constants.VariantLabel, "2",
							"app", "foo",
							"error", "true",
							"latency", "250s",
							"method", "GET",
							"size", "1024",
						),
					},
				},
			},
			{
				name: "three logfmt variants with different extractors",
				query: `variants(
					count_over_time({app="foo"} |= "error" | logfmt [5m]),
					bytes_over_time({app="foo"} |= "error" | logfmt [5m]),
					sum_over_time({app="foo"} |= "error" | logfmt | unwrap size [5m])
				) of ({app="foo"} |= "error" | logfmt [5m])`,
				testLine: "error=true method=GET status=500 size=1024 latency=250ms",
				expected: []sample{
					{
						1,
						labels.FromStrings(
							constants.VariantLabel, "0",
							"app", "foo",
							"error", "true",
							"latency", "250ms",
							"method", "GET",
							"size", "1024",
							"status", "500",
						),
					},
					{
						len("error=true method=GET status=500 size=1024 latency=250ms"),
						labels.FromStrings(
							constants.VariantLabel, "1",
							"app", "foo",
							"error", "true",
							"latency", "250ms",
							"method", "GET",
							"size", "1024",
							"status", "500",
						),
					},
					{
						1024,
						labels.FromStrings(
							constants.VariantLabel, "2",
							"app", "foo",
							"error", "true",
							"latency", "250ms",
							"method", "GET",
							"status", "500",
						),
					},
				},
			},
			{
				name: "variants with nested expressions",
				query: `variants(
					sum by (status) (count_over_time({app="foo"} |= "error" | logfmt [5m])),
					bytes_over_time({app="foo"} |= "error" | logfmt [5m]),
					sum by (method) (sum_over_time({app="foo"} |= "error" | logfmt | unwrap size [5m]))
				) of ({app="foo"} |= "error" | logfmt [5m])`,
				testLine: "error=true method=GET status=500 size=1024 latency=250ms",
				expected: []sample{
					{
						1,
						labels.FromStrings(
							constants.VariantLabel, "0",
							"status", "500",
						),
					},
					{
						len("error=true method=GET status=500 size=1024 latency=250ms"),
						labels.FromStrings(
							constants.VariantLabel, "1",
							"app", "foo",
							"error", "true",
							"latency", "250ms",
							"method", "GET",
							"size", "1024",
							"status", "500",
						),
					},
					{
						1024,
						labels.FromStrings(
							constants.VariantLabel, "2",
							"method", "GET",
						),
					},
				},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				now := time.Now()
				expr, err := ParseSampleExpr(tc.query)
				require.NoError(t, err)

				extractors, err := expr.Extractors()
				require.NoError(t, err)
				require.Len(t, extractors, 1, "should return a single consolidated extractor")

				// Test that the extractor actually works with a mock stream
				lbls := labels.FromStrings("app", "foo")

				streamExtractor := extractors[0].ForStream(lbls)
				require.NotNil(t, streamExtractor, "stream extractor should not be nil")

				samples, ok := streamExtractor.Process(now.UnixNano(), []byte(tc.testLine), labels.EmptyLabels())
				require.True(t, ok)

				expectedSamples := make(map[string]float64, len(tc.expected))
				for _, s := range tc.expected {
					expectedSamples[s.labels.String()] = float64(s.value)
				}

				seenSamples := make(map[string]float64, len(samples))
				for _, s := range samples {
					seenSamples[s.Labels.String()] = s.Value
				}

				require.Equal(t, expectedSamples, seenSamples)
			})
		}
	})
}
