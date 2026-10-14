package dataobjread

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestNewProjectionPlan(t *testing.T) {
	tests := map[string]struct {
		query   string
		deletes []string

		// streamLabels are the label names the planned section's streams carry, which decide
		// whether a metadata matcher can be pushed into the read.
		streamLabels []string

		wantColumns  []logs.ColumnType
		wantMetadata []string

		// wantRowPredicates are the predicates pushed into the section read, which drop rows.
		wantRowPredicates []logs.RowPredicate

		// wantKeepLines and wantRejectLines are lines the pushed message predicate keeps and
		// drops. The closure is not comparable, so the test runs it on these.
		wantKeepLines, wantRejectLines []string

		// wantSectionPredicates are the matchers handed to the metastore, which drop whole
		// sections. Each is written as the matcher prints.
		wantSectionPredicates []string
	}{
		"count_over_time under a sum reads no message, only the grouping key and the error labels": {
			query:        `sum by (app) (count_over_time({app="x"}[1m]))`,
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata: []string{"__error__", "__error_details__", "app"},
		},
		"bytes_over_time reads the message to measure the line": {
			query:        `sum by (app) (bytes_over_time({app="x"}[1m]))`,
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMessage},
			wantMetadata: []string{"__error__", "__error_details__", "app"},
		},
		"bytes_rate reads the message to measure the line": {
			query:        `sum by (app) (bytes_rate({app="x"}[1m]))`,
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMessage},
			wantMetadata: []string{"__error__", "__error_details__", "app"},
		},
		"a sum with no grouping reads only the error label metadata, which can set the pipeline error": {
			query:        `sum (count_over_time({app="x"}[1m]))`,
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata: []string{"__error__", "__error_details__"},
		},
		"a bare range aggregation reads all metadata because its output keeps the whole label set": {
			query:       `count_over_time({app="x"}[1m])`,
			wantColumns: []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMetadata},
		},
		"a without grouping reads all metadata because it cannot list the surviving keys": {
			query:       `sum without (app) (count_over_time({app="x"}[1m]))`,
			wantColumns: []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMetadata},
		},
		"a non-sum vector aggregation reads all metadata, because only a sum may reduce labels at the source": {
			query:       `max by (app) (count_over_time({app="x"}[1m]))`,
			wantColumns: []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMetadata},
		},
		"an unwrap narrows the metadata to the grouping key and the unwrapped name": {
			query:        `sum by (app) (sum_over_time({app="x"} | unwrap duration [1m]))`,
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata: []string{"__error__", "__error_details__", "app", "duration"},
		},
		"an unwrap on a bare range aggregation reads all metadata because its output keeps the whole label set": {
			query:       `max_over_time({app="x"} | unwrap duration [1m]) by (pod)`,
			wantColumns: []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMetadata},
		},
		"a duration label filter narrows the metadata to the grouping key and the filtered name": {
			query:        `sum by (app) (count_over_time({app="x"} | latency > 1s [1m]))`,
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata: []string{"__error__", "__error_details__", "app", "latency"},
		},
		"a numeric label filter narrows the metadata to the grouping key and the filtered name": {
			query:        `sum by (app) (count_over_time({app="x"} | status > 400 [1m]))`,
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata: []string{"__error__", "__error_details__", "app", "status"},
		},
		"sum(max_over_time) reads all metadata, because the extractor keeps every label": {
			query:       `sum by (app) (max_over_time({app="x"} | unwrap duration | __error__="" [1m]))`,
			wantColumns: []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMetadata},
		},
		"sum(sum_over_time) narrows the metadata, because merging its series keeps the total": {
			query:        `sum by (app) (sum_over_time({app="x"} | unwrap duration | __error__="" [1m]))`,
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata: []string{"__error__", "__error_details__", "app", "duration"},
		},
		"a range aggregation with its own grouping reads all metadata, because that grouping wins": {
			query:       `sum by (app) (max_over_time({app="x"} | unwrap duration | __error__="" [1m]) by (pod))`,
			wantColumns: []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMetadata},
		},
		// A metadata key colliding with a stream label reaches the output renamed, while its
		// column keeps the original name. The plan cannot tell which of the two a query means,
		// so it reads both.
		"a grouping on a renamed key reads the column behind it as well": {
			query:        `sum by (level_extracted) (count_over_time({app="x"}[1m]))`,
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata: []string{"__error__", "__error_details__", "level", "level_extracted"},
		},
		"a matcher on a renamed key is not pushed as a predicate when the streams carry that label": {
			query:        `sum by (app) (count_over_time({app="x"} | level_extracted="error" [1m]))`,
			streamLabels: []string{"level"},
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata: []string{"__error__", "__error_details__", "app", "level", "level_extracted"},
		},
		"a matcher on a renamed key is pushed as a predicate when no stream carries that label": {
			query:                 `sum by (app) (count_over_time({app="x"} | level_extracted="error" [1m]))`,
			wantColumns:           []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata:          []string{"__error__", "__error_details__", "app", "level", "level_extracted"},
			wantRowPredicates:     []logs.RowPredicate{logs.MetadataMatcherRowPredicate{Key: "level_extracted", Value: "error"}},
			wantSectionPredicates: nil,
		},
		"two equalities on one name reach the metastore as one": {
			query:        `sum by (app) (count_over_time({app="x"} | level="error" | level="warn" [1m]))`,
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata: []string{"__error__", "__error_details__", "app", "level"},
			wantRowPredicates: []logs.RowPredicate{
				logs.MetadataMatcherRowPredicate{Key: "level", Value: "error"},
				logs.MetadataMatcherRowPredicate{Key: "level", Value: "warn"},
			},
			wantSectionPredicates: []string{`level="error"`},
		},
		"an unwrap's own filters name metadata the read must project": {
			query:        `sum by (app) (sum_over_time({app="x"} | unwrap duration | __error__="" | level="error" [1m]))`,
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata: []string{"__error__", "__error_details__", "app", "duration", "level"},
		},
		"a delete's label filter narrows the metadata to the grouping key and the filtered name": {
			query:        `sum by (app) (count_over_time({app="x"}[1m]))`,
			deletes:      []string{`{app="x"} | latency > 1s`},
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata: []string{"__error__", "__error_details__", "app", "latency"},
		},
		"a binary filter with a converting child narrows the metadata, and still pushes the string half": {
			query:                 `sum by (app) (count_over_time({app="x"} | level="error" and latency > 1s [1m]))`,
			wantColumns:           []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata:          []string{"__error__", "__error_details__", "app", "latency", "level"},
			wantRowPredicates:     []logs.RowPredicate{logs.MetadataMatcherRowPredicate{Key: "level", Value: "error"}},
			wantSectionPredicates: []string{`level="error"`},
		},
		"an equality against an empty value is pushed to the reader but withheld from the metastore": {
			query:        `sum by (app) (count_over_time({app="x"} | level="" [1m]))`,
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata: []string{"__error__", "__error_details__", "app", "level"},
			// The reader reduces an absent column against an empty value and keeps the row, but
			// a section holding no such column has no bloom entry, so the metastore would drop
			// exactly the sections the query must read.
			wantRowPredicates: []logs.RowPredicate{logs.MetadataMatcherRowPredicate{Key: "level", Value: ""}},
		},
		"a filter on the pipeline error label is projected but not pushed as a predicate": {
			query:        `sum by (app) (count_over_time({app="x"} | __error__="" [1m]))`,
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata: []string{"__error__", "__error_details__", "app"},
		},
		"a line filter reads the message and is pushed as a message predicate": {
			query:             `sum by (app) (count_over_time({app="x"} |= "boom" [1m]))`,
			wantColumns:       []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMessage},
			wantMetadata:      []string{"__error__", "__error_details__", "app"},
			wantRowPredicates: []logs.RowPredicate{logs.LogMessageFilterRowPredicate{}},
			wantKeepLines:     []string{"boom", "a boom b"},
			wantRejectLines:   []string{"", "ok"},
		},
		"chained line filters are pushed as one message predicate that requires all of them": {
			query:             `sum by (app) (count_over_time({app="x"} != "a" |~ "b.*" [1m]))`,
			wantColumns:       []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMessage},
			wantMetadata:      []string{"__error__", "__error_details__", "app"},
			wantRowPredicates: []logs.RowPredicate{logs.LogMessageFilterRowPredicate{}},
			wantKeepLines:     []string{"b", "xbx"},
			wantRejectLines:   []string{"ab", "c"},
		},
		"an or chain is pushed as one message predicate that keeps a line matching any operand": {
			query:             `sum by (app) (count_over_time({app="x"} |= "a" or "b" [1m]))`,
			wantColumns:       []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMessage},
			wantMetadata:      []string{"__error__", "__error_details__", "app"},
			wantRowPredicates: []logs.RowPredicate{logs.LogMessageFilterRowPredicate{}},
			wantKeepLines:     []string{"a", "b", "xay"},
			wantRejectLines:   []string{"c", ""},
		},
		"a line filter after a parser is pushed and the parser still reads all metadata": {
			query:             `sum by (app) (count_over_time({app="x"} | json |= "boom" [1m]))`,
			wantColumns:       []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMetadata, logs.ColumnTypeMessage},
			wantRowPredicates: []logs.RowPredicate{logs.LogMessageFilterRowPredicate{}},
			wantKeepLines:     []string{"boom"},
			wantRejectLines:   []string{"ok"},
		},
		"line filters on both sides of a parser are pushed as one message predicate": {
			query:             `sum by (app) (count_over_time({app="x"} |= "a" | json |= "b" [1m]))`,
			wantColumns:       []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMetadata, logs.ColumnTypeMessage},
			wantRowPredicates: []logs.RowPredicate{logs.LogMessageFilterRowPredicate{}},
			wantKeepLines:     []string{"ab", "xbxa"},
			wantRejectLines:   []string{"a", "b"},
		},
		"a line filter after a label filter is pushed after the metadata predicate": {
			query:        `sum by (app) (count_over_time({app="x"} | level="error" |= "boom" [1m]))`,
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMessage},
			wantMetadata: []string{"__error__", "__error_details__", "app", "level"},
			wantRowPredicates: []logs.RowPredicate{
				logs.MetadataMatcherRowPredicate{Key: "level", Value: "error"},
				logs.LogMessageFilterRowPredicate{},
			},
			wantSectionPredicates: []string{`level="error"`},
			wantKeepLines:         []string{"boom"},
			wantRejectLines:       []string{"ok"},
		},
		"a line filter before a label filter is still pushed after the metadata predicate": {
			query:        `sum by (app) (count_over_time({app="x"} |= "boom" | level="error" [1m]))`,
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMessage},
			wantMetadata: []string{"__error__", "__error_details__", "app", "level"},
			wantRowPredicates: []logs.RowPredicate{
				logs.MetadataMatcherRowPredicate{Key: "level", Value: "error"},
				logs.LogMessageFilterRowPredicate{},
			},
			wantSectionPredicates: []string{`level="error"`},
			wantKeepLines:         []string{"boom"},
			wantRejectLines:       []string{"ok"},
		},
		"a line filter before a line_format is pushed and one after it is not": {
			query:             `sum by (app) (count_over_time({app="x"} |= "a" | line_format "{{.app}}" |= "b" [1m]))`,
			wantColumns:       []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMetadata, logs.ColumnTypeMessage},
			wantRowPredicates: []logs.RowPredicate{logs.LogMessageFilterRowPredicate{}},
			wantKeepLines:     []string{"a", "ab"},
			wantRejectLines:   []string{"b"},
		},
		"a line filter after a line_format is not pushed": {
			query:       `sum by (app) (count_over_time({app="x"} | line_format "{{.app}}" |= "b" [1m]))`,
			wantColumns: []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMetadata, logs.ColumnTypeMessage},
		},
		"a line filter after a decolorize is not pushed": {
			query:       `sum by (app) (count_over_time({app="x"} | decolorize != "x" [1m]))`,
			wantColumns: []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMetadata, logs.ColumnTypeMessage},
		},
		"a line filter after an unpack is not pushed": {
			query:       `sum by (app) (count_over_time({app="x"} | unpack |= "x" [1m]))`,
			wantColumns: []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMetadata, logs.ColumnTypeMessage},
		},
		"a line filter that keeps every line reads the message and pushes nothing": {
			query:        `sum by (app) (count_over_time({app="x"} |= "" [1m]))`,
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMessage},
			wantMetadata: []string{"__error__", "__error_details__", "app"},
		},
		"a query's line filter is pushed and a delete's line filter is not": {
			query:             `sum by (app) (count_over_time({app="x"} |= "boom" [1m]))`,
			deletes:           []string{`{app="x"} |= "secret"`},
			wantColumns:       []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMessage},
			wantMetadata:      []string{"__error__", "__error_details__", "app"},
			wantRowPredicates: []logs.RowPredicate{logs.LogMessageFilterRowPredicate{}},
			wantKeepLines:     []string{"boom", "boom secret"},
			wantRejectLines:   []string{"secret"},
		},
		"a metadata equality is pushed as a matcher predicate and given to the metastore": {
			query:                 `sum by (app) (count_over_time({app="x"} | level="error" [1m]))`,
			wantColumns:           []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata:          []string{"__error__", "__error_details__", "app", "level"},
			wantRowPredicates:     []logs.RowPredicate{logs.MetadataMatcherRowPredicate{Key: "level", Value: "error"}},
			wantSectionPredicates: []string{`level="error"`},
		},
		"a metadata inequality is pushed as a filter predicate": {
			query:                 `sum by (app) (count_over_time({app="x"} | level!="error" [1m]))`,
			wantColumns:           []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata:          []string{"__error__", "__error_details__", "app", "level"},
			wantRowPredicates:     []logs.RowPredicate{logs.MetadataFilterRowPredicate{Key: "level"}},
			wantSectionPredicates: []string{`level!="error"`},
		},
		"both sides of an and are pushed": {
			query:        `sum by (app) (count_over_time({app="x"} | level="error" | pod="a" [1m]))`,
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata: []string{"__error__", "__error_details__", "app", "level", "pod"},
			wantRowPredicates: []logs.RowPredicate{
				logs.MetadataMatcherRowPredicate{Key: "level", Value: "error"},
				logs.MetadataMatcherRowPredicate{Key: "pod", Value: "a"},
			},
			wantSectionPredicates: []string{`level="error"`, `pod="a"`},
		},
		"neither side of an or is pushed because the other can satisfy the filter": {
			query:        `sum by (app) (count_over_time({app="x"} | level="error" or pod="a" [1m]))`,
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata: []string{"__error__", "__error_details__", "app", "level", "pod"},
		},
		"a matcher on a stream label is not pushed because the predicate cannot see that label": {
			query:        `sum by (app) (count_over_time({app="x"} | app="x" [1m]))`,
			streamLabels: []string{"app"},
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata: []string{"__error__", "__error_details__", "app"},
			// The metastore still receives it: it decides per section whether the name is a
			// stream label there.
			wantSectionPredicates: []string{`app="x"`},
		},
		"a line filter is still pushed when a matcher on a stream label is not": {
			query:                 `sum by (app) (count_over_time({app="x"} | app="x" |= "boom" [1m]))`,
			streamLabels:          []string{"app"},
			wantColumns:           []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMessage},
			wantMetadata:          []string{"__error__", "__error_details__", "app"},
			wantRowPredicates:     []logs.RowPredicate{logs.LogMessageFilterRowPredicate{}},
			wantKeepLines:         []string{"boom"},
			wantRejectLines:       []string{"ok"},
			wantSectionPredicates: []string{`app="x"`},
		},
		"a parser reads the message and all metadata and pushes nothing": {
			query:       `sum by (app) (count_over_time({app="x"} | json | level="error" [1m]))`,
			wantColumns: []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMetadata, logs.ColumnTypeMessage},
		},
		"a line_format reads the message and all metadata and pushes nothing": {
			query:       `sum by (app) (count_over_time({app="x"} | line_format "{{.app}}" [1m]))`,
			wantColumns: []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMetadata, logs.ColumnTypeMessage},
		},
		"a label_format reads the message and all metadata and pushes nothing": {
			query:       `sum by (app) (count_over_time({app="x"} | label_format pod=app [1m]))`,
			wantColumns: []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMetadata, logs.ColumnTypeMessage},
		},
		"a delete with only stream matchers widens nothing": {
			query:        `sum by (app) (count_over_time({app="x"}[1m]))`,
			deletes:      []string{`{app="x"}`},
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata: []string{"__error__", "__error_details__", "app"},
		},
		"a delete with a line filter widens the projection to the message": {
			query:        `sum by (app) (count_over_time({app="x"}[1m]))`,
			deletes:      []string{`{app="x"} |= "secret"`},
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMessage},
			wantMetadata: []string{"__error__", "__error_details__", "app"},
		},
		"a delete with a metadata filter widens the projection to that key": {
			query:        `sum by (app) (count_over_time({app="x"}[1m]))`,
			deletes:      []string{`{app="x"} | trace_id="abc"`},
			wantColumns:  []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp},
			wantMetadata: []string{"__error__", "__error_details__", "app", "trace_id"},
		},
		"a delete with a parser widens the projection to the message and all metadata": {
			query:       `sum by (app) (count_over_time({app="x"}[1m]))`,
			deletes:     []string{`{app="x"} | json | level="error"`},
			wantColumns: []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMetadata, logs.ColumnTypeMessage},
		},
		"a delete with a parser does not stop the query's own filter from being pushed": {
			query:                 `sum by (app) (count_over_time({app="x"} | level="error" [1m]))`,
			deletes:               []string{`{app="x"} | json | trace_id="abc"`},
			wantColumns:           []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp, logs.ColumnTypeMetadata, logs.ColumnTypeMessage},
			wantRowPredicates:     []logs.RowPredicate{logs.MetadataMatcherRowPredicate{Key: "level", Value: "error"}},
			wantSectionPredicates: []string{`level="error"`},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			expr, err := syntax.ParseSampleExpr(test.query)
			require.NoError(t, err)

			deletes := make([]syntax.LogSelectorExpr, 0, len(test.deletes))
			for _, selector := range test.deletes {
				parsed, err := syntax.ParseLogSelector(selector, true)
				require.NoError(t, err)
				deletes = append(deletes, parsed)
			}

			// The store builds the extractor first, and that rewrites the AST the plan walks.
			_, err = expr.Extractor()
			require.NoError(t, err)

			plan, err := NewProjectionPlan(expr, deletes)
			require.NoError(t, err)

			streamLabels := map[string]struct{}{}
			for _, name := range test.streamLabels {
				streamLabels[name] = struct{}{}
			}

			columns, metadata, predicates := plan.forStreams(streamLabels)
			require.Equal(t, test.wantColumns, columns)
			require.Equal(t, test.wantMetadata, metadata)
			requirePredicatesEqual(t, test.wantRowPredicates, predicates)

			if len(test.wantKeepLines)+len(test.wantRejectLines) > 0 {
				message, ok := predicates[len(predicates)-1].(logs.LogMessageFilterRowPredicate)
				require.True(t, ok, "want the message predicate last, got %T", predicates[len(predicates)-1])
				for _, line := range test.wantKeepLines {
					require.True(t, message.Keep([]byte(line)), "line %q should be kept", line)
				}
				for _, line := range test.wantRejectLines {
					require.False(t, message.Keep([]byte(line)), "line %q should be dropped", line)
				}
			}

			var sectionPredicates []string
			for _, matcher := range plan.sectionPredicates() {
				sectionPredicates = append(sectionPredicates, matcher.String())
			}
			require.Equal(t, test.wantSectionPredicates, sectionPredicates)
		})
	}
}

