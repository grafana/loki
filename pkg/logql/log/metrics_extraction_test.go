package log

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_labelSampleExtractor_Extract(t *testing.T) {
	tests := []struct {
		name               string
		ex                 SampleExtractor
		in                 labels.Labels
		structuredMetadata labels.Labels
		want               float64
		wantLbs            labels.Labels
		wantOk             bool
		line               string
	}{
		{
			name: "convert float",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, nil, false, false, nil, NoopStage,
			)),
			in:      labels.FromStrings("foo", "15.0"),
			want:    15,
			wantLbs: labels.EmptyLabels(),
			wantOk:  true,
		},
		{
			name: "convert float as vector with no grouping",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, nil, false, true, nil, NoopStage,
			)),
			in:      labels.FromStrings("foo", "15.0", "bar", "buzz"),
			want:    15,
			wantLbs: labels.EmptyLabels(),
			wantOk:  true,
		},
		{
			name: "convert float without",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, []string{"bar", "buzz"}, true, false, nil, NoopStage,
			)),
			in: labels.FromStrings("foo", "10",
				"bar", "foo",
				"buzz", "blip",
				"namespace", "dev",
			),
			want:    10,
			wantLbs: labels.FromStrings("namespace", "dev"),
			wantOk:  true,
		},
		{
			name: "convert float with",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			in: labels.FromStrings("foo", "0.6",
				"bar", "foo",
				"buzz", "blip",
				"namespace", "dev",
			),
			want: 0.6,
			wantLbs: labels.FromStrings("bar", "foo",
				"buzz", "blip",
			),
			wantOk: true,
		},
		{
			name: "convert float with structured metadata",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, nil, false, false, nil, NoopStage,
			)),
			in:                 labels.EmptyLabels(),
			structuredMetadata: labels.FromStrings("foo", "15.0"),
			want:               15,
			wantLbs:            labels.EmptyLabels(),
			wantOk:             true,
		},
		{
			name: "convert float as vector with structured metadata with no grouping",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, nil, false, true, nil, NoopStage,
			)),
			in:                 labels.FromStrings("bar", "buzz"),
			structuredMetadata: labels.FromStrings("foo", "15.0", "buzz", "blip"),
			want:               15,
			wantLbs:            labels.EmptyLabels(),
			wantOk:             true,
		},
		{
			name: "convert float with structured metadata and grouping",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			in:                 labels.FromStrings("bar", "buzz", "namespace", "dev"),
			structuredMetadata: labels.FromStrings("foo", "15.0", "buzz", "blip"),
			want:               15,
			wantLbs:            labels.FromStrings("bar", "buzz", "buzz", "blip"),
			wantOk:             true,
		},
		{
			name: "convert float with structured metadata and grouping without",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, []string{"bar", "buzz"}, true, false, nil, NoopStage,
			)),
			in:                 labels.FromStrings("bar", "buzz", "namespace", "dev"),
			structuredMetadata: labels.FromStrings("foo", "15.0", "buzz", "blip"),
			want:               15,
			wantLbs:            labels.FromStrings("namespace", "dev"),
			wantOk:             true,
		},
		{
			name: "convert duration with",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertDuration, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			in: labels.FromStrings("foo", "500ms",
				"bar", "foo",
				"buzz", "blip",
				"namespace", "dev",
			),
			want: 0.5,
			wantLbs: labels.FromStrings("bar", "foo",
				"buzz", "blip",
			),
			wantOk: true,
		},
		{
			name: "convert duration with structured metadata",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertDuration, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			in: labels.FromStrings(
				"bar", "foo",
				"namespace", "dev",
			),
			structuredMetadata: labels.FromStrings("foo", "500ms", "buzz", "blip"),
			want:               0.5,
			wantLbs: labels.FromStrings("bar", "foo",
				"buzz", "blip",
			),
			wantOk: true,
		},
		{
			name: "convert bytes",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertBytes, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			in: labels.FromStrings("foo", "13 MiB",
				"bar", "foo",
				"buzz", "blip",
				"namespace", "dev",
			),
			want: 13 * 1024 * 1024,
			wantLbs: labels.FromStrings("bar", "foo",
				"buzz", "blip",
			),
			wantOk: true,
		},
		{
			name: "convert bytes without spaces",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertBytes, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			in: labels.FromStrings("foo", "13MiB",
				"bar", "foo",
				"buzz", "blip",
				"namespace", "dev",
			),
			want: 13 * 1024 * 1024,
			wantLbs: labels.FromStrings("bar", "foo",
				"buzz", "blip",
			),
			wantOk: true,
		},
		{
			name: "convert bytes with structured metadata",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertBytes, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			in: labels.FromStrings(
				"bar", "foo",
				"namespace", "dev",
			),
			structuredMetadata: labels.FromStrings("foo", "13 MiB", "buzz", "blip"),
			want:               13 * 1024 * 1024,
			wantLbs: labels.FromStrings("bar", "foo",
				"buzz", "blip",
			),
			wantOk: true,
		},
		{
			name: "not convertable",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			in: labels.FromStrings("foo", "not_a_number",
				"bar", "foo",
			),
			wantLbs: labels.FromStrings("__error__", "SampleExtractionErr",
				"__error_details__", "strconv.ParseFloat: parsing \"not_a_number\": invalid syntax",
				"bar", "foo",
				"foo", "not_a_number",
			),
			wantOk: true,
		},
		{
			name: "not convertable with structured metadata",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			in:                 labels.FromStrings("bar", "foo"),
			structuredMetadata: labels.FromStrings("foo", "not_a_number"),
			wantLbs: labels.FromStrings("__error__", "SampleExtractionErr",
				"__error_details__", "strconv.ParseFloat: parsing \"not_a_number\": invalid syntax",
				"bar", "foo",
				"foo", "not_a_number",
			),
			wantOk: true,
		},
		{
			name: "dynamic label, convert duration",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertDuration, []string{"bar", "buzz"}, false, false, []Stage{NewLogfmtParser(false, false)}, NoopStage,
			)),
			in:      labels.FromStrings("bar", "foo"),
			want:    0.1234,
			wantLbs: labels.FromStrings("bar", "foo"),
			wantOk:  true,
			line:    "foo=123.4ms",
		},
		{
			name: "dynamic label, not convertable",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertDuration, []string{"bar", "buzz"}, false, false, []Stage{NewLogfmtParser(false, false)}, NoopStage,
			)),
			in: labels.FromStrings("bar", "foo"),
			wantLbs: labels.FromStrings("__error__", "SampleExtractionErr",
				"__error_details__", "time: invalid duration \"not_a_number\"",
				"bar", "foo",
				"foo", "not_a_number",
			),
			wantOk: true,
			line:   "foo=not_a_number",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sample, ok := tt.ex.ForStream(tt.in).Process(0, []byte(tt.line), tt.structuredMetadata)
			require.Equal(t, tt.wantOk, ok)
			if ok {
				require.Equal(t, tt.want, sample.Value)
				require.Equal(t, tt.wantLbs, sample.Labels.Labels())
			}

			sample, ok = tt.ex.ForStream(tt.in).ProcessString(0, tt.line, tt.structuredMetadata)
			require.Equal(t, tt.wantOk, ok)
			if ok {
				require.Equal(t, tt.want, sample.Value)
				require.Equal(t, tt.wantLbs, sample.Labels.Labels())
			}
		})
	}
}

