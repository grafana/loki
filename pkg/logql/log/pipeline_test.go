package log

import (
	"sort"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logqlmodel"
)

func TestNoopPipeline(t *testing.T) {
	lbs := labels.Labels{{Name: "foo", Value: "bar"}}
	l, lbr, matches := NewNoopPipeline().ForStream(lbs).Process(0, []byte(""))
	require.Equal(t, []byte(""), l)
	require.Equal(t, NewLabelsResult(lbs, lbs.Hash()), lbr)
	require.Equal(t, true, matches)

	ls, lbr, matches := NewNoopPipeline().ForStream(lbs).ProcessString(0, "")
	require.Equal(t, "", ls)
	require.Equal(t, NewLabelsResult(lbs, lbs.Hash()), lbr)
	require.Equal(t, true, matches)
}

func TestPipeline(t *testing.T) {
	lbs := labels.Labels{{Name: "foo", Value: "bar"}}
	p := NewPipeline([]Stage{
		NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")),
		newMustLineFormatter("lbs {{.foo}}"),
	})
	l, lbr, matches := p.ForStream(lbs).Process(0, []byte("line"))
	require.Equal(t, []byte("lbs bar"), l)
	require.Equal(t, NewLabelsResult(lbs, lbs.Hash()), lbr)
	require.Equal(t, true, matches)

	ls, lbr, matches := p.ForStream(lbs).ProcessString(0, "line")
	require.Equal(t, "lbs bar", ls)
	require.Equal(t, NewLabelsResult(lbs, lbs.Hash()), lbr)
	require.Equal(t, true, matches)

	l, lbr, matches = p.ForStream(labels.Labels{}).Process(0, []byte("line"))
	require.Equal(t, []byte(nil), l)
	require.Equal(t, nil, lbr)
	require.Equal(t, false, matches)

	ls, lbr, matches = p.ForStream(labels.Labels{}).ProcessString(0, "line")
	require.Equal(t, "", ls)
	require.Equal(t, nil, lbr)
	require.Equal(t, false, matches)
}

func TestFilteringPipeline(t *testing.T) {
	p := NewFilteringPipeline([]PipelineFilter{
		newPipelineFilter(2, 4, labels.Labels{{Name: "foo", Value: "bar"}, {Name: "bar", Value: "baz"}}, "e"),
		newPipelineFilter(3, 5, labels.Labels{{Name: "baz", Value: "foo"}}, "e"),
	}, newStubPipeline())

	tt := []struct {
		name              string
		ts                int64
		line              string
		inputStreamLabels labels.Labels
		ok                bool
	}{
		{"it is before the timerange", 1, "line", labels.Labels{{Name: "baz", Value: "foo"}}, true},
		{"it is after the timerange", 6, "line", labels.Labels{{Name: "baz", Value: "foo"}}, true},
		{"it doesn't match the filter", 3, "all good", labels.Labels{{Name: "baz", Value: "foo"}}, true},
		{"it doesn't match all the selectors", 3, "line", labels.Labels{{Name: "foo", Value: "bar"}}, true},
		{"it doesn't match any selectors", 3, "line", labels.Labels{{Name: "beep", Value: "boop"}}, true},
		{"it matches all selectors", 3, "line", labels.Labels{{Name: "foo", Value: "bar"}, {Name: "bar", Value: "baz"}}, false},
		{"it tries all the filters", 5, "line", labels.Labels{{Name: "baz", Value: "foo"}}, false},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			_, _, matches := p.ForStream(test.inputStreamLabels).Process(test.ts, []byte(test.line))
			require.Equal(t, test.ok, matches)

			_, _, matches = p.ForStream(test.inputStreamLabels).ProcessString(test.ts, test.line)
			require.Equal(t, test.ok, matches)
		})
	}
}

//nolint:unparam
func newPipelineFilter(start, end int64, lbls labels.Labels, filter string) PipelineFilter {
	var stages []Stage
	var matchers []*labels.Matcher
	for _, l := range lbls {
		m := labels.MustNewMatcher(labels.MatchEqual, l.Name, l.Value)
		matchers = append(matchers, m)
	}
	stages = append(stages, mustFilter(NewFilter(filter, labels.MatchEqual)).ToStage())

	return PipelineFilter{start, end, matchers, NewPipeline(stages)}
}

