package tenantfederation

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	tsdb_errors "github.com/prometheus/prometheus/tsdb/errors"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/tenant"
	"github.com/cortexproject/cortex/pkg/util/concurrency"
)

const (
	defaultTenantLabel         = "__tenant_id__"
	retainExistingPrefix       = "original_"
	originalDefaultTenantLabel = retainExistingPrefix + defaultTenantLabel
	maxConcurrency             = 16
)

// NewQueryable returns a queryable that iterates through all the tenant IDs
// that are part of the request and aggregates the results from each tenant's
// Querier by sending of subsequent requests.
// The result contains a label tenantLabelName to identify the tenant ID that
// it originally resulted from.
// If the label tenantLabelName is already existing, its value is overwritten
// by the tenant ID and the previous value is exposed through a new label
// prefixed with "original_". This behaviour is not implemented recursively
func NewQueryable(upstream storage.Queryable) storage.Queryable {
	return &mergeQueryable{
		upstream: upstream,
	}
}

type mergeQueryable struct {
	upstream storage.Queryable
}

// Querier returns a new mergeQuerier, which aggregates results from multiple
// tenants into a single result.
func (m *mergeQueryable) Querier(ctx context.Context, mint int64, maxt int64) (storage.Querier, error) {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}

	if len(tenantIDs) <= 1 {
		return m.upstream.Querier(ctx, mint, maxt)
	}

	var queriers = make([]storage.Querier, len(tenantIDs))
	for pos, tenantID := range tenantIDs {
		q, err := m.upstream.Querier(
			user.InjectOrgID(ctx, tenantID),
			mint,
			maxt,
		)
		if err != nil {
			return nil, err
		}
		queriers[pos] = q
	}

	return &mergeQuerier{
		ctx:       ctx,
		queriers:  queriers,
		tenantIDs: tenantIDs,
	}, nil
}

// mergeQuerier aggregates the results from underlying queriers and adds a
// label tenantLabelName to identify the tenant ID that the metric resulted
// from.
// If the label tenantLabelName is already existing, its value is
// overwritten by the tenant ID and the previous value is exposed through a new
// label prefixed with "original_". This behaviour is not implemented recursively
type mergeQuerier struct {
	ctx       context.Context
	queriers  []storage.Querier
	tenantIDs []string
}

// LabelValues returns all potential values for a label name.
// It is not safe to use the strings beyond the lifefime of the querier.
// For the label "tenantLabelName" it will return all the tenant IDs available.
// For the label "original_" + tenantLabelName it will return all the values
// of the underlying queriers for tenantLabelName.
func (m *mergeQuerier) LabelValues(name string, matchers ...*labels.Matcher) ([]string, storage.Warnings, error) {
	if name == defaultTenantLabel {
		return m.tenantIDs, nil, nil
	}

	// ensure the name of a retained tenant id label gets handled under the
	// original label name
	if name == originalDefaultTenantLabel {
		name = defaultTenantLabel
	}

	return m.mergeDistinctStringSlice(func(ctx context.Context, q storage.Querier) ([]string, storage.Warnings, error) {
		return q.LabelValues(name, matchers...)
	})
}

// LabelNames returns all the unique label names present in the underlying
// queriers. It also adds the defaultTenantLabel and if present in the original
// results the originalDefaultTenantLabel
func (m *mergeQuerier) LabelNames() ([]string, storage.Warnings, error) {
	labelNames, warnings, err := m.mergeDistinctStringSlice(func(ctx context.Context, q storage.Querier) ([]string, storage.Warnings, error) {
		return q.LabelNames()
	})
	if err != nil {
		return nil, nil, err
	}

	// check if the tenant label exists in the original result
	var tenantLabelExists bool
	labelPos := sort.SearchStrings(labelNames, defaultTenantLabel)
	if labelPos < len(labelNames) && labelNames[labelPos] == defaultTenantLabel {
		tenantLabelExists = true
	}

	labelToAdd := defaultTenantLabel

	// if defaultTenantLabel already exists, we need to add the
	// originalDefaultTenantLabel
	if tenantLabelExists {
		labelToAdd = originalDefaultTenantLabel
		labelPos = sort.SearchStrings(labelNames, labelToAdd)
	}

	// insert label at the correct position
	labelNames = append(labelNames, "")
	copy(labelNames[labelPos+1:], labelNames[labelPos:])
	labelNames[labelPos] = labelToAdd

	return labelNames, warnings, nil
}