func Test_Extract_ExpectedLabels(t *testing.T) {
	ex := mustSampleExtractor(LabelExtractorWithStages("duration", ConvertDuration, []string{"foo"}, false, false, []Stage{NewJSONParser(false)}, NoopStage))

	sample, ok := ex.ForStream(labels.FromStrings("bar", "foo")).ProcessString(0, `{"duration":"20ms","foo":"json"}`, labels.EmptyLabels())
	require.True(t, ok)
	require.Equal(t, (20 * time.Millisecond).Seconds(), sample.Value)
	require.Equal(t, labels.FromStrings("foo", "json"), sample.Labels.Labels())

}
func TestLabelExtractorWithStages(t *testing.T) {

	// A helper type to check if particular logline should be skipped
	// during `ProcessLine` or got correct sample value extracted.
	type checkLine struct {
		logLine string
		skip    bool
		sample  float64
	}

	tests := []struct {
		name       string
		extractor  SampleExtractor
		checkLines []checkLine
		shouldFail bool
	}{
		{
			name: "with just logfmt and stringlabelfilter",
			// {foo="bar"} | logfmt | subqueries != "0" (note: "0", a stringlabelfilter)
			extractor: mustSampleExtractor(
				LabelExtractorWithStages("subqueries", ConvertFloat, []string{"foo"}, false, false, []Stage{NewLogfmtParser(false, false), NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotEqual, "subqueries", "0"))}, NoopStage),
			),
			checkLines: []checkLine{
				{logLine: "msg=hello subqueries=5", skip: false, sample: 5},
				{logLine: "msg=hello subqueries=0", skip: true},
				{logLine: "msg=hello ", skip: true}, // log lines doesn't contain the `subqueries` label
			},
		},
		{
			name: "with just logfmt and numeric labelfilter",
			// {foo="bar"} | logfmt | subqueries != 0 (note: "0", a numericLabelFilter)
			extractor: mustSampleExtractor(
				LabelExtractorWithStages("subqueries", ConvertFloat, []string{"foo"}, false, false, []Stage{NewLogfmtParser(false, false), NewNumericLabelFilter(LabelFilterNotEqual, "subqueries", 0)}, NoopStage),
			),
			checkLines: []checkLine{
				{logLine: "msg=hello subqueries=5", skip: false, sample: 5},
				{logLine: "msg=hello subqueries=0", skip: true},
				{logLine: "msg=hello ", skip: true}, // log lines doesn't contain the `subqueries` label
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, line := range tc.checkLines {
				sample, ok := tc.extractor.ForStream(labels.FromStrings("bar", "foo")).ProcessString(0, line.logLine, labels.EmptyLabels())
				skipped := !ok
				assert.Equal(t, line.skip, skipped, "line", line.logLine)
				if !skipped {
					assert.Equal(t, line.sample, sample.Value)

					// lbs shouldn't have __error__ = SampleExtractionError
					assert.Empty(t, sample.Labels.Labels())
					continue
				}

				assert.Equal(t, ExtractedSample{}, sample, "line", line.logLine)
			}
		})
	}
}