func newStubPipeline() *stubPipeline {
	return &stubPipeline{
		sp: &stubStreamPipeline{},
	}
}

// A stub always returns the same data
type stubPipeline struct {
	sp *stubStreamPipeline
}

func (p *stubPipeline) ForStream(labels labels.Labels) StreamPipeline {
	return p.sp
}

// A stub always returns the same data
type stubStreamPipeline struct{}

func (p *stubStreamPipeline) BaseLabels() LabelsResult {
	return nil
}

func (p *stubStreamPipeline) Process(ts int64, line []byte) ([]byte, LabelsResult, bool) {
	return nil, nil, true
}

func (p *stubStreamPipeline) ProcessString(ts int64, line string) (string, LabelsResult, bool) {
	return "", nil, true
}

var (
	resMatches    bool
	resLine       []byte
	resLineString string
	resLbs        LabelsResult
	resSample     float64
)

func TestDropLabelsPipeline(t *testing.T) {
	tests := []struct {
		name       string
		stages     []Stage
		lines      [][]byte
		wantLine   [][]byte
		wantLabels []labels.Labels
	}{
		{
			"drop __error__",
			[]Stage{
				NewLogfmtParser(),
				NewJSONParser(),
				NewDropLabels([]DropLabel{
					{
						nil,
						"__error__",
					},
					{
						nil,
						"__error_details__",
					},
				}),
			},
			[][]byte{
				[]byte(`level=info ts=2020-10-18T18:04:22.147378997Z caller=metrics.go:81 status=200`),
				[]byte(`{"app":"foo","namespace":"prod","pod":{"uuid":"foo","deployment":{"ref":"foobar"}}}`),
			},
			[][]byte{
				[]byte(`level=info ts=2020-10-18T18:04:22.147378997Z caller=metrics.go:81 status=200`),
				[]byte(`{"app":"foo","namespace":"prod","pod":{"uuid":"foo","deployment":{"ref":"foobar"}}}`),
			},
			[]labels.Labels{
				{
					{Name: "level", Value: "info"},
					{Name: "ts", Value: "2020-10-18T18:04:22.147378997Z"},
					{Name: "caller", Value: "metrics.go:81"},
					{Name: "status", Value: "200"},
				},
				{
					{Name: "app", Value: "foo"},
					{Name: "namespace", Value: "prod"},
					{Name: "pod_uuid", Value: "foo"},
					{Name: "pod_deployment_ref", Value: "foobar"},
				},
			},
		},
		{
			"drop __error__ with matching value",
			[]Stage{
				NewLogfmtParser(),
				NewJSONParser(),
				NewDropLabels([]DropLabel{
					{
						labels.MustNewMatcher(labels.MatchEqual, logqlmodel.ErrorLabel, errLogfmt),
						"",
					},
					{
						labels.MustNewMatcher(labels.MatchEqual, "status", "200"),
						"",
					},
					{
						nil,
						"app",
					},
				}),
			},
			[][]byte{
				[]byte(`level=info ts=2020-10-18T18:04:22.147378997Z caller=metrics.go:81 status=200`),
				[]byte(`{"app":"foo","namespace":"prod","pod":{"uuid":"foo","deployment":{"ref":"foobar"}}}`),
			},
			[][]byte{
				[]byte(`level=info ts=2020-10-18T18:04:22.147378997Z caller=metrics.go:81 status=200`),
				[]byte(`{"app":"foo","namespace":"prod","pod":{"uuid":"foo","deployment":{"ref":"foobar"}}}`),
			},
			[]labels.Labels{
				{
					{Name: "level", Value: "info"},
					{Name: "ts", Value: "2020-10-18T18:04:22.147378997Z"},
					{Name: "caller", Value: "metrics.go:81"},
					{Name: logqlmodel.ErrorLabel, Value: errJSON},
					{Name: logqlmodel.ErrorDetailsLabel, Value: "Value looks like object, but can't find closing '}' symbol"},
				},
				{
					{Name: "namespace", Value: "prod"},
					{Name: "pod_uuid", Value: "foo"},
					{Name: "pod_deployment_ref", Value: "foobar"},
					{Name: logqlmodel.ErrorDetailsLabel, Value: "logfmt syntax error at pos 2 : unexpected '\"'"},
				},
			},
		},
	}
	for _, tt := range tests {
		p := NewPipeline(tt.stages)
		sp := p.ForStream(labels.Labels{})
		for i, line := range tt.lines {
			_, finalLbs, _ := sp.Process(0, line)
			sort.Sort(tt.wantLabels[i])
			require.Equal(t, tt.wantLabels[i], finalLbs.Labels())
		}
	}

}
func Benchmark_Pipeline(b *testing.B) {
	b.ReportAllocs()

	stages := []Stage{
		mustFilter(NewFilter("metrics.go", labels.MatchEqual)).ToStage(),
		NewLogfmtParser(),
		NewAndLabelFilter(
			NewDurationLabelFilter(LabelFilterGreaterThan, "duration", 10*time.Millisecond),
			NewNumericLabelFilter(LabelFilterEqual, "status", 200.0),
		),
		mustNewLabelsFormatter([]LabelFmt{NewRenameLabelFmt("caller_foo", "caller"), NewTemplateLabelFmt("new", "{{.query_type}}:{{.range_type}}")}),
		NewJSONParser(),
		NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, logqlmodel.ErrorLabel, errJSON)),
		newMustLineFormatter("Q=>{{.query}},D=>{{.duration}}"),
	}
	p := NewPipeline(stages)
	line := []byte(`level=info ts=2020-10-18T18:04:22.147378997Z caller=metrics.go:81 org_id=29 traceID=29a0f088b047eb8c latency=fast query="{stream=\"stdout\",pod=\"loki-canary-xmjzp\"}" query_type=limited range_type=range length=20s step=1s duration=58.126671ms status=200 throughput_mb=2.496547 total_bytes_mb=0.145116`)
	lineString := string(line)
	lbs := labels.Labels{
		{Name: "cluster", Value: "ops-tool1"},
		{Name: "name", Value: "querier"},
		{Name: "pod", Value: "querier-5896759c79-q7q9h"},
		{Name: "stream", Value: "stderr"},
		{Name: "container", Value: "querier"},
		{Name: "namespace", Value: "loki-dev"},
		{Name: "job", Value: "loki-dev/querier"},
		{Name: "pod_template_hash", Value: "5896759c79"},
	}

	sp := p.ForStream(lbs)

	b.Run("pipeline bytes", func(b *testing.B) {
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			resLine, resLbs, resMatches = sp.Process(0, line)
		}
	})
	b.Run("pipeline string", func(b *testing.B) {
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			resLineString, resLbs, resMatches = sp.ProcessString(0, lineString)
		}
	})

	extractor, err := NewLineSampleExtractor(CountExtractor, stages, []string{"cluster", "level"}, false, false)
	require.NoError(b, err)
	ex := extractor.ForStream(lbs)
	b.Run("line extractor bytes", func(b *testing.B) {
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			resSample, resLbs, resMatches = ex.Process(0, line)
		}
	})
	b.Run("line extractor string", func(b *testing.B) {
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			resSample, resLbs, resMatches = ex.ProcessString(0, lineString)
		}
	})

	extractor, err = LabelExtractorWithStages("duration", "duration", []string{"cluster", "level"}, false, false, stages, NoopStage)
	require.NoError(b, err)
	ex = extractor.ForStream(lbs)

	b.Run("label extractor bytes", func(b *testing.B) {
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			resSample, resLbs, resMatches = ex.Process(0, line)
		}
	})
	b.Run("label extractor string", func(b *testing.B) {
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			resSample, resLbs, resMatches = ex.ProcessString(0, lineString)
		}
	})
}