type stringSliceFunc func(context.Context, storage.Querier) ([]string, storage.Warnings, error)

type stringSliceFuncJob struct {
	querier  storage.Querier
	tenantID string
	result   []string
	warnings storage.Warnings
}

// mergeDistinctStringSlice is aggregating results from stringSliceFunc calls
// on per querier in parallel. It removes duplicates and sorts the result. It
// doesn't require the output of the stringSliceFunc to be sorted, as results
// of LabelValues are not sorted.
func (m *mergeQuerier) mergeDistinctStringSlice(f stringSliceFunc) ([]string, storage.Warnings, error) {
	var jobs = make([]interface{}, len(m.tenantIDs))

	for pos := range m.tenantIDs {
		jobs[pos] = &stringSliceFuncJob{
			querier:  m.queriers[pos],
			tenantID: m.tenantIDs[pos],
		}
	}

	run := func(ctx context.Context, jobIntf interface{}) error {
		job, ok := jobIntf.(*stringSliceFuncJob)
		if !ok {
			return fmt.Errorf("unexpected type %T", jobIntf)
		}

		var err error
		job.result, job.warnings, err = f(ctx, job.querier)
		if err != nil {
			return errors.Wrapf(err, "error querying %s %s", rewriteLabelName(defaultTenantLabel), job.tenantID)
		}

		return nil
	}

	err := concurrency.ForEach(m.ctx, jobs, maxConcurrency, run)
	if err != nil {
		return nil, nil, err
	}

	// aggregate warnings and deduplicate string results
	var warnings storage.Warnings
	resultMap := make(map[string]struct{})
	for _, jobIntf := range jobs {
		job, ok := jobIntf.(*stringSliceFuncJob)
		if !ok {
			return nil, nil, fmt.Errorf("unexpected type %T", jobIntf)
		}

		for _, e := range job.result {
			resultMap[e] = struct{}{}
		}

		for _, w := range job.warnings {
			warnings = append(warnings, errors.Wrapf(w, "warning querying %s %s", rewriteLabelName(defaultTenantLabel), job.tenantID))
		}
	}

	var result = make([]string, 0, len(resultMap))
	for e := range resultMap {
		result = append(result, e)
	}
	sort.Strings(result)
	return result, warnings, nil
}

// Close releases the resources of the Querier.
func (m *mergeQuerier) Close() error {
	errs := tsdb_errors.NewMulti()
	for pos, tenantID := range m.tenantIDs {
		errs.Add(errors.Wrapf(m.queriers[pos].Close(), "failed to close querier for %s %s", rewriteLabelName(defaultTenantLabel), tenantID))
	}
	return errs.Err()
}

type selectJob struct {
	pos      int
	querier  storage.Querier
	tenantID string
}

// Select returns a set of series that matches the given label matchers. If the
// tenantLabelName is matched on it only considers those queriers matching. The
// forwarded labelSelector is not containing those that operate on
// tenantLabelName.
func (m *mergeQuerier) Select(sortSeries bool, hints *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	matchedTenants, filteredMatchers := filterValuesByMatchers(defaultTenantLabel, m.tenantIDs, matchers...)
	var jobs = make([]interface{}, len(matchedTenants))
	var seriesSets = make([]storage.SeriesSet, len(matchedTenants))
	var jobPos int
	for tenantPos := range m.tenantIDs {
		if _, matched := matchedTenants[m.tenantIDs[tenantPos]]; !matched {
			continue
		}
		jobs[jobPos] = &selectJob{
			pos:      jobPos,
			querier:  m.queriers[tenantPos],
			tenantID: m.tenantIDs[tenantPos],
		}
		jobPos++
	}

	run := func(ctx context.Context, jobIntf interface{}) error {
		job, ok := jobIntf.(*selectJob)
		if !ok {
			return fmt.Errorf("unexpected type %T", jobIntf)
		}
		seriesSets[job.pos] = &addLabelsSeriesSet{
			upstream: job.querier.Select(sortSeries, hints, filteredMatchers...),
			labels: labels.Labels{
				{
					Name:  defaultTenantLabel,
					Value: job.tenantID,
				},
			},
		}
		return nil
	}

	err := concurrency.ForEach(m.ctx, jobs, maxConcurrency, run)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}

	return storage.NewMergeSeriesSet(seriesSets, storage.ChainedSeriesMerge)
}