func mustSampleExtractor(ex SampleExtractor, err error) SampleExtractor {
	if err != nil {
		panic(err)
	}
	return ex
}

func TestNewLineSampleExtractor(t *testing.T) {
	se, err := NewLineSampleExtractor(CountExtractor, nil, nil, false, false)
	require.NoError(t, err)

	lbs := labels.FromStrings("namespace", "dev",
		"cluster", "us-central1",
	)

	sse := se.ForStream(lbs)
	sample, ok := sse.Process(0, []byte(`foo`), labels.EmptyLabels())
	require.True(t, ok)
	require.Equal(t, 1., sample.Value)
	assertLabelResult(t, lbs, sample.Labels)

	sample, ok = sse.ProcessString(0, `foo`, labels.EmptyLabels())
	require.True(t, ok)
	require.Equal(t, 1., sample.Value)
	assertLabelResult(t, lbs, sample.Labels)

	stage := mustFilter(NewFilter("foo", LineMatchEqual)).ToStage()
	se, err = NewLineSampleExtractor(BytesExtractor, []Stage{stage}, []string{"namespace"}, false, false)
	require.NoError(t, err)

	sse = se.ForStream(lbs)
	sample, ok = sse.Process(0, []byte(`foo`), labels.EmptyLabels())
	require.True(t, ok)
	require.Equal(t, 3., sample.Value)
	assertLabelResult(t, labels.FromStrings("namespace", "dev"), sample.Labels)

	sse = se.ForStream(lbs)
	_, ok = sse.Process(0, []byte(`nope`), labels.EmptyLabels())
	require.False(t, ok)
}