func mustFilter(f Filterer, err error) Filterer {
	if err != nil {
		panic(err)
	}
	return f
}

func jsonBenchmark(b *testing.B, parser Stage) {
	b.ReportAllocs()

	p := NewPipeline([]Stage{
		mustFilter(NewFilter("metrics.go", labels.MatchEqual)).ToStage(),
		parser,
	})
	line := []byte(`{"ts":"2020-12-27T09:15:54.333026285Z","error":"action could not be completed", "context":{"file": "metrics.go"}}`)
	lbs := labels.Labels{
		{Name: "cluster", Value: "ops-tool1"},
		{Name: "name", Value: "querier"},
		{Name: "pod", Value: "querier-5896759c79-q7q9h"},
		{Name: "stream", Value: "stderr"},
		{Name: "container", Value: "querier"},
		{Name: "namespace", Value: "loki-dev"},
		{Name: "job", Value: "loki-dev/querier"},
		{Name: "pod_template_hash", Value: "5896759c79"},
	}
	b.ResetTimer()
	sp := p.ForStream(lbs)
	for n := 0; n < b.N; n++ {
		resLine, resLbs, resMatches = sp.Process(0, line)

		if !resMatches {
			b.Fatalf("resulting line not ok: %s\n", line)
		}

		if resLbs.Labels().Get("context_file") != "metrics.go" {
			b.Fatalf("label was not extracted correctly! %+v\n", resLbs)
		}
	}
}