// filterValuesByMatchers applies matchers to inputed labelName and
// labelValues. A map of matched values is returned and also all label matchers
// not matching the labelName.
// In case a label matcher is set on a label conflicting with tenantLabelName,
// we need to rename this labelMatcher's name to its original name. This is
// used to as part of Select in the mergeQueryable, to ensure only relevant
// queries are considered and the forwarded matchers do not contain matchers on
// the tenantLabelName.
func filterValuesByMatchers(labelName string, labelValues []string, matchers ...*labels.Matcher) (matchedValues map[string]struct{}, unrelatedMatchers []*labels.Matcher) {
	// this contains the matchers which are not related to labelName
	unrelatedMatchers = make([]*labels.Matcher, 0, len(matchers))

	// build map of values to consider for the matchers
	matchedValues = make(map[string]struct{}, len(labelValues))
	for _, value := range labelValues {
		matchedValues[value] = struct{}{}
	}

	for _, m := range matchers {
		if m.Name != labelName {
			// check if has the retained label name
			if m.Name == originalDefaultTenantLabel {
				// rewrite label to the original name, by copying matcher and
				// replacing the label name
				rewrittenM := *m
				rewrittenM.Name = labelName
				unrelatedMatchers = append(unrelatedMatchers, &rewrittenM)
			} else {
				unrelatedMatchers = append(unrelatedMatchers, m)
			}
			continue
		}

		for value := range matchedValues {
			if !m.Matches(value) {
				delete(matchedValues, value)
			}
		}
	}

	return matchedValues, unrelatedMatchers
}

type addLabelsSeriesSet struct {
	upstream storage.SeriesSet
	labels   labels.Labels
}

func (m *addLabelsSeriesSet) Next() bool {
	return m.upstream.Next()
}

// At returns full series. Returned series should be iteratable even after Next is called.
func (m *addLabelsSeriesSet) At() storage.Series {
	return &addLabelsSeries{
		upstream: m.upstream.At(),
		labels:   m.labels,
	}
}

// The error that iteration as failed with.
// When an error occurs, set cannot continue to iterate.
func (m *addLabelsSeriesSet) Err() error {
	return errors.Wrapf(m.upstream.Err(), "error querying %s", labelsToString(m.labels))
}

// A collection of warnings for the whole set.
// Warnings could be return even iteration has not failed with error.
func (m *addLabelsSeriesSet) Warnings() storage.Warnings {
	upstream := m.upstream.Warnings()
	warnings := make(storage.Warnings, len(upstream))
	for pos := range upstream {
		warnings[pos] = errors.Wrapf(upstream[pos], "warning querying %s", labelsToString(m.labels))
	}
	return warnings
}

// rewrite label name to be more readable in error output
func rewriteLabelName(s string) string {
	return strings.TrimRight(strings.TrimLeft(s, "_"), "_")
}

// this outputs a more readable error format
func labelsToString(labels labels.Labels) string {
	parts := make([]string, len(labels))
	for pos, l := range labels {
		parts[pos] = rewriteLabelName(l.Name) + " " + l.Value
	}
	return strings.Join(parts, ", ")
}

type addLabelsSeries struct {
	upstream storage.Series
	labels   labels.Labels
}

// Labels returns the complete set of labels. For series it means all labels identifying the series.
func (a *addLabelsSeries) Labels() labels.Labels {
	return setLabelsRetainExisting(a.upstream.Labels(), a.labels...)
}

// Iterator returns a new, independent iterator of the data of the series.
func (a *addLabelsSeries) Iterator() chunkenc.Iterator {
	return a.upstream.Iterator()
}

// this sets a label and preserves an existing value a new label prefixed with
// original_. It doesn't do this recursively.
func setLabelsRetainExisting(src labels.Labels, additionalLabels ...labels.Label) labels.Labels {
	lb := labels.NewBuilder(src)

	for _, additionalL := range additionalLabels {
		if oldValue := src.Get(additionalL.Name); oldValue != "" {
			lb.Set(
				retainExistingPrefix+additionalL.Name,
				oldValue,
			)
		}
		lb.Set(additionalL.Name, additionalL.Value)
	}

	return lb.Labels()
}