func TestNewLineSampleExtractorWithStructuredMetadata(t *testing.T) {
	lbs := labels.FromStrings("foo", "bar")
	structuredMetadata := labels.FromStrings("user", "bob")
	expectedLabelsResults := appendLabels(lbs, structuredMetadata)
	se, err := NewLineSampleExtractor(CountExtractor, []Stage{
		NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")),
		NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "user", "bob")),
	}, nil, false, false)
	require.NoError(t, err)

	sse := se.ForStream(lbs)
	sample, ok := sse.Process(0, []byte(`foo`), structuredMetadata)
	require.True(t, ok)
	require.Equal(t, 1., sample.Value)
	assertLabelResult(t, expectedLabelsResults, sample.Labels)

	sample, ok = sse.ProcessString(0, `foo`, structuredMetadata)
	require.True(t, ok)
	require.Equal(t, 1., sample.Value)
	assertLabelResult(t, expectedLabelsResults, sample.Labels)

	// test duplicated structured metadata with stream labels
	expectedLabelsResults = appendLabel(lbs, "foo_extracted", "baz")
	expectedLabelsResults = appendLabels(expectedLabelsResults, structuredMetadata)
	sample, ok = sse.Process(0, []byte(`foo`), appendLabel(structuredMetadata, "foo", "baz"))
	require.True(t, ok)
	require.Equal(t, 1., sample.Value)
	assertLabelResult(t, expectedLabelsResults, sample.Labels)

	sample, ok = sse.ProcessString(0, `foo`, appendLabel(structuredMetadata, "foo", "baz"))
	require.True(t, ok)
	require.Equal(t, 1., sample.Value)
	assertLabelResult(t, expectedLabelsResults, sample.Labels)

	se, err = NewLineSampleExtractor(BytesExtractor, []Stage{
		NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")),
		NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "user", "bob")),
		mustFilter(NewFilter("foo", LineMatchEqual)).ToStage(),
	}, []string{"foo"}, false, false)
	require.NoError(t, err)

	sse = se.ForStream(lbs)
	sample, ok = sse.Process(0, []byte(`foo`), structuredMetadata)
	require.True(t, ok)
	require.Equal(t, 3., sample.Value)
	assertLabelResult(t, labels.FromStrings("foo", "bar"), sample.Labels)

	sse = se.ForStream(lbs)
	_, ok = sse.Process(0, []byte(`nope`), labels.EmptyLabels())
	require.False(t, ok)
}