func invalidJSONBenchmark(b *testing.B, parser Stage) {
	b.ReportAllocs()

	p := NewPipeline([]Stage{
		mustFilter(NewFilter("invalid json", labels.MatchEqual)).ToStage(),
		parser,
	})
	line := []byte(`invalid json`)
	b.ResetTimer()
	sp := p.ForStream(labels.Labels{})
	for n := 0; n < b.N; n++ {
		resLine, resLbs, resMatches = sp.Process(0, line)

		if !resMatches {
			b.Fatalf("resulting line not ok: %s\n", line)
		}

		if resLbs.Labels().Get(logqlmodel.ErrorLabel) != errJSON {
			b.Fatalf("no %s label found: %+v\n", logqlmodel.ErrorLabel, resLbs.Labels())
		}
	}
}

func BenchmarkJSONParser(b *testing.B) {
	jsonBenchmark(b, NewJSONParser())
}

func BenchmarkJSONParserInvalidLine(b *testing.B) {
	invalidJSONBenchmark(b, NewJSONParser())
}

func BenchmarkJSONExpressionParser(b *testing.B) {
	parser, err := NewJSONExpressionParser([]LabelExtractionExpr{
		NewLabelExtractionExpr("context_file", "context.file"),
	})
	if err != nil {
		b.Fatal("cannot create new JSON expression parser")
	}

	jsonBenchmark(b, parser)
}

func BenchmarkJSONExpressionParserInvalidLine(b *testing.B) {
	parser, err := NewJSONExpressionParser([]LabelExtractionExpr{
		NewLabelExtractionExpr("context_file", "some.expression"),
	})
	if err != nil {
		b.Fatal("cannot create new JSON expression parser")
	}

	invalidJSONBenchmark(b, parser)
}

func logfmtBenchmark(b *testing.B, parser Stage) {
	b.ReportAllocs()

	p := NewPipeline([]Stage{
		mustFilter(NewFilter("ts", labels.MatchEqual)).ToStage(),
		parser,
	})

	line := []byte(`level=info ts=2020-10-18T18:04:22.147378997Z caller=metrics.go:81 org_id=29 traceID=29a0f088b047eb8c latency=fast query="{stream=\"stdout\",pod=\"loki-canary-xmjzp\"}" query_type=limited range_type=range length=20s step=1s duration=58.126671ms status=200 throughput_mb=2.496547 total_bytes_mb=0.145116`)
	lbs := labels.Labels{
		{Name: "cluster", Value: "ops-tool1"},
		{Name: "name", Value: "querier"},
		{Name: "ts", Value: "2020-10-18T18:04:22.147378997Z"},
	}
	b.ResetTimer()
	sp := p.ForStream(lbs)
	for n := 0; n < b.N; n++ {
		resLine, resLbs, resMatches = sp.Process(0, line)

		if !resMatches {
			b.Fatalf("resulting line not ok: %s\n", line)
		}

		if resLbs.Labels().Get("ts") != "2020-10-18T18:04:22.147378997Z" {
			b.Fatalf("label was not extracted correctly! %+v\n", resLbs)
		}
	}
}

func BenchmarkLogfmtParser(b *testing.B) {
	logfmtBenchmark(b, NewLogfmtParser())
}

func BenchmarkLogfmtExpressionParser(b *testing.B) {
	parser, err := NewLogfmtExpressionParser([]LabelExtractionExpr{
		NewLabelExtractionExpr("timestamp", "ts"),
	})
	if err != nil {
		b.Fatal("cannot create new logfmt expression parser:", err.Error())
	}

	logfmtBenchmark(b, parser)
}
