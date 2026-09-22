package dataobjread

import (
	"maps"
	"slices"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	logqllog "github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

// ProjectionPlan says which columns of a logs section a query reads and which of its metadata
// filters can be pushed into the read.
//
// It is derived once per query and then applied per section, because whether a filter can be
// pushed depends on the labels of the streams that section holds.
type ProjectionPlan struct {
	// needMessage reports whether the log line must be read.
	needMessage bool

	// needAllMetadata reports whether every metadata column must be read.
	needAllMetadata bool

	// metadataNames name the metadata columns to read when needAllMetadata is false, sorted.
	metadataNames []string

	// pushdownCandidates are the query's label-filter matchers a read may push down. A name one
	// of them carries can turn out to be a stream label rather than metadata.
	pushdownCandidates []*labels.Matcher
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
		needMessage bool
		anyWithout  bool

		// canError records a failure that survives to the output labels.
		//
		// A failure keeps every label: LabelsBuilder.GroupedLabels returns the whole set when
		// the builder holds an error, so the error is not lost along with the labels the
		// grouping would drop. So such a query cannot have its metadata narrowed, whatever its
		// grouping says. See [canErrorInOrder] for what stops a failure surviving.
		canError bool

		// queryDerivesLabels and deleteDerivesLabels record a parser, formatter or other stage
		// that builds labels out of the line. They are tracked apart because they have
		// different consequences: either one forces every metadata column to be read, but only
		// the query's own blocks pushdown. After a parser, a filter's name may refer to a
		// parsed label rather than to metadata, and a predicate seeing only the metadata column
		// would drop rows the extractor keeps. A delete's pipeline cannot make the query's own
		// filters unsafe.
		queryDerivesLabels  bool
		deleteDerivesLabels bool

		// metadataNames are the metadata columns the extractor reads, through a filter or an
		// unwrap, or emits through a grouping.
		metadataNames      = map[string]struct{}{}
		pushdownCandidates []*labels.Matcher
	)

	addMetadataName := func(name string) {
		if name != "" && !isPipelineErrorLabel(name) {
			metadataNames[name] = struct{}{}
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
				needMessage = true
			}
			if typed.Left != nil {
				if canErrorInOrder(typed.Left.Left, typed.Left.Unwrap) {
					canError = true
				}
				if typed.Left.Unwrap != nil {
					addMetadataName(typed.Left.Unwrap.Identifier)
					for _, filter := range typed.Left.Unwrap.PostFilters {
						for _, name := range filter.RequiredLabelNames() {
							addMetadataName(name)
						}
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

	walkStages := func(sel syntax.LogSelectorExpr, collectCandidates bool, derivesLabels *bool) {
		pipeline, ok := sel.(*syntax.PipelineExpr)
		if !ok {
			return
		}
		for _, stage := range pipeline.MultiStages {
			switch typed := stage.(type) {
			case *syntax.LineFilterExpr:
				needMessage = true
			case *syntax.LabelFilterExpr:
				for _, name := range typed.LabelFilterer.RequiredLabelNames() {
					addMetadataName(name)
				}
				if collectCandidates {
					pushdownCandidates = append(pushdownCandidates, metadataMatcherCandidates(typed.LabelFilterer)...)
				}
			default:
				*derivesLabels = true
			}
		}
	}

	walkStages(selector, true, &queryDerivesLabels)
	for _, del := range deletes {
		walkStages(del, false, &deleteDerivesLabels)
		// A delete's pipeline runs against the same line, so its failures reach the same output
		// labels. It carries no unwrap.
		if canErrorInOrder(del, nil) {
			canError = true
		}
	}

	derivesLabels := queryDerivesLabels || deleteDerivesLabels
	plan := ProjectionPlan{
		needMessage: needMessage || derivesLabels,
		// The output carries names that cannot be listed ahead of time unless the top-level
		// aggregation reduces it to a known label set and no stage can fail.
		needAllMetadata: derivesLabels || anyWithout || canError || !reducesOutputLabels(expr),
	}
	if !plan.needAllMetadata {
		plan.metadataNames = slices.Sorted(maps.Keys(metadataNames))
	}
	if !queryDerivesLabels {
		plan.pushdownCandidates = pushdownCandidates
	}
	return plan, nil
}

// forStreams returns the columns to read and the predicates to push for a section whose streams
// carry the given label names.
func (p ProjectionPlan) forStreams(streamLabelNames map[string]struct{}) (columns []logs.ColumnType, metadataNames []string, predicates []logs.RowPredicate) {
	columns = make([]logs.ColumnType, 0, 4)
	columns = append(columns, logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp)
	if p.needAllMetadata {
		columns = append(columns, logs.ColumnTypeMetadata)
	}
	if p.needMessage {
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
		predicates = append(predicates, metadataPredicate(matcher))
	}

	return columns, metadataNames, predicates
}

// sectionPredicates returns the matchers to hand the metastore, which drops a section whose
// bloom filters show it holds none of them. The metastore uses the equalities and ignores the
// rest, and it decides per section whether a name is a stream label there.
func (p ProjectionPlan) sectionPredicates() []*labels.Matcher {
	out := make([]*labels.Matcher, 0, len(p.pushdownCandidates))

	for _, matcher := range p.pushdownCandidates {
		// An equality against an empty value is held back. LogQL keeps every row that has no value for
		// the key, but a section with no column for it has no bloom entry either, so the metastore
		// would drop exactly the sections the query must read.
		if matcher.Type == labels.MatchEqual && matcher.Value == "" {
			continue
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

// canErrorInOrder reports whether a failure of the pipeline's label filters, or of its unwrap,
// survives to the output labels.
//
// A parser or a formatter can fail too. Those are not considered here, because the caller reads
// every metadata column for them anyway.
//
// It follows the order the stages run in, which is not the order a plan walks the expression: the
// pipeline stages first, then the unwrap, then the unwrap's own filters. That order decides the
// answer, because a filter that keeps only the lines carrying no error clears every failure set
// before it and none set after it. So `| unwrap x | __error__=""` cannot fail, while
// `| __error__="" | unwrap x` still can.
func canErrorInOrder(sel syntax.LogSelectorExpr, unwrap *syntax.UnwrapExpr) bool {
	var canError bool
	apply := func(filter logqllog.LabelFilterer) {
		switch {
		// A label filter never adds, removes or replaces a real label, so the only label it can
		// modify is the pipeline error one. Its own hint therefore answers whether it can fail,
		// and a filter type added later cannot be forgotten here.
		case filter.Hints().CanModifyLabels:
			canError = true
		case dropsErroredLines(filter):
			canError = false
		}
	}

	if pipeline, ok := sel.(*syntax.PipelineExpr); ok {
		for _, stage := range pipeline.MultiStages {
			if filter, ok := stage.(*syntax.LabelFilterExpr); ok {
				apply(filter.LabelFilterer)
			}
		}
	}
	if unwrap == nil {
		return canError
	}

	// An unwrap converts a label to a number, which fails on a value that is not one. It runs
	// after the whole pipeline, so it fails whatever the stages before it dropped.
	canError = true
	for _, filter := range unwrap.PostFilters {
		apply(filter)
	}
	return canError
}

// dropsErroredLines reports whether a label filter keeps only the lines that carry no pipeline
// error.
//
// Only an equality against an empty value does. A filter selecting one error keeps the lines
// carrying it, and a negation keeps the lines carrying a different one, so neither clears a
// failure.
func dropsErroredLines(filter logqllog.LabelFilterer) bool {
	typed, ok := filter.(*logqllog.LineFilterLabelFilter)
	if !ok || typed.Matcher == nil {
		return false
	}
	return typed.Matcher.Name == logqlmodel.ErrorLabel &&
		typed.Matcher.Type == labels.MatchEqual &&
		typed.Matcher.Value == ""
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

// metadataMatcherCandidates returns the string matchers in a label filter that a metadata
// predicate can represent.
//
// It descends only into an and: either side of an or can be satisfied by the other, so pushing
// one side alone would drop rows the filter keeps.
//
// Filters that convert their value, meaning duration, bytes and numeric, are left out because
// the predicate compares the raw column string.
func metadataMatcherCandidates(filter logqllog.LabelFilterer) []*labels.Matcher {
	switch typed := filter.(type) {
	case *logqllog.LineFilterLabelFilter:
		return nonNilMatcher(typed.Matcher)
	case *logqllog.StringLabelFilter:
		return nonNilMatcher(typed.Matcher)
	case *logqllog.BinaryLabelFilter:
		if typed.And {
			return append(metadataMatcherCandidates(typed.Left), metadataMatcherCandidates(typed.Right)...)
		}
	}
	return nil
}

func nonNilMatcher(matcher *labels.Matcher) []*labels.Matcher {
	if matcher == nil || isPipelineErrorLabel(matcher.Name) {
		return nil
	}
	return []*labels.Matcher{matcher}
}

// isPipelineErrorLabel reports whether a name is one of the labels the pipeline sets when a
// stage fails.
//
// A filter on one of them reads the error the builder holds, never a stored column, so none is a
// metadata matcher in any direction: pushing one down would filter rows against a column no
// object has, and handing one to the metastore would drop every section.
func isPipelineErrorLabel(name string) bool {
	return name == logqlmodel.ErrorLabel ||
		name == logqlmodel.ErrorDetailsLabel ||
		name == logqlmodel.PreserveErrorLabel
}