func TestLineSampleExtractor_ForStream_ShouldReturnOptimizedExtractorWhenOutputHasConstantLabels(t *testing.T) {
	lbs := labels.FromStrings("namespace", "dev", "cluster", "us-central1")

	// builderExtractor returns the non-specialized, per-line builder path for a grouping, to compare the
	// constant fast path against. (ForStream auto-selects the constant path, so there is no other way to
	// get the builder path for the same grouping.)
	builderExtractor := func(groups []string, streamLabels labels.Labels) StreamSampleExtractor {
		base := NewBaseLabelsBuilderWithGrouping(groups, NewParserHint(nil, groups, false, false, "", nil), false, false)
		return &streamLineSampleExtractor{Stage: NoopStage, LineExtractor: CountExtractor, builder: base.ForLabels(streamLabels, base.Hash(streamLabels))}
	}

	t.Run("grouping by a stream label is constant and matches the builder path", func(t *testing.T) {
		fast, err := NewLineSampleExtractor(CountExtractor, nil, []string{"namespace"}, false, false)
		require.NoError(t, err)

		fsse := fast.ForStream(lbs)
		require.IsType(t, &noopConstantLabelStreamExtractor{}, fsse)
		ref := builderExtractor([]string{"namespace"}, lbs)

		// BaseLabels is the stream's own identity (for StreamHash/dedup), not the grouped output.
		assertLabelResult(t, lbs, fsse.BaseLabels())
		require.Equal(t, ref.BaseLabels().String(), fsse.BaseLabels().String())

		for i := 0; i < 3; i++ {
			fs, ok := fsse.Process(int64(i), []byte("line"), labels.EmptyLabels())
			require.True(t, ok)
			rs, ok := ref.Process(int64(i), []byte("line"), labels.EmptyLabels())
			require.True(t, ok)
			require.Equal(t, 1., fs.Value)
			require.Equal(t, rs.Labels.String(), fs.Labels.String())
			assertLabelResult(t, labels.FromStrings("namespace", "dev"), fs.Labels)
		}
	})

	t.Run("structured metadata cannot change a stream-label grouping", func(t *testing.T) {
		// A line carrying SM "namespace" spills to namespace_extracted (stream labels win the base name),
		// so grouping by "namespace" still yields the stream value. The constant extractor ignores the SM
		// and stays in agreement with the builder path.
		fast, err := NewLineSampleExtractor(CountExtractor, nil, []string{"namespace"}, false, false)
		require.NoError(t, err)

		fsse := fast.ForStream(lbs)
		require.IsType(t, &noopConstantLabelStreamExtractor{}, fsse)

		sm := labels.FromStrings("namespace", "override", "trace_id", "t1")
		fs, ok := fsse.Process(0, []byte("line"), sm)
		require.True(t, ok)
		rs, ok := builderExtractor([]string{"namespace"}, lbs).Process(0, []byte("line"), sm)
		require.True(t, ok)
		require.Equal(t, rs.Labels.String(), fs.Labels.String())
		assertLabelResult(t, labels.FromStrings("namespace", "dev"), fs.Labels)
	})

	t.Run("noLabels grouping is constant and empty", func(t *testing.T) {
		se, err := NewLineSampleExtractor(CountExtractor, nil, nil, false, true)
		require.NoError(t, err)
		sse := se.ForStream(lbs)
		require.IsType(t, &noopConstantLabelStreamExtractor{}, sse)

		fs, ok := sse.Process(0, []byte("line"), labels.EmptyLabels())
		require.True(t, ok)
		require.Equal(t, 1., fs.Value)
		assertLabelResult(t, labels.EmptyLabels(), fs.Labels)
	})

	t.Run("bytes value follows the line while labels stay constant", func(t *testing.T) {
		se, err := NewLineSampleExtractor(BytesExtractor, nil, []string{"namespace"}, false, false)
		require.NoError(t, err)
		sse := se.ForStream(lbs)
		require.IsType(t, &noopConstantLabelStreamExtractor{}, sse)

		s1, ok := sse.Process(0, []byte("hello"), labels.EmptyLabels())
		require.True(t, ok)
		require.Equal(t, 5., s1.Value)
		assertLabelResult(t, labels.FromStrings("namespace", "dev"), s1.Labels)

		s2, ok := sse.ProcessString(0, "hi", labels.EmptyLabels())
		require.True(t, ok)
		require.Equal(t, 2., s2.Value)
		assertLabelResult(t, labels.FromStrings("namespace", "dev"), s2.Labels)
	})

	t.Run("ungrouped is not constant: the full label set carries metadata per line", func(t *testing.T) {
		se, err := NewLineSampleExtractor(CountExtractor, nil, nil, false, false)
		require.NoError(t, err)
		sse := se.ForStream(lbs)
		require.IsType(t, &streamLineSampleExtractor{}, sse)

		s, ok := sse.Process(0, []byte("x"), labels.FromStrings("trace_id", "t1"))
		require.True(t, ok)
		assertLabelResult(t, appendLabels(lbs, labels.FromStrings("trace_id", "t1")), s.Labels)
	})

	t.Run("grouping by a non-stream label (metadata) is not constant", func(t *testing.T) {
		se, err := NewLineSampleExtractor(CountExtractor, nil, []string{"trace_id"}, false, false)
		require.NoError(t, err)
		sse := se.ForStream(lbs) // lbs has no trace_id, so it comes from per-line metadata
		require.IsType(t, &streamLineSampleExtractor{}, sse)
	})

	t.Run("a line-filter stage keeps the constant fast path and still filters", func(t *testing.T) {
		// A line filter cannot change the labels (Stage.Hints().CanModifyLabels is false), so the grouping
		// stays constant. The filtered constant path runs the stage per line to drop non-matching lines but
		// emits the cached constant labels.
		se, err := NewLineSampleExtractor(CountExtractor, []Stage{mustFilter(NewFilter("keep", LineMatchEqual)).ToStage()}, []string{"namespace"}, false, false)
		require.NoError(t, err)
		sse := se.ForStream(lbs)
		require.IsType(t, &filteredConstantLabelStreamExtractor{}, sse)

		assertLabelResult(t, lbs, sse.BaseLabels())

		s, ok := sse.Process(0, []byte("keep me"), labels.EmptyLabels())
		require.True(t, ok)
		require.Equal(t, 1., s.Value)
		assertLabelResult(t, labels.FromStrings("namespace", "dev"), s.Labels)

		_, ok = sse.Process(0, []byte("drop me"), labels.EmptyLabels())
		require.False(t, ok)
	})

	t.Run("a label-modifying stage disables the fast path", func(t *testing.T) {
		// label_format can add or rename a label, so the output labels are no longer the stream labels.
		lf, err := NewLabelsFormatter([]LabelFmt{NewRenameLabelFmt("region", "cluster")})
		require.NoError(t, err)
		se, err := NewLineSampleExtractor(CountExtractor, []Stage{lf}, []string{"namespace"}, false, false)
		require.NoError(t, err)
		sse := se.ForStream(lbs)
		require.IsType(t, &streamLineSampleExtractor{}, sse)

		s, ok := sse.Process(0, []byte("line"), labels.EmptyLabels())
		require.True(t, ok)
		require.Equal(t, 1., s.Value)
	})

	t.Run("a filter reading a stream label keeps the constant fast path", func(t *testing.T) {
		// `| namespace="dev"` reads only a stream label, so the constant path runs it against the stream's
		// own labels. This stream matches, so every line is kept with the constant grouped labels.
		f := NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "namespace", "dev"))
		se, err := NewLineSampleExtractor(CountExtractor, []Stage{f}, []string{"namespace"}, false, false)
		require.NoError(t, err)
		sse := se.ForStream(lbs)
		require.IsType(t, &filteredConstantLabelStreamExtractor{}, sse)

		s, ok := sse.Process(0, []byte("line"), labels.EmptyLabels())
		require.True(t, ok)
		require.Equal(t, 1., s.Value)
		assertLabelResult(t, labels.FromStrings("namespace", "dev"), s.Labels)
	})

	t.Run("a filter reading a stream label drops a non-matching stream", func(t *testing.T) {
		// The fast path still applies (the filter reads a stream label), but this stream's namespace does
		// not match, so every line is dropped.
		f := NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "namespace", "other"))
		se, err := NewLineSampleExtractor(CountExtractor, []Stage{f}, []string{"namespace"}, false, false)
		require.NoError(t, err)
		sse := se.ForStream(lbs)
		require.IsType(t, &filteredConstantLabelStreamExtractor{}, sse)

		_, ok := sse.Process(0, []byte("line"), labels.EmptyLabels())
		require.False(t, ok)
	})

	t.Run("a filter reading a metadata label disables the fast path", func(t *testing.T) {
		// `| trace_id="t1"` reads a label the stream does not carry: it can only come from per-line
		// structured metadata, which the constant path does not build. The builder path must run instead.
		f := NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "trace_id", "t1"))
		se, err := NewLineSampleExtractor(CountExtractor, []Stage{f}, []string{"namespace"}, false, false)
		require.NoError(t, err)
		sse := se.ForStream(lbs)
		require.IsType(t, &streamLineSampleExtractor{}, sse)

		// The builder path adds the metadata, so the filter matches the line carrying trace_id=t1.
		s, ok := sse.Process(0, []byte("line"), labels.FromStrings("trace_id", "t1"))
		require.True(t, ok)
		require.Equal(t, 1., s.Value)
		assertLabelResult(t, labels.FromStrings("namespace", "dev"), s.Labels)

		// A line whose metadata does not match is dropped.
		_, ok = sse.Process(0, []byte("line"), labels.FromStrings("trace_id", "other"))
		require.False(t, ok)
	})

	t.Run("without grouping is not constant: the output keeps per-line metadata", func(t *testing.T) {
		// `without (pod)` keeps every other label, including per-line structured metadata, so the output
		// is not constant.
		se, err := NewLineSampleExtractor(CountExtractor, nil, []string{"pod"}, true, false)
		require.NoError(t, err)
		sse := se.ForStream(lbs)
		require.IsType(t, &streamLineSampleExtractor{}, sse)
	})

	t.Run("distinct streams get their own constant labels", func(t *testing.T) {
		se, err := NewLineSampleExtractor(CountExtractor, nil, []string{"app"}, false, false)
		require.NoError(t, err)

		a := labels.FromStrings("app", "a")
		b := labels.FromStrings("app", "b")
		ra, ok := se.ForStream(a).Process(0, []byte("x"), labels.EmptyLabels())
		require.True(t, ok)
		rb, ok := se.ForStream(b).Process(0, []byte("x"), labels.EmptyLabels())
		require.True(t, ok)
		assertLabelResult(t, a, ra.Labels)
		assertLabelResult(t, b, rb.Labels)
	})
}

