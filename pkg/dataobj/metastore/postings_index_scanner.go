package metastore

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// postingsIndexScanner reads the postings sections of index objects.
type postingsIndexScanner struct {
	bucket objstore.BucketReader

	// indexMetadataCache serves the metadata region of the index objects. It may be nil.
	indexMetadataCache dataobj.MetadataCache
}

// openedIndexObject is an index object with its postings sections of one tenant opened.
type openedIndexObject struct {
	postingsSections []*postings.Section
}

// open opens the postings sections that the index object holds for the tenant. The error names the
// index object.
func (s *postingsIndexScanner) open(ctx context.Context, tenantID, indexPath string) (*openedIndexObject, error) {
	obj, err := dataobj.FromBucket(ctx, s.bucket, indexPath, indexPrefetchBytes, dataobj.WithMetadataCache(s.indexMetadataCache))
	if err != nil {
		return nil, fmt.Errorf("prepare obj %s: %w", indexPath, err)
	}

	sections, err := openPostingsSectionsConcurrently(ctx, tenantPostingsSections(obj, tenantID))
	if err != nil {
		return nil, fmt.Errorf("open the postings sections of index object %s: %w", indexPath, err)
	}
	return &openedIndexObject{postingsSections: sections}, nil
}

// tenantPostingsSections returns the postings sections that the object holds for the tenant.
func tenantPostingsSections(obj *dataobj.Object, tenantID string) []*dataobj.Section {
	var sections []*dataobj.Section
	for _, section := range obj.Sections() {
		if section.Tenant == tenantID && postings.CheckSection(section) {
			sections = append(sections, section)
		}
	}
	return sections
}

// openPostingsSectionsConcurrently opens the postings sections, a limited number at a time.
func openPostingsSectionsConcurrently(ctx context.Context, sections []*dataobj.Section) ([]*postings.Section, error) {
	opened := make([]*postings.Section, len(sections))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(maxConcurrentSectionOpens)
	for i, section := range sections {
		goRecovering(groupCtx, group, func() error {
			sec, err := postings.Open(groupCtx, section)
			if err != nil {
				return fmt.Errorf("opening postings section: %w", err)
			}
			opened[i] = sec
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return opened, nil
}

// scanSelectorMatches returns the sections of the index object whose streams match the stream
// matchers, using only the rows that overlap the shard bucket range. A nil range uses every row.
func (o *openedIndexObject) scanSelectorMatches(ctx context.Context, matchers []*labels.Matcher, bucketRange *postings.ShardBucketRange) (*indexSelectorMatches, error) {
	rowScan, err := o.scanSelectorRows(ctx, matchers, bucketRange)
	if err != nil {
		return nil, err
	}
	return rowScan.selectorMatches(), nil
}

// scanSelectorMatchesOfEachBucket returns the sections of the index object whose streams match the
// stream matchers, for each shard bucket of a layout with the given number of buckets, by bucket.
// It fails for an index object that does not record the shard bucket range of its rows.
func (o *openedIndexObject) scanSelectorMatchesOfEachBucket(ctx context.Context, matchers []*labels.Matcher, buckets uint32) ([]*indexSelectorMatches, error) {
	// The scan reads the rows of every bucket and cuts them by bucket afterwards. The rows of an
	// index object that does not record their bucket range cannot be cut.
	for _, section := range o.postingsSections {
		if err := postings.CheckShardBucketRanges(section); err != nil {
			return nil, err
		}
	}

	rowScan, err := o.scanSelectorRows(ctx, matchers, nil)
	if err != nil {
		return nil, err
	}
	rowScan.bucketRangesChecked = true
	return rowScan.selectorMatchesForEachBucket(buckets)
}

// scanSelectorRows reads the rows that match the stream matchers and overlap the bucket range. A
// nil range reads every row. The result records the range, and holds only the rows whose bucket
// span overlaps it.
func (o *openedIndexObject) scanSelectorRows(ctx context.Context, matchers []*labels.Matcher, bucketRange *postings.ShardBucketRange) (*selectorRowScan, error) {
	selector := newStreamSelector(matchers, nil, time.Time{}, time.Time{}, bucketRange)
	if err := selector.open(ctx, o.postingsSections, maxConcurrentSectionOpens); err != nil {
		return nil, fmt.Errorf("opening postings readers: %w", err)
	}
	defer func() { _ = selector.close() }()
	defer selector.reportStats(ctx)

	return selector.scanRows(ctx)
}

// scanPredicateAdmissions reads the answer of the index object to each equality predicate. It
// ignores the predicates of another type.
func (o *openedIndexObject) scanPredicateAdmissions(ctx context.Context, predicates []*labels.Matcher) (predicateAdmissions, error) {
	selector := newStreamSelector(nil, predicates, time.Time{}, time.Time{}, nil)
	if err := selector.open(ctx, o.postingsSections, maxConcurrentSectionOpens); err != nil {
		return nil, fmt.Errorf("opening postings readers: %w", err)
	}
	defer func() { _ = selector.close() }()
	defer selector.reportStats(ctx)

	return selector.scanPredicateAdmissions(ctx)
}