func TestMetadataPredicate(t *testing.T) {
	tests := map[string]struct {
		matcher    *labels.Matcher
		wantKeep   []string
		wantReject []string
	}{
		"an equality becomes a matcher predicate so the reader can skip pages by column statistics": {
			matcher: labels.MustNewMatcher(labels.MatchEqual, "level", "error"),
		},
		"an inequality becomes a filter predicate keeping the values that do not match": {
			matcher:    labels.MustNewMatcher(labels.MatchNotEqual, "level", "error"),
			wantKeep:   []string{"warn", ""},
			wantReject: []string{"error"},
		},
		"a regular expression becomes a filter predicate keeping the values that match": {
			matcher:    labels.MustNewMatcher(labels.MatchRegexp, "level", "err.*"),
			wantKeep:   []string{"error", "errno"},
			wantReject: []string{"warn"},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			predicate := metadataPredicate(test.matcher)

			if test.matcher.Type == labels.MatchEqual {
				require.Equal(t, logs.MetadataMatcherRowPredicate{Key: test.matcher.Name, Value: test.matcher.Value}, predicate)
				return
			}

			filter, ok := predicate.(logs.MetadataFilterRowPredicate)
			require.True(t, ok, "want a metadata filter, got %T", predicate)
			require.Equal(t, test.matcher.Name, filter.Key)
			for _, value := range test.wantKeep {
				require.True(t, filter.Keep(test.matcher.Name, value), "value %q should be kept", value)
			}
			for _, value := range test.wantReject {
				require.False(t, filter.Keep(test.matcher.Name, value), "value %q should be rejected", value)
			}
		})
	}
}

// requirePredicatesEqual compares row predicates, matching a MetadataFilterRowPredicate on its
// key alone because its Keep closure is not comparable.
func requirePredicatesEqual(t *testing.T, want, got []logs.RowPredicate) {
	t.Helper()
	require.Len(t, got, len(want))
	for i := range want {
		// Match a LogMessageFilterRowPredicate on its type alone, because its Keep closure is not
		// comparable.
		if _, ok := want[i].(logs.LogMessageFilterRowPredicate); ok {
			gotMessage, ok := got[i].(logs.LogMessageFilterRowPredicate)
			require.True(t, ok, "predicate %d: want a message filter, got %T", i, got[i])
			require.NotNil(t, gotMessage.Keep)
			continue
		}
		if wantFilter, ok := want[i].(logs.MetadataFilterRowPredicate); ok {
			gotFilter, ok := got[i].(logs.MetadataFilterRowPredicate)
			require.True(t, ok, "predicate %d: want a metadata filter, got %T", i, got[i])
			require.Equal(t, wantFilter.Key, gotFilter.Key)
			require.NotNil(t, gotFilter.Keep)
			continue
		}
		require.Equal(t, want[i], got[i])
	}
}