// TestLineSampleExtractor_ForStream_FilteredConstantIsolatesFromSiblingStreams is a regression test: the
// filtered-constant fast path shares its LabelsBuilder overlay with the sibling normal-path extractors of
// the same SampleExtractor. A normal-path stream must not leave structured metadata in that overlay that a
// filtered-constant stream then reads.
func TestLineSampleExtractor_ForStream_FilteredConstantIsolatesFromSiblingStreams(t *testing.T) {
	se, err := NewLineSampleExtractor(CountExtractor,
		[]Stage{NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "namespace", "dev"))},
		[]string{"app"}, false, false)
	require.NoError(t, err)

	// Stream A carries namespace as a stream label → filtered-constant fast path.
	a := labels.FromStrings("app", "a", "namespace", "dev")
	// Stream B lacks namespace as a stream label; it arrives as metadata → normal builder path.
	b := labels.FromStrings("app", "b")

	seA := se.ForStream(a)
	seB := se.ForStream(b)
	require.IsType(t, &filteredConstantLabelStreamExtractor{}, seA)
	require.IsType(t, &streamLineSampleExtractor{}, seB)

	// Interleave A → B → A. B's line carries namespace="prod" in metadata; it does not match namespace="dev"
	// and is dropped, but it leaves namespace="prod" in the shared builder overlay.
	ra, ok := seA.Process(0, []byte("line"), labels.EmptyLabels())
	require.True(t, ok)
	assertLabelResult(t, labels.FromStrings("app", "a"), ra.Labels)

	_, ok = seB.Process(0, []byte("line"), labels.FromStrings("namespace", "prod"))
	require.False(t, ok)

	// A's own stream label namespace="dev" matches, so its line must still be kept — not dropped because
	// of B's leftover metadata.
	ra, ok = seA.Process(0, []byte("line"), labels.EmptyLabels())
	require.True(t, ok, "stream A must match its own namespace stream label, not stream B's leftover metadata")
	require.Equal(t, 1., ra.Value)
	assertLabelResult(t, labels.FromStrings("app", "a"), ra.Labels)
}

