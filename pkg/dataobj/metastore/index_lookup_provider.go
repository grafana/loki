package metastore

import (
	"context"
	"fmt"
	"iter"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// indexLookupProvider looks up a stream selector in the index objects of one request.
type indexLookupProvider interface {
	// lookupIndexObjects yields the result for each index object as soon as it is ready, in no
	// fixed order.
	//
	// The sequence yields at most one error, which ends it. Breaking out of the loop cancels the
	// lookups that run and waits for them.
	lookupIndexObjects(ctx context.Context, req indexLookupRequest, indexes []IndexEntry) iter.Seq2[indexLookupResult, error]
}

// indexLookupRequest is what a lookup needs to know about a request. The time range of the request
// is not part of it, because a lookup does not depend on it.
//
// The cache keys are built from these fields. A new field that changes the result of a lookup needs
// a place in the keys.
type indexLookupRequest struct {
	tenantID       string
	streamMatchers []*labels.Matcher

	// equalityPredicates are the predicates that can prune a section, in the order of the request.
	// A predicate of another type prunes nothing.
	equalityPredicates []*labels.Matcher

	streamHashRange *streams.HashRange
}

// newIndexLookupRequest returns the lookup request of a sections request. It fails for two equality
// predicates on the same label.
func newIndexLookupRequest(tenantID string, req SectionsRequest) (indexLookupRequest, error) {
	lookupReq := indexLookupRequest{
		tenantID:        tenantID,
		streamMatchers:  req.Matchers,
		streamHashRange: req.StreamHashRange,
	}

	// The postings scanner maps a bloom row to a predicate by label name, so it supports one equality
	// predicate for each name.
	seen := map[string]struct{}{}
	for _, predicate := range req.Predicates {
		if predicate == nil || predicate.Type != labels.MatchEqual {
			continue
		}
		if _, duplicate := seen[predicate.Name]; duplicate {
			return indexLookupRequest{}, fmt.Errorf("duplicate equality predicate on the label %q", predicate.Name)
		}
		seen[predicate.Name] = struct{}{}
		lookupReq.equalityPredicates = append(lookupReq.equalityPredicates, predicate)
	}
	return lookupReq, nil
}

// shardBucketRangeFor returns the shard buckets of the layout that hold the hash range. It returns
// nil when there is no hash range, or when the range covers every bucket.
func shardBucketRangeFor(layout streams.ShardLayout, hashRange *streams.HashRange) *postings.ShardBucketRange {
	if hashRange == nil {
		return nil
	}

	from, to := layout.BucketRange(*hashRange)

	// A range that covers every bucket prunes nothing, so the lookup reads without a range, and the
	// cache shares one key between all such ranges.
	if layout.CoversAllShardBuckets(from, to) {
		return nil
	}
	return &postings.ShardBucketRange{From: from, To: to}
}

// objectStoreIndexLookupProvider looks up a stream selector by reading the index objects from
// object storage.
type objectStoreIndexLookupProvider struct {
	scanner        *postingsIndexScanner
	layoutResolver *shardLayoutResolver
	logger         log.Logger

	// parallelism is the number of index objects that the provider reads at the same time.
	parallelism int
}

func (p *objectStoreIndexLookupProvider) lookupIndexObjects(ctx context.Context, req indexLookupRequest, indexes []IndexEntry) iter.Seq2[indexLookupResult, error] {
	return lookupIndexObjectsConcurrently(ctx, p.logger, p.parallelism, indexes, func(ctx context.Context, index IndexEntry) (indexLookupResult, error) {
		return p.lookupIndexObject(ctx, req, index)
	})
}

func (p *objectStoreIndexLookupProvider) lookupIndexObject(ctx context.Context, req indexLookupRequest, index IndexEntry) (indexLookupResult, error) {
	// The index object only needs its shard layout to turn the hash range of the request into a
	// shard bucket range.
	var bucketRange *postings.ShardBucketRange
	if req.streamHashRange != nil {
		layout, err := p.layoutResolver.resolveShardLayout(ctx, req.tenantID, index.Path)
		if err != nil {
			return indexLookupResult{}, fmt.Errorf("reading the shard layout of index object %q: %w", index.Path, err)
		}
		bucketRange = shardBucketRangeFor(layout, req.streamHashRange)
	}

	opened, err := p.scanner.open(ctx, req.tenantID, index.Path)
	if err != nil {
		return indexLookupResult{}, err
	}

	// The two reads are independent, so they can overlap. Nothing recovers a panic of a goroutine
	// that the group starts, so each goroutine recovers its own.
	var (
		matches    *indexSelectorMatches
		admissions predicateAdmissions
	)
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() (err error) {
		defer recoverPanicAsError(groupCtx, p.logger, index.Path, &err)

		matches, err = opened.scanSelectorMatches(groupCtx, req.streamMatchers, bucketRange)
		if err != nil {
			return fmt.Errorf("matching the stream selector in index object %q: %w", index.Path, err)
		}
		return nil
	})
	if len(req.equalityPredicates) > 0 {
		group.Go(func() (err error) {
			defer recoverPanicAsError(groupCtx, p.logger, index.Path, &err)

			admissions, err = opened.scanPredicateAdmissions(groupCtx, req.equalityPredicates)
			if err != nil {
				return fmt.Errorf("reading the predicate admissions of index object %q: %w", index.Path, err)
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return indexLookupResult{}, err
	}

	return indexLookupResult{index: index, selectorMatches: matches, predicateAdmissions: admissions}, nil
}

type indexLookupOutcome struct {
	result indexLookupResult
	err    error
}

// lookupIndexObjectsConcurrently calls lookupOne for each index object, at most parallelism at a
// time, and yields the results in the order they finish. A panic in lookupOne ends the sequence
// with an error.
//
// The sequence yields at most one error, which ends it. Breaking out of the loop cancels the calls
// that run and waits for them.
func lookupIndexObjectsConcurrently(
	ctx context.Context,
	logger log.Logger,
	parallelism int,
	indexes []IndexEntry,
	lookupOne func(ctx context.Context, index IndexEntry) (indexLookupResult, error),
) iter.Seq2[indexLookupResult, error] {
	return func(yield func(indexLookupResult, error) bool) {
		ctx, cancel := context.WithCancel(ctx)

		// The channel holds one outcome for each index object, so that no worker waits for the
		// consumer. A worker that ends after the consumer left still sends its outcome.
		outcomes := make(chan indexLookupOutcome, len(indexes))

		go func() {
			defer close(outcomes)

			// A limit of zero would block every call of Go forever.
			var group errgroup.Group
			group.SetLimit(max(parallelism, 1))
			for _, index := range indexes {
				group.Go(func() error {
					outcomes <- callRecoveringPanics(ctx, logger, index, lookupOne)
					return nil
				})
			}
			_ = group.Wait()
		}()

		// On return, cancel the workers and drop the outcomes that remain. The channel closes after
		// the last worker ends, so this loop also waits for the workers.
		defer func() {
			cancel()
			for range outcomes {
			}
		}()

		for outcome := range outcomes {
			if outcome.err != nil {
				yield(indexLookupResult{}, outcome.err)
				return
			}
			if !yield(outcome.result, nil) {
				return
			}
		}
	}
}

// callRecoveringPanics calls lookupOne and returns its outcome. A panic becomes an error outcome.
func callRecoveringPanics(
	ctx context.Context,
	logger log.Logger,
	index IndexEntry,
	lookupOne func(ctx context.Context, index IndexEntry) (indexLookupResult, error),
) indexLookupOutcome {
	result, err := func() (result indexLookupResult, err error) {
		defer recoverPanicAsError(ctx, logger, index.Path, &err)
		return lookupOne(ctx, index)
	}()
	return indexLookupOutcome{result: result, err: err}
}
