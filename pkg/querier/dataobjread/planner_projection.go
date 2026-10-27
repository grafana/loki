package dataobjread

import (
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	logqllog "github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

// ProjectionPlan says which columns of a logs section a query returns and which of its metadata
// filters and line filters can be pushed into the read. It also yields the unpushed query, through
// [ProjectionPlan.UnpushedExpr].
//
// It is derived once per query and then applied per section, because whether a filter can be
// pushed depends on the labels of the streams that section holds.
type ProjectionPlan struct {
	// needReturningLine reports whether the returned records should include the log line. It is
	// true when the unpushed query or a delete may read the line.
	needReturningLine bool

	// needAllMetadata reports whether every metadata column must be returned.
	needAllMetadata bool

	// metadataNames name the metadata columns to return when needAllMetadata is false, sorted.
	metadataNames []string

	// pushdownCandidates are the matchers of the label filters that can be safely pushed down.
	pushdownCandidates []*labels.Matcher

	// lineFilter is the push-down line filter, which runs on the stored line. It never comes from a
	// delete request, and it must be safe for concurrent use.
	lineFilter logqllog.Filterer

	// unpushedExpr is the query without the line filter stages the read applies or that keep every
	// line. It is nil when the query has none.
	unpushedExpr syntax.SampleExpr
}

// NewProjectionPlan derives the projection for expr, widened by the selectors of the query's
// delete requests.
//
// A delete request's pipeline runs over the same line and metadata as the query's, so a delete
// that filters on the line or on a metadata key needs those columns read even when the query
// alone would skip them. A delete carrying only stream matchers needs nothing: those are
// matched against the stream's labels, never against a column.
//
// A delete never contributes a pushed-down predicate. A pushed predicate drops rows, and a
// delete request only removes samples, so using one to drop rows early would discard rows the
// query must still count.
func NewProjectionPlan(expr syntax.SampleExpr, deletes []syntax.LogSelectorExpr) (ProjectionPlan, error) {
	var (
		needReturningLine bool
		anyWithout        bool

		// derivesLabels records a stage that is not a filter, in the query or in a delete. Such a
		// stage may read or change the line and any label. The read must then return the line and
		// every metadata column.
		derivesLabels bool

		// metadataNames are the metadata columns the extractor reads, through a filter or an
		// unwrap, or emits through a grouping.
		metadataNames = map[string]struct{}{}
	)

	addMetadataName := func(name string) {
		if name == "" || logqlmodel.IsPipelineErrorLabel(name) {
			return
		}
		metadataNames[name] = struct{}{}

		// A metadata key whose name collides with a stream label reaches the output renamed,
		// under [logqllog.DuplicateSuffix], while its column keeps the original name. A query
		// can only name the renamed form.
		//
		// Read both, because either may be the column: a key can also genuinely end with the
		// suffix. LogQL's own parser hints resolve the ambiguity the same way.
		//
		// A name of exactly the suffix trims to nothing, which names no column.
		if base := strings.TrimSuffix(name, logqllog.DuplicateSuffix); base != name && base != "" {
			metadataNames[base] = struct{}{}
		}
	}
	addGrouping := func(grouping *syntax.Grouping) {
		if grouping == nil {
			return
		}
		if grouping.Without {
			anyWithout = true
			return
		}
		for _, name := range grouping.Groups {
			addMetadataName(name)
		}
	}

	expr.Walk(func(e syntax.Expr) bool {
		switch typed := e.(type) {
		case *syntax.RangeAggregationExpr:
			// bytes_over_time and bytes_rate measure the line length.
			if typed.Operation == syntax.OpRangeTypeBytes || typed.Operation == syntax.OpRangeTypeBytesRate {
				needReturningLine = true
			}
			if typed.Left != nil && typed.Left.Unwrap != nil {
				addMetadataName(typed.Left.Unwrap.Identifier)
				for _, filter := range typed.Left.Unwrap.PostFilters {
					for _, name := range filter.RequiredLabelNames() {
						addMetadataName(name)
					}
				}
			}
			addGrouping(typed.Grouping)
		case *syntax.VectorAggregationExpr:
			addGrouping(typed.Grouping)
		}
		return true
	})

	selector, err := expr.Selector()
	if err != nil {
		return ProjectionPlan{}, err
	}

	walkStages := func(sel syntax.LogSelectorExpr) {
		pipeline, ok := sel.(*syntax.PipelineExpr)
		if !ok {
			return
		}
		for _, stage := range pipeline.MultiStages {
			switch typed := stage.(type) {
			case *syntax.LineFilterExpr:
				needReturningLine = true
			case *syntax.LabelFilterExpr:
				for _, name := range typed.RequiredLabelNames() {
					addMetadataName(name)
				}
			default:
				derivesLabels = true
			}
		}
	}

	// Push down the line filters that are safe to run on the stored line. Also collect the matchers
	// of the label filters that run before any stage that can change a label.
	var (
		lineFilter         logqllog.Filterer
		pushdownCandidates []*labels.Matcher
	)
	if pipeline, ok := selector.(*syntax.PipelineExpr); ok {
		lineFilter, err = pipeline.MultiStages.PushableLineFilters()
		if err != nil {
			return ProjectionPlan{}, err
		}

		// Unlike a line filter, a label filter stays in the unpushed query. The read decides per
		// section whether to push a matcher. It skips the matcher when the name is a stream label
		// of at least one stream in the section. Other streams in that section can hold the same
		// name as structured metadata. The predicate reads only the metadata column, so it cannot
		// serve both kinds of stream. One extractor serves every section, so it must apply every
		// label filter itself.
		var labelFilters []logqllog.LabelFilterer
		labelFilters, err = pipeline.MultiStages.PushableLabelFilters()
		if err != nil {
			return ProjectionPlan{}, err
		}
		for _, filter := range labelFilters {
			pushdownCandidates = append(pushdownCandidates, metadataMatcherCandidates(filter)...)
		}
	}

	// The read applies the pushable line filters, and a filter that keeps every line needs no check.
	// Leave them all out of the unpushed query, so they do not force the read to forward the line.
	unpushed, err := syntax.WithoutPushableLineFilters(expr)
	if err != nil {
		return ProjectionPlan{}, err
	}

	pipelineSelector := selector
	if unpushed != nil {
		if pipelineSelector, err = unpushed.Selector(); err != nil {
			return ProjectionPlan{}, err
		}
	}

	walkStages(pipelineSelector)
	for _, del := range deletes {
		walkStages(del)
	}

	plan := ProjectionPlan{
		lineFilter:         lineFilter,
		needReturningLine:  needReturningLine || derivesLabels,
		unpushedExpr:       unpushed,
		pushdownCandidates: pushdownCandidates,
		// The output carries names that cannot be listed ahead of time unless the top-level
		// aggregation reduces it to a known label set.
		needAllMetadata: derivesLabels || anyWithout || !reducesOutputLabels(expr),
	}
	if !plan.needAllMetadata {
		// A metadata key named after an error label sets the pipeline error, the same way a
		// failed stage does. So read those columns even when the query does not name them.
		metadataNames[logqlmodel.ErrorLabel] = struct{}{}
		metadataNames[logqlmodel.ErrorDetailsLabel] = struct{}{}
		plan.metadataNames = slices.Sorted(maps.Keys(metadataNames))
	}
	return plan, nil
}

// UnpushedExpr returns the query without the line filter stages that the read applies or that keep
// every line. It returns nil when the query has no such stage. The unpushed query is then the whole
// query.
func (p ProjectionPlan) UnpushedExpr() syntax.SampleExpr { return p.unpushedExpr }

// forStreams returns the columns to return and the predicates to push for a section whose streams
// carry the given label names. The message predicate of a line filter is the last predicate. The
// reader reads the message column itself, so columns lists it only when the query returns the
// line.
func (p ProjectionPlan) forStreams(streamLabelNames map[string]struct{}) (columns []logs.ColumnType, metadataNames []string, predicates []logs.RowPredicate) {
	columns = make([]logs.ColumnType, 0, 4)
	columns = append(columns, logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp)
	if p.needAllMetadata {
		columns = append(columns, logs.ColumnTypeMetadata)
	}
	if p.needReturningLine {
		columns = append(columns, logs.ColumnTypeMessage)
	}
	if !p.needAllMetadata {
		metadataNames = p.metadataNames
	}

	// A matcher on a name that is a stream label in any of those streams is not pushed. The
	// predicate reads only the metadata column, where that name holds no value, so it would drop
	// every row while the extractor, which sees the stream's labels too, keeps them.
	for _, matcher := range p.pushdownCandidates {
		if _, isStreamLabel := streamLabelNames[matcher.Name]; isStreamLabel {
			continue
		}

		// A renamed name may mean the column without the suffix, when this section's streams
		// carry the unsuffixed name. A predicate reads one column by name, so pushing it would
		// drop rows the extractor keeps.
		if base := strings.TrimSuffix(matcher.Name, logqllog.DuplicateSuffix); base != matcher.Name {
			if _, isStreamLabel := streamLabelNames[base]; isStreamLabel {
				continue
			}
		}

		predicates = append(predicates, metadataPredicate(matcher))
	}

	// The message predicate goes last because it is the most expensive. It then runs only on the
	// rows that the previous predicates did not filter out.
	if p.lineFilter != nil {
		predicates = append(predicates, logs.LogMessageFilterRowPredicate{
			Keep: p.lineFilter.Filter,
			KeepBatch: func(data []byte, offsets []int32, dstIndexes []int32) []int32 {
				return logqllog.FilterBatch(p.lineFilter, logqllog.LinesBatch{Data: data, Offsets: offsets}, dstIndexes)
			},
		})
	}

	return columns, metadataNames, predicates
}

// sectionPredicates returns the matchers to hand the metastore, which drops a section whose
// bloom filters show it holds none of them. The metastore uses the equalities and ignores the
// rest, and it decides per section whether a name is a stream label there.
func (p ProjectionPlan) sectionPredicates() []*labels.Matcher {
	var (
		out        = make([]*labels.Matcher, 0, len(p.pushdownCandidates))
		equalNames = make(map[string]struct{}, len(p.pushdownCandidates))
	)

	for _, matcher := range p.pushdownCandidates {
		// An equality against an empty value is held back. LogQL keeps every row that has no value for
		// the key, but a section with no column for it has no bloom entry either, so the metastore
		// would drop exactly the sections the query must read.
		if matcher.Type == labels.MatchEqual && matcher.Value == "" {
			continue
		}

		// A name ending in [logqllog.DuplicateSuffix] may be the column it spells, or the column
		// without the suffix. The blooms are keyed by the column's name, so the wrong guess drops
		// sections the query needs. Only the section's stream labels tell the two apart, and this
		// plan is built before any section is known.
		if strings.TrimSuffix(matcher.Name, logqllog.DuplicateSuffix) != matcher.Name {
			continue
		}

		// One equality per name. The metastore reads one bloom per name and rejects a request
		// that names a column twice, which would fail the whole query. `| level="a" | level="b"`
		// reaches here as two, so keep the first: the second only adds pruning.
		if matcher.Type == labels.MatchEqual {
			if _, seen := equalNames[matcher.Name]; seen {
				continue
			}
			equalNames[matcher.Name] = struct{}{}
		}
		out = append(out, matcher)
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// metadataPredicate turns a metadata matcher into a row predicate.
func metadataPredicate(matcher *labels.Matcher) logs.RowPredicate {
	if matcher.Type == labels.MatchEqual {
		// An equality lets the reader skip whole pages by their column statistics.
		return logs.MetadataMatcherRowPredicate{Key: matcher.Name, Value: matcher.Value}
	}

	// Any other matcher cannot skip pages, but it still makes the column primary, so the
	// message and the other secondary columns are read only for the rows that pass.
	return logs.MetadataFilterRowPredicate{
		Key:  matcher.Name,
		Keep: func(_, value string) bool { return matcher.Matches(value) },
	}
}

// reducesOutputLabels reports whether the top-level aggregation of expr reduces the output to a
// label set that can be listed ahead of time, so unreferenced metadata cannot surface.
func reducesOutputLabels(expr syntax.SampleExpr) bool {
	// Only an aggregation whose grouping the extractor injects does. Injection replaces the label
	// set with the grouping at extraction time, which is what stops an unread metadata column
	// from surfacing. Without injection the extractor keeps the labels the pipeline produced, so
	// dropping a column would merge the series that differ only in it.
	//
	// [syntax.CanInjectVectorGrouping] decides which operation pairs qualify. Asking it rather
	// than restating its rule keeps this from drifting when LogQL changes them.
	aggr, ok := expr.(*syntax.VectorAggregationExpr)
	if !ok {
		return false
	}

	// A range aggregation carrying its own grouping keeps it, and the extractor applies that one
	// instead of the injected one.
	rangeAggr, ok := aggr.Left.(*syntax.RangeAggregationExpr)
	if !ok || rangeAggr.Grouping != nil {
		return false
	}
	if !syntax.CanInjectVectorGrouping(aggr.Operation, rangeAggr.Operation) {
		return false
	}

	// A without grouping names the labels to drop rather than the ones to keep, so its output
	// cannot be listed ahead of time.
	return aggr.Grouping != nil && !aggr.Grouping.Without
}

// metadataMatcherCandidates returns the matchers that the read may push down for filter. A row
// that fails one of them also fails filter.
func metadataMatcherCandidates(filter logqllog.LabelFilterer) []*labels.Matcher {
	switch typed := filter.(type) {
	case *logqllog.LineFilterLabelFilter:
		return pushdownCandidate(typed.Matcher)
	case *logqllog.StringLabelFilter:
		return pushdownCandidate(typed.Matcher)
	case *logqllog.BinaryLabelFilter:
		// Push both sides of an and. Do not push one side of an or. A row can pass through the
		// other side. [metadataMatcherEqualityAlternation] handles an or of equalities on one
		// name.
		if typed.And {
			return append(metadataMatcherCandidates(typed.Left), metadataMatcherCandidates(typed.Right)...)
		}
		return metadataMatcherEqualityAlternation(typed)
	default:
		// Any other filter, such as a duration, bytes, numeric or ip filter, needs more than a raw
		// string comparison, which is all the predicate does.
		return nil
	}
}

// metadataMatcherEqualityAlternation returns one regular expression matcher for an or of
// equalities on one name. It returns nil when any leaf is not an equality that
// [pushdownCandidate] accepts.
func metadataMatcherEqualityAlternation(filter *logqllog.BinaryLabelFilter) []*labels.Matcher {
	var (
		name    string
		values  []string
		collect func(logqllog.LabelFilterer) bool
	)

	collect = func(f logqllog.LabelFilterer) bool {
		var matcher *labels.Matcher
		switch typed := f.(type) {
		case *logqllog.BinaryLabelFilter:
			return !typed.And && collect(typed.Left) && collect(typed.Right)
		case *logqllog.LineFilterLabelFilter:
			matcher = typed.Matcher
		case *logqllog.StringLabelFilter:
			matcher = typed.Matcher
		default:
			return false
		}

		if pushdownCandidate(matcher) == nil || matcher.Type != labels.MatchEqual {
			return false
		}
		if len(values) > 0 && matcher.Name != name {
			return false
		}
		name = matcher.Name
		values = append(values, regexp.QuoteMeta(matcher.Value))
		return true
	}
	if !collect(filter) {
		return nil
	}

	// A regular expression matcher matches the whole value, so it keeps the values that the
	// equalities keep. An absent key reads as the empty value in both forms.
	matcher, err := labels.NewMatcher(labels.MatchRegexp, name, strings.Join(values, "|"))
	if err != nil {
		return nil
	}
	return []*labels.Matcher{matcher}
}

// pushdownCandidate returns matcher as a pushdown candidate, or nil when it is not one.
func pushdownCandidate(matcher *labels.Matcher) []*labels.Matcher {
	// A filter on an error label reads the error the builder holds, not a column. A failed stage
	// or a stored metadata key can set that error, so a predicate on the column does not match the
	// filter. Pushing one down would drop rows the pipeline keeps, and handing one to the metastore
	// would drop sections the query needs.
	if matcher == nil || logqlmodel.IsPipelineErrorLabel(matcher.Name) {
		return nil
	}

	return []*labels.Matcher{matcher}
}