func TestStageHints_Merge(t *testing.T) {
	require.False(t, StageHints{CanModifyLabels: false}.Merge(StageHints{CanModifyLabels: false}).CanModifyLabels)
	require.True(t, StageHints{CanModifyLabels: true}.Merge(StageHints{CanModifyLabels: false}).CanModifyLabels)
	require.True(t, StageHints{CanModifyLabels: false}.Merge(StageHints{CanModifyLabels: true}).CanModifyLabels)
}

func TestReduceStages_FoldsHintsAcrossStages(t *testing.T) {
	lineFilter := mustFilter(NewFilter("x", LineMatchEqual)).ToStage() // cannot modify labels
	parser := NewLogfmtParser(false, false)                            // adds parsed labels

	// The fold is OR across every stage, regardless of order: one label-modifying stage taints the pipeline.
	require.True(t, ReduceStages([]Stage{parser, lineFilter}).Hints().CanModifyLabels)
	require.True(t, ReduceStages([]Stage{lineFilter, parser}).Hints().CanModifyLabels)
	require.False(t, ReduceStages([]Stage{lineFilter, lineFilter}).Hints().CanModifyLabels)
}

func TestBinaryLabelFilter_Hints(t *testing.T) {
	safe := NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "a", "1")) // reads only
	unsafe := NewBytesLabelFilter(LabelFilterGreaterThan, "size", 5)                 // sets __error__ on parse failure

	require.True(t, NewAndLabelFilter(safe, unsafe).Hints().CanModifyLabels)
	require.True(t, NewOrLabelFilter(unsafe, safe).Hints().CanModifyLabels)
	require.False(t, NewAndLabelFilter(safe, safe).Hints().CanModifyLabels)
}

func appendLabel(l labels.Labels, name, value string) labels.Labels {
	b := labels.NewBuilder(l)
	b.Set(name, value)
	return b.Labels()
}

func TestFilteringSampleExtractor(t *testing.T) {
	se := NewFilteringSampleExtractor([]PipelineFilter{
		newPipelineFilter(2, 4, labels.FromStrings("foo", "bar", "bar", "baz"), labels.EmptyLabels(), "e"),
		newPipelineFilter(3, 5, labels.FromStrings("baz", "foo"), labels.EmptyLabels(), "e"),
		newPipelineFilter(3, 5, labels.FromStrings("foo", "baz"), labels.FromStrings("user", "bob"), "e"),
	}, newStubExtractor())

	tt := []struct {
		name               string
		ts                 int64
		line               string
		labels             labels.Labels
		structuredMetadata labels.Labels
		ok                 bool
	}{
		{"it is after the timerange", 6, "line", labels.FromStrings("baz", "foo"), labels.EmptyLabels(), true},
		{"it is before the timerange", 1, "line", labels.FromStrings("baz", "foo"), labels.EmptyLabels(), true},
		{"it doesn't match the filter", 3, "all good", labels.FromStrings("baz", "foo"), labels.EmptyLabels(), true},
		{"it doesn't match all the selectors", 3, "line", labels.FromStrings("foo", "bar"), labels.EmptyLabels(), true},
		{"it doesn't match any selectors", 3, "line", labels.FromStrings("beep", "boop"), labels.EmptyLabels(), true},
		{"it matches all selectors", 3, "line", labels.FromStrings("foo", "bar", "bar", "baz"), labels.EmptyLabels(), false},
		{"it doesn't match all structured metadata", 3, "line", labels.FromStrings("foo", "baz"), labels.FromStrings("user", "alice"), true},
		{"it matches all structured metadata", 3, "line", labels.FromStrings("foo", "baz"), labels.FromStrings("user", "bob"), false},
		{"it tries all the filters", 5, "line", labels.FromStrings("baz", "foo"), labels.EmptyLabels(), false},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			_, ok := se.ForStream(test.labels).Process(test.ts, []byte(test.line), test.structuredMetadata)
			require.Equal(t, test.ok, ok)

			_, ok = se.ForStream(test.labels).ProcessString(test.ts, test.line, test.structuredMetadata)
			require.Equal(t, test.ok, ok)
		})
	}
}

