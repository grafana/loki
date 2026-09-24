package log_test

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

func TestNewLabelFilterHints(t *testing.T) {
	logfmt := log.NewLogfmtParser(false, false)
	json := log.NewJSONParser(false)
	fooIsBar := stringFilter(labels.MatchEqual, "foo", "bar")
	fooIsIP := log.NewIPLabelFilter("10.0.0.0/8", "foo", log.LabelFilterEqual)

	// Every case extracts foo=nope. The early check drops the line only when the parser got a
	// filter on foo that fails.
	for _, tt := range []struct {
		name     string
		stages   []log.Stage
		parser   log.Stage // stages[0] when nil
		wantDrop bool
	}{
		{
			name:     "drops the line when the value fails a filter after the parser",
			stages:   []log.Stage{logfmt, fooIsBar},
			wantDrop: true,
		},
		{
			name:   "keeps the line when the filter is before the parser",
			stages: []log.Stage{fooIsBar, logfmt},
			parser: logfmt,
		},
		{
			name:   "keeps the line when the parser is not in the pipeline",
			stages: []log.Stage{logfmt, fooIsBar},
			parser: log.NewLogfmtParser(false, false),
		},
		{
			name:   "keeps the line when the parser appears twice in the pipeline",
			stages: []log.Stage{logfmt, fooIsBar, logfmt},
		},
		{
			name:   "keeps the line when label_format sets the label before the filter",
			stages: []log.Stage{logfmt, labelsFormatter(t, log.NewTemplateLabelFmt("foo", "bar")), fooIsBar},
		},
		{
			name:   "keeps the line when label_format renames another label to it before the filter",
			stages: []log.Stage{logfmt, labelsFormatter(t, log.NewRenameLabelFmt("foo", "bar")), fooIsBar},
		},
		{
			name:   "keeps the line when label_format renames it to another label before the filter",
			stages: []log.Stage{logfmt, labelsFormatter(t, log.NewRenameLabelFmt("baz", "foo")), fooIsBar},
		},
		{
			name:     "drops the line when label_format sets another label before the filter",
			stages:   []log.Stage{logfmt, labelsFormatter(t, log.NewTemplateLabelFmt("baz", "bar")), fooIsBar},
			wantDrop: true,
		},
		{
			name:   "keeps the line when drop removes the label before the filter",
			stages: []log.Stage{logfmt, log.NewDropLabels([]log.NamedLabelMatcher{log.NewNamedLabelMatcher(nil, "foo")}), fooIsBar},
		},
		{
			name: "keeps the line when drop removes the label for some values before the filter",
			stages: []log.Stage{
				logfmt,
				log.NewDropLabels([]log.NamedLabelMatcher{log.NewNamedLabelMatcher(labels.MustNewMatcher(labels.MatchEqual, "foo", "nope"), "")}),
				fooIsBar,
			},
		},
		{
			name:     "drops the line when drop removes another label before the filter",
			stages:   []log.Stage{logfmt, log.NewDropLabels([]log.NamedLabelMatcher{log.NewNamedLabelMatcher(nil, "baz")}), fooIsBar},
			wantDrop: true,
		},
		{
			name:   "keeps the line when keep does not list the label before the filter",
			stages: []log.Stage{logfmt, log.NewKeepLabels([]log.NamedLabelMatcher{log.NewNamedLabelMatcher(nil, "baz")}), fooIsBar},
		},
		{
			name: "keeps the line when keep lists the label only with a matcher before the filter",
			stages: []log.Stage{
				logfmt,
				log.NewKeepLabels([]log.NamedLabelMatcher{log.NewNamedLabelMatcher(labels.MustNewMatcher(labels.MatchEqual, "foo", "nope"), "")}),
				fooIsBar,
			},
		},
		{
			name:     "drops the line when keep lists the label by name before the filter",
			stages:   []log.Stage{logfmt, log.NewKeepLabels([]log.NamedLabelMatcher{log.NewNamedLabelMatcher(nil, "foo")}), fooIsBar},
			wantDrop: true,
		},
		{
			name:   "keeps the line when a json expression parser sets the label before the filter",
			stages: []log.Stage{logfmt, jsonExpressionParser(t, "foo", "bar"), fooIsBar},
		},
		{
			name:   "keeps the line when a logfmt expression parser sets the label before the filter",
			stages: []log.Stage{logfmt, logfmtExpressionParser(t, "foo", "bar"), fooIsBar},
		},
		{
			name:     "drops the line when an expression parser sets another label before the filter",
			stages:   []log.Stage{logfmt, jsonExpressionParser(t, "baz", "bar"), fooIsBar},
			wantDrop: true,
		},
		{
			name:     "drops the line when another parser runs before the filter",
			stages:   []log.Stage{logfmt, json, fooIsBar},
			wantDrop: true,
		},
		{
			name:   "keeps the line when unpack runs before the filter",
			stages: []log.Stage{logfmt, log.NewUnpackParser(), fooIsBar},
		},
		{
			name:     "drops the line when line_format runs before the filter",
			stages:   []log.Stage{logfmt, lineFormatter(t, "{{.foo}}"), fooIsBar},
			wantDrop: true,
		},
		{
			name:     "drops the line when a numeric filter on another label runs before the filter",
			stages:   []log.Stage{logfmt, log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "baz", 1), fooIsBar},
			wantDrop: true,
		},
		{
			name:     "drops the line when a later filter on the same label fails",
			stages:   []log.Stage{logfmt, stringFilter(labels.MatchNotEqual, "foo", ""), fooIsBar},
			wantDrop: true,
		},
		{
			name:     "drops the line when a binary filter on the label fails",
			stages:   []log.Stage{logfmt, log.NewOrLabelFilter(fooIsBar, stringFilter(labels.MatchEqual, "foo", "baz"))},
			wantDrop: true,
		},
		{
			name:   "keeps the line when a binary filter also reads another label",
			stages: []log.Stage{logfmt, log.NewOrLabelFilter(fooIsBar, stringFilter(labels.MatchEqual, "baz", "x"))},
		},
		{
			name:     "drops the line when an ip filter fails after logfmt",
			stages:   []log.Stage{logfmt, fooIsIP},
			wantDrop: true,
		},
		{
			name:   "keeps the line when an ip filter follows json, which can set __error__ later in the line",
			stages: []log.Stage{json, fooIsIP},
		},
		{
			name:   "keeps the line when an ip filter follows strict logfmt, which can set __error__ later in the line",
			stages: []log.Stage{log.NewLogfmtParser(true, false), fooIsIP},
		},
		{
			name:   "keeps the line when a numeric filter, which can set __error__, runs before an ip filter",
			stages: []log.Stage{logfmt, log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "baz", 1), fooIsIP},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parser := tt.parser
			if parser == nil {
				parser = tt.stages[0]
			}
			lb := log.NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
			lb.Set(log.ParsedLabel, "foo", "nope")

			filters := log.NewLabelFilterHints(tt.stages).ForParser(parser)
			require.Equal(t, !tt.wantDrop, filters.ShouldContinueParsingLine("foo", lb))
		})
	}

	t.Run("keeps the line when nil hints have no filters", func(t *testing.T) {
		lb := log.NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
		lb.Set(log.ParsedLabel, "foo", "nope")

		var hints *log.LabelFilterHints
		require.True(t, hints.ForParser(logfmt).ShouldContinueParsingLine("foo", lb))
	})

	t.Run("keeps the line and sets no error when a numeric filter cannot convert the value", func(t *testing.T) {
		lb := log.NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
		lb.Set(log.ParsedLabel, "foo", "nope")

		filters := log.NewLabelFilterHints([]log.Stage{logfmt, log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "foo", 1)}).ForParser(logfmt)
		require.True(t, filters.ShouldContinueParsingLine("foo", lb))
		require.False(t, lb.HasErr())
		require.False(t, lb.HasErrorDetails())
	})

	t.Run("keeps the line when the parser extracts a key named __error__", func(t *testing.T) {
		lb := log.NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
		lb.Set(log.ParsedLabel, logqlmodel.ErrorLabel, "x")

		filters := log.NewLabelFilterHints([]log.Stage{logfmt, stringFilter(labels.MatchNotEqual, logqlmodel.ErrorLabel, "")}).ForParser(logfmt)
		require.True(t, filters.ShouldContinueParsingLine(logqlmodel.ErrorLabel, lb))
	})

	t.Run("drops the line when the parse error fails a filter after the parser", func(t *testing.T) {
		strict := log.NewLogfmtParser(true, false)
		lb := log.NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
		lb.SetErr("LogfmtParserErr")

		filters := log.NewLabelFilterHints([]log.Stage{strict, stringFilter(labels.MatchEqual, logqlmodel.ErrorLabel, "")}).ForParser(strict)
		require.False(t, filters.ShouldContinueAfterParseError(lb))
	})

	t.Run("keeps the line when line_format, which can set __error__, runs before the __error__ filter", func(t *testing.T) {
		strict := log.NewLogfmtParser(true, false)
		lb := log.NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
		lb.SetErr("LogfmtParserErr")

		filters := log.NewLabelFilterHints([]log.Stage{strict, lineFormatter(t, "{{.foo}}"), stringFilter(labels.MatchEqual, logqlmodel.ErrorLabel, "")}).ForParser(strict)
		require.True(t, filters.ShouldContinueAfterParseError(lb))
	})
}

func stringFilter(t labels.MatchType, name, value string) log.LabelFilterer {
	return log.NewStringLabelFilter(labels.MustNewMatcher(t, name, value))
}

func labelsFormatter(t *testing.T, fmts ...log.LabelFmt) log.Stage {
	f, err := log.NewLabelsFormatter(fmts)
	require.NoError(t, err)
	return f
}

func lineFormatter(t *testing.T, tmpl string) log.Stage {
	f, err := log.NewFormatter(tmpl)
	require.NoError(t, err)
	return f
}

func jsonExpressionParser(t *testing.T, id, expr string) log.Stage {
	p, err := log.NewJSONExpressionParser([]log.LabelExtractionExpr{log.NewLabelExtractionExpr(id, expr)})
	require.NoError(t, err)
	return p
}

func logfmtExpressionParser(t *testing.T, id, expr string) log.Stage {
	p, err := log.NewLogfmtExpressionParser([]log.LabelExtractionExpr{log.NewLabelExtractionExpr(id, expr)}, false)
	require.NoError(t, err)
	return p
}