func newStubExtractor() *stubExtractor {
	return &stubExtractor{
		sp: &stubStreamExtractor{},
	}
}

// A stub always returns the same data
type stubExtractor struct {
	sp *stubStreamExtractor
}

func (p *stubExtractor) ForStream(_ labels.Labels) StreamSampleExtractor {
	return p.sp
}

// A stub always returns the same data
type stubStreamExtractor struct{}

func (p *stubStreamExtractor) BaseLabels() LabelsResult {
	builder := NewBaseLabelsBuilder().ForLabels(labels.FromStrings("foo", "bar"), 0)
	return builder.LabelsResult()
}

func (p *stubStreamExtractor) Process(
	_ int64,
	_ []byte,
	structuredMetadata labels.Labels,
) (ExtractedSample, bool) {
	builder := NewBaseLabelsBuilder().ForLabels(labels.FromStrings("foo", "bar"), 0)
	builder.Add(StructuredMetadataLabel, structuredMetadata)
	return ExtractedSample{Value: 1.0, Labels: builder.LabelsResult()}, true
}

func (p *stubStreamExtractor) ProcessString(
	_ int64,
	_ string,
	structuredMetadata labels.Labels,
) (ExtractedSample, bool) {
	builder := NewBaseLabelsBuilder().ForLabels(labels.FromStrings("foo", "bar"), 0)
	builder.Add(StructuredMetadataLabel, structuredMetadata)
	return ExtractedSample{Value: 1.0, Labels: builder.LabelsResult()}, true
}

func (p *stubStreamExtractor) ReferencedStructuredMetadata() bool {
	return false
}

// TestForStream_HashCollisionKeepsStreamsDistinct guards against two streams whose labels collide on
// the builder's hash being conflated: each must get its own extractor/pipeline reporting its own
// labels, never the other stream's cached one.
func TestForStream_HashCollisionKeepsStreamsDistinct(t *testing.T) {
	a, b := collidingLabelPair(t)

	t.Run("line sample extractor", func(t *testing.T) {
		ex, err := NewLineSampleExtractor(CountExtractor, nil, nil, false, false)
		require.NoError(t, err)

		sa := ex.ForStream(a)
		sb := ex.ForStream(b)
		require.True(t, labels.Equal(a, sa.BaseLabels().Stream()), "stream A must expose A's labels")
		require.True(t, labels.Equal(b, sb.BaseLabels().Stream()), "stream B must expose B's labels (not A's)")
		// Re-fetching A after B must still return A's identity, not the colliding B entry.
		require.True(t, labels.Equal(a, ex.ForStream(a).BaseLabels().Stream()))
	})

	t.Run("label sample extractor", func(t *testing.T) {
		ex, err := LabelExtractorWithStages("pod", ConvertFloat, nil, false, false, nil, NoopStage)
		require.NoError(t, err)

		require.True(t, labels.Equal(a, ex.ForStream(a).BaseLabels().Stream()))
		require.True(t, labels.Equal(b, ex.ForStream(b).BaseLabels().Stream()))
	})

	t.Run("pipeline", func(t *testing.T) {
		p := NewNoopPipeline()
		require.True(t, labels.Equal(a, p.ForStream(a).BaseLabels().Stream()))
		require.True(t, labels.Equal(b, p.ForStream(b).BaseLabels().Stream()))
	})
}
