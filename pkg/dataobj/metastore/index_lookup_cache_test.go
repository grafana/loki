package metastore

import (
	"context"
	"fmt"
	"io"
	"iter"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/dataobj/metadatacache"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

func TestIndexLookupCache(t *testing.T) {
	ctx := user.InjectOrgID(t.Context(), tenantID)

	t.Run("resolves the same sections as a metastore without the cache", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		withoutCache := fixture.newMetastore(nil)
		withCache := fixture.newMetastore(cache.NewMockCache())

		var requestsWithSections int
		requests := fixture.requests()
		for _, request := range requests {
			want := resolvedSections(ctx, t, withoutCache, request)
			if len(want) > 0 {
				requestsWithSections++
			}

			require.Equal(t, want, resolvedSections(ctx, t, withCache, request), "the first request: %s", describeRequest(request))
			require.Equal(t, want, resolvedSections(ctx, t, withCache, request), "the repeated request: %s", describeRequest(request))
		}
		require.Greater(t, requestsWithSections, len(requests)/3, "the fixture resolves sections for too few requests to prove anything")
	})

	t.Run("reads no index object when only the time range changes", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		m := fixture.newMetastore(cache.NewMockCache())
		withoutCache := fixture.newMetastore(nil)

		request := fixture.wholeRangeRequest(`app=a`)
		first := resolvedSections(ctx, t, m, request)
		require.NotEmpty(t, first)
		require.Positive(t, fixture.bucket.rangeReads.Load())

		for _, window := range fixture.windows() {
			shifted := request
			shifted.Start, shifted.End = window.start, window.end
			want := resolvedSections(ctx, t, withoutCache, shifted)

			reads := fixture.bucket.rangeReads.Load()
			require.Equal(t, want, resolvedSections(ctx, t, m, shifted), "window %s", window.name)
			require.Equal(t, reads, fixture.bucket.rangeReads.Load(), "window %s read an index object", window.name)
		}
	})

	t.Run("reads the index objects once for the shards of a query", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		m := fixture.newMetastore(cache.NewMockCache())
		withoutCache := fixture.newMetastore(nil)
		request := fixture.wholeRangeRequest(`app=a env=prod`)
		layout := streams.DefaultShardLayout()

		var requestsWithSections int
		for bucket := range layout.Buckets() {
			shard := request
			shard.StreamHashRange = hashRangeOfBuckets(layout, &postings.ShardBucketRange{From: bucket, To: bucket})

			reads := fixture.bucket.rangeReads.Load()
			got := resolvedSections(ctx, t, m, shard)
			if bucket > 0 {
				require.Equal(t, reads, fixture.bucket.rangeReads.Load(), "bucket %d read an index object", bucket)
			}

			want := resolvedSections(ctx, t, withoutCache, shard)
			require.Equal(t, want, got, "bucket %d", bucket)
			if len(want) > 0 {
				requestsWithSections++
			}
		}
		require.Positive(t, requestsWithSections)
	})

	t.Run("shares the sections of a selector between requests with different predicates", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		backend := cache.NewMockCache()
		m := fixture.newMetastore(backend)

		first := fixture.wholeRangeRequest(`app=a`)
		first.Predicates = parseMatchersText(`detected_level=error`)
		_ = resolvedSections(ctx, t, m, first)
		selectorEntries := cacheKeysWithPrefix(backend, indexLookupCacheKeyPrefix+"m/")
		admissionEntries := cacheKeysWithPrefix(backend, indexLookupCacheKeyPrefix+"a/")
		require.NotEmpty(t, selectorEntries)
		require.NotEmpty(t, admissionEntries)

		second := fixture.wholeRangeRequest(`app=a`)
		second.Predicates = parseMatchersText(`trace_id=t-obj-1-3`)
		_ = resolvedSections(ctx, t, m, second)

		require.Equal(t, selectorEntries, cacheKeysWithPrefix(backend, indexLookupCacheKeyPrefix+"m/"), "the second request stored selector matches again")
		require.Greater(t, len(cacheKeysWithPrefix(backend, indexLookupCacheKeyPrefix+"a/")), len(admissionEntries))
	})

	t.Run("resolves the request when the backend fails", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		backend := cache.NewMockCache()
		backend.SetErr(fmt.Errorf("store failed"), fmt.Errorf("fetch failed"))
		m := fixture.newMetastore(backend)
		withoutCache := fixture.newMetastore(nil)

		request := fixture.wholeRangeRequest(`app=a env=prod`)
		request.Predicates = parseMatchersText(`detected_level=error`)

		want := resolvedSections(ctx, t, withoutCache, request)
		require.NotEmpty(t, want)
		require.Equal(t, want, resolvedSections(ctx, t, m, request))
	})

	t.Run("reads again an entry that it cannot use", func(t *testing.T) {
		for name, corrupt := range map[string]func(t *testing.T, backend cache.MockCache){
			"an entry that holds garbage": func(_ *testing.T, backend cache.MockCache) {
				for key := range backend.GetInternal() {
					backend.GetInternal()[key] = []byte("not a protobuf message")
				}
			},
			"an entry stored under the key of another entry": func(_ *testing.T, backend cache.MockCache) {
				keys := slices.Sorted(maps.Keys(backend.GetInternal()))
				values := make([][]byte, len(keys))
				for i, key := range keys {
					values[i] = backend.GetInternal()[key]
				}
				for i, key := range keys {
					backend.GetInternal()[key] = values[(i+1)%len(keys)]
				}
			},
			"selector matches with another version": func(t *testing.T, backend cache.MockCache) {
				changeVersionOfEntries(t, backend, "m/")
			},
			"a predicate admission with another version": func(t *testing.T, backend cache.MockCache) {
				changeVersionOfEntries(t, backend, "a/")
			},
		} {
			t.Run(name, func(t *testing.T) {
				fixture := newLookupCacheFixture(t)
				backend := cache.NewMockCache()
				m := fixture.newMetastore(backend)
				request := fixture.wholeRangeRequest(`app=a env=prod`)
				request.Predicates = parseMatchersText(`detected_level=error`)

				want := resolvedSections(ctx, t, m, request)
				require.NotEmpty(t, want)
				require.Greater(t, len(backend.GetInternal()), 1)
				corrupt(t, backend)

				fixture.bucket.rangeReads.Store(0)
				require.Equal(t, want, resolvedSections(ctx, t, m, request))
				require.Positive(t, fixture.bucket.rangeReads.Load(), "the metastore did not read the index objects again")
			})
		}
	})

	t.Run("reuses the entries of a request whose matchers come in another order", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		m := fixture.newMetastore(cache.NewMockCache())

		want := resolvedSections(ctx, t, m, fixture.wholeRangeRequest(`app=a env=prod`))
		require.NotEmpty(t, want)
		fixture.bucket.rangeReads.Store(0)

		require.Equal(t, want, resolvedSections(ctx, t, m, fixture.wholeRangeRequest(`env=prod app=a`)))
		require.Zero(t, fixture.bucket.rangeReads.Load())
	})

	t.Run("keeps apart the entries of selectors that differ only in an operator", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		m := fixture.newMetastore(cache.NewMockCache())
		withoutCache := fixture.newMetastore(nil)

		for _, selector := range []string{`app=a env=prod`, `app=a env!=prod`, `app=a env=~prod`, `app=a env!~prod`} {
			request := fixture.wholeRangeRequest(selector)
			require.Equal(t, resolvedSections(ctx, t, withoutCache, request), resolvedSections(ctx, t, m, request), selector)
		}
	})

	t.Run("keeps apart the admissions of predicates that differ in the name or in the value", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		m := fixture.newMetastore(cache.NewMockCache())
		withoutCache := fixture.newMetastore(nil)

		var resolvedAny bool
		for _, predicates := range []string{`detected_level=error`, `detected_level=error trace_id=t-obj-1-3`, `detected_level=error`, `trace_id=t-obj-1-3`, `detected_level=info`, `trace_id=error`} {
			request := fixture.wholeRangeRequest(`app=a`)
			request.Predicates = parseMatchersText(predicates)

			want := resolvedSections(ctx, t, withoutCache, request)
			resolvedAny = resolvedAny || len(want) > 0
			require.Equal(t, want, resolvedSections(ctx, t, m, request), predicates)
		}
		require.True(t, resolvedAny)
	})

	t.Run("fails a request with two equality predicates on one label when the backend has both admissions", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		m := fixture.newMetastore(cache.NewMockCache())

		for _, predicates := range []string{`detected_level=error`, `detected_level=info`} {
			request := fixture.wholeRangeRequest(`app=a`)
			request.Predicates = parseMatchersText(predicates)
			_ = resolvedSections(ctx, t, m, request)
		}

		request := fixture.wholeRangeRequest(`app=a`)
		request.Predicates = parseMatchersText(`detected_level=error detected_level=info`)
		_, err := m.Sections(ctx, request)
		require.ErrorContains(t, err, `duplicate equality predicate on the label "detected_level"`)
	})

	t.Run("reads no index object when a request that matches nothing repeats", func(t *testing.T) {
		for name, newRequest := range map[string]func(f *lookupCacheFixture) SectionsRequest{
			"a selector that matches no stream": func(f *lookupCacheFixture) SectionsRequest {
				return f.wholeRangeRequest(`app=nope`)
			},
			"a predicate on a column without bloom filters": func(f *lookupCacheFixture) SectionsRequest {
				request := f.wholeRangeRequest(`app=a`)
				request.Predicates = parseMatchersText(`missing_column=nope`)
				return request
			},
		} {
			t.Run(name, func(t *testing.T) {
				fixture := newLookupCacheFixture(t)
				m := fixture.newMetastore(cache.NewMockCache())
				request := newRequest(fixture)

				require.Empty(t, resolvedSections(ctx, t, m, request))
				require.Positive(t, fixture.bucket.rangeReads.Load())
				fixture.bucket.rangeReads.Store(0)

				require.Empty(t, resolvedSections(ctx, t, m, request))
				require.Zero(t, fixture.bucket.rangeReads.Load())
			})
		}
	})

	t.Run("stores no entry over the size limit and still resolves the request", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		backend := cache.NewMockCache()
		m := NewObjectMetastore(fixture.bucket, Config{}, log.NewNopLogger(), prometheus.NewRegistry(), WithIndexLookupCache(backend, 1))
		withoutCache := fixture.newMetastore(nil)

		request := fixture.wholeRangeRequest(`app=a`)
		request.Predicates = parseMatchersText(`detected_level=error`)
		want := resolvedSections(ctx, t, withoutCache, request)
		require.NotEmpty(t, want)

		require.Equal(t, want, resolvedSections(ctx, t, m, request))
		require.Empty(t, backend.GetInternal())
	})

	t.Run("keeps apart the entries of two tenants that share an index object", func(t *testing.T) {
		for name, predicates := range map[string]string{"selector matches": "", "predicate admissions": "app=foo"} {
			t.Run(name, func(t *testing.T) {
				bucket := objstore.NewInMemBucket()
				indexBucket := NewIndexBucket(bucket)

				observationsOf := func(objectPath string) []postings.LabelObservation {
					return []postings.LabelObservation{
						{ObjectPath: objectPath, SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 1, Time: postings.At(now)},
						{ObjectPath: objectPath, SectionIndex: 0, ColumnName: "app", LabelValue: "foo", StreamID: 1, Time: postings.At(now)},
					}
				}
				obj, closer := buildSharedIndexObject(t, map[string][]postings.LabelObservation{
					"tenant-a": observationsOf("obj-a"),
					"tenant-b": observationsOf("obj-b"),
				})
				t.Cleanup(closer)
				path := writeIndexObject(ctx, t, indexBucket, obj)
				registerIndexObject(ctx, t, indexBucket, "tenant-a", path)
				registerIndexObject(ctx, t, indexBucket, "tenant-b", path)

				m := NewObjectMetastore(bucket, Config{}, log.NewNopLogger(), prometheus.NewRegistry(), WithIndexLookupCache(cache.NewMockCache(), 0))
				request := SectionsRequest{Start: now.Add(-time.Hour), End: now.Add(time.Hour), Matchers: parseMatchersText(`env=prod`), Predicates: parseMatchersText(predicates)}

				for _, tenant := range []string{"tenant-a", "tenant-b", "tenant-a", "tenant-b"} {
					response, err := m.Sections(user.InjectOrgID(t.Context(), tenant), request)
					require.NoError(t, err)
					require.Equal(t, []string{"obj-" + strings.TrimPrefix(tenant, "tenant-")}, objectPathsOf(response.Sections), tenant)
				}
			})
		}
	})

	t.Run("fills the entry of every bucket only for a layout of at most 256 buckets", func(t *testing.T) {
		for buckets, wantEntries := range map[uint32]int{256: 256, 512: 1} {
			t.Run(fmt.Sprintf("a layout of %d buckets stores %d entries for one request", buckets, wantEntries), func(t *testing.T) {
				bucket := objstore.NewInMemBucket()
				layout := streams.MustShardLayoutFromBuckets(buckets)
				createIndexObject(ctx, t, bucket, []postings.LabelObservation{
					{ObjectPath: "obj", SectionIndex: 0, ColumnName: "app", LabelValue: "foo", StreamID: 1, Time: postings.At(now), ShardBucket: 7},
				}, streams.NewLayout(layout, nil))
				backend := cache.NewMockCache()
				m := NewObjectMetastore(bucket, Config{}, log.NewNopLogger(), prometheus.NewRegistry(), WithIndexLookupCache(backend, 0))

				response, err := m.Sections(ctx, SectionsRequest{
					Start:           now.Add(-time.Hour),
					End:             now.Add(time.Hour),
					Matchers:        parseMatchersText(`app=foo`),
					StreamHashRange: hashRangeOfBuckets(layout, &postings.ShardBucketRange{From: 7, To: 7}),
				})
				require.NoError(t, err)
				require.Equal(t, []string{"obj"}, objectPathsOf(response.Sections))

				require.Len(t, cacheKeysWithPrefix(backend, indexLookupCacheKeyPrefix+"m/"), wantEntries)
			})
		}
	})

	t.Run("reads the index object without the cache when the fill takes too long", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		backend := cache.NewMockCache()
		bucket := &deadlineBlockingBucket{Bucket: fixture.bucket}
		m := NewObjectMetastore(bucket, Config{}, log.NewNopLogger(), prometheus.NewRegistry(), WithIndexLookupCache(backend, 0))
		m.indexLookupProvider.(*indexLookupCache).fillTimeout = 100 * time.Millisecond
		withoutCache := fixture.newMetastore(nil)

		request := fixture.wholeRangeRequest(`app=a env=prod`)
		request.Predicates = parseMatchersText(`detected_level=error`)

		require.Equal(t, resolvedSections(ctx, t, withoutCache, request), resolvedSections(ctx, t, m, request))
		require.Positive(t, bucket.blockedReads.Load(), "no fill reached its time limit")
		require.Empty(t, backend.GetInternal())
	})

	t.Run("fails the request with the error of a failed fill and resolves the next request", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		withoutCache := fixture.newMetastore(nil)
		failing := &failFirstReadBucket{Bucket: fixture.bucket, failedPaths: map[string]bool{}}
		m := NewObjectMetastore(failing, Config{}, log.NewNopLogger(), prometheus.NewRegistry(), WithIndexLookupCache(cache.NewMockCache(), 0))
		request := fixture.wholeRangeRequest(`app=a env=prod`)

		_, err := m.Sections(ctx, request)
		require.ErrorContains(t, err, "transient read failure")

		require.Equal(t, resolvedSections(ctx, t, withoutCache, request), resolvedSections(ctx, t, m, request))
	})

	t.Run("yields the index objects that the cache has before it reads the ones that it lacks", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		gated := newGatedBucket(fixture.bucket)
		backend := cache.NewMockCache()
		m := NewObjectMetastore(gated, Config{}, log.NewNopLogger(), prometheus.NewRegistry(), WithIndexLookupCache(backend, 0))
		request := fixture.wholeRangeRequest(`app=a`)

		indexes, err := m.GetIndexes(ctx, GetIndexesRequest{Start: request.Start, End: request.End})
		require.NoError(t, err)
		require.Len(t, indexes.Indexes, 2)
		require.NotEmpty(t, resolvedSections(ctx, t, m, request))

		lookupRequest, err := newIndexLookupRequest(tenantID, request)
		require.NoError(t, err)
		plan := &indexLookupPlan{req: lookupRequest, canonicalSelector: canonicalSelector(request.Matchers)}
		missingKey := plan.selectorMatchesKey(indexes.Indexes[1], streams.ShardLayout{}, nil)
		require.Contains(t, backend.GetInternal(), missingKey.hashed)
		delete(backend.GetInternal(), missingKey.hashed)
		gate := gated.hold(indexes.Indexes[1].Path, nil)

		boundedCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		next, stop := iter.Pull2(m.SectionsStream(boundedCtx, request))
		defer stop()

		batch, err, ok := next()
		require.NoError(t, boundedCtx.Err(), "the first batch waited for the index object that the cache lacks")
		require.True(t, ok)
		require.NoError(t, err)
		require.NotEmpty(t, batch)

		gate.open()
		for {
			_, err, ok = next()
			if !ok {
				break
			}
			require.NoError(t, err)
		}
	})

	t.Run("resolves the same sections as a metastore without the cache when the predicates are regex or not-equal", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		m := fixture.newMetastore(cache.NewMockCache())
		withoutCache := fixture.newMetastore(nil)

		for _, predicates := range []string{`detected_level=~err.*`, `detected_level!=info`, `detected_level!~info`, `detected_level=error trace_id=~t-.*`} {
			request := fixture.wholeRangeRequest(`app=a`)
			request.Predicates = parseMatchersText(predicates)

			want := resolvedSections(ctx, t, withoutCache, request)
			require.NotEmpty(t, want, predicates)
			require.Equal(t, want, resolvedSections(ctx, t, m, request), "the first request: %s", predicates)
			require.Equal(t, want, resolvedSections(ctx, t, m, request), "the repeated request: %s", predicates)
		}
	})

	t.Run("resolves the same sections as a metastore without the cache when the index objects have different shard layouts", func(t *testing.T) {
		layout8, layout64 := streams.MustShardLayoutFromBuckets(8), streams.MustShardLayoutFromBuckets(64)
		observation := func(objectPath string, bucket uint32) postings.LabelObservation {
			return postings.LabelObservation{ObjectPath: objectPath, SectionIndex: 0, ColumnName: "app", LabelValue: "foo", StreamID: 1, Time: postings.At(now), ShardBucket: bucket}
		}
		bucket := objstore.NewInMemBucket()
		createIndexObject(ctx, t, bucket, []postings.LabelObservation{observation("obj-8-lower", 1), observation("obj-8-upper", 5)}, streams.NewLayout(layout8, nil))
		createIndexObject(ctx, t, bucket, []postings.LabelObservation{observation("obj-64-lower", 20), observation("obj-64-upper", 40)}, streams.NewLayout(layout64, nil))
		m := NewObjectMetastore(bucket, Config{}, log.NewNopLogger(), prometheus.NewRegistry(), WithIndexLookupCache(cache.NewMockCache(), 0))
		withoutCache := NewObjectMetastore(bucket, Config{}, log.NewNopLogger(), prometheus.NewRegistry())

		for name, hashRange := range map[string]*streams.HashRange{
			"the upper half of the hash space":    {From: 1 << 63, To: ^uint64(0)},
			"the lower half of the hash space":    {From: 0, To: 1<<63 - 1},
			"no hash range":                       nil,
			"bucket 5 of the layout of 8":         hashRangeOfBuckets(layout8, &postings.ShardBucketRange{From: 5, To: 5}),
			"bucket 1 of the layout of 8":         hashRangeOfBuckets(layout8, &postings.ShardBucketRange{From: 1, To: 1}),
			"bucket 40 of the layout of 64":       hashRangeOfBuckets(layout64, &postings.ShardBucketRange{From: 40, To: 40}),
			"bucket 20 of the layout of 64":       hashRangeOfBuckets(layout64, &postings.ShardBucketRange{From: 20, To: 20}),
			"a bucket range of the layout of 64":  hashRangeOfBuckets(layout64, &postings.ShardBucketRange{From: 16, To: 47}),
			"a bucket range of the layout of 8":   hashRangeOfBuckets(layout8, &postings.ShardBucketRange{From: 2, To: 6}),
			"the whole hash space, as one bucket": hashRangeOfBuckets(layout8, &postings.ShardBucketRange{From: 0, To: 7}),
		} {
			request := SectionsRequest{Start: now.Add(-time.Hour), End: now.Add(time.Hour), Matchers: parseMatchersText(`app=foo`), StreamHashRange: hashRange}

			want, err := withoutCache.Sections(ctx, request)
			require.NoError(t, err)
			for _, round := range []string{"the first request", "the repeated request"} {
				got, err := m.Sections(ctx, request)
				require.NoError(t, err)
				require.ElementsMatch(t, objectPathsOf(want.Sections), objectPathsOf(got.Sections), "%s for %s", round, name)
			}
		}
	})

	t.Run("resolves the request and stores no entry when the first read of the shard layout fails", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		withoutCache := fixture.newMetastore(nil)
		layout := streams.DefaultShardLayout()

		for name, bucketRange := range map[string]*postings.ShardBucketRange{
			"one bucket":            {From: 3, To: 3},
			"a range of 16 buckets": {From: 0, To: 15},
		} {
			request := fixture.wholeRangeRequest(`app=a`)
			request.StreamHashRange = hashRangeOfBuckets(layout, bucketRange)
			want := resolvedSections(ctx, t, withoutCache, request)
			require.NotEmpty(t, want, name)

			backend := cache.NewMockCache()
			failing := &failFirstReadBucket{Bucket: fixture.bucket, failedPaths: map[string]bool{}}
			m := NewObjectMetastore(failing, Config{}, log.NewNopLogger(), prometheus.NewRegistry(), WithIndexLookupCache(backend, 0))

			require.Equal(t, want, resolvedSections(ctx, t, m, request), name)
			require.Empty(t, backend.GetInternal(), "the lookup stored entries computed without a layout: %s", name)
		}
	})

	t.Run("keeps apart the results of concurrent requests that miss different entries", func(t *testing.T) {
		layout := streams.DefaultShardLayout()
		inBucket := func(request SectionsRequest, bucket uint32) SectionsRequest {
			request.StreamHashRange = hashRangeOfBuckets(layout, &postings.ShardBucketRange{From: bucket, To: bucket})
			return request
		}
		withPredicates := func(request SectionsRequest, predicates string) SectionsRequest {
			request.Predicates = parseMatchersText(predicates)
			return request
		}

		for name, requests := range map[string][2]func(f *lookupCacheFixture) SectionsRequest{
			"selectors": {
				func(f *lookupCacheFixture) SectionsRequest { return f.wholeRangeRequest(`app=a`) },
				func(f *lookupCacheFixture) SectionsRequest { return f.wholeRangeRequest(`app=b`) },
			},
			"selectors of one bucket": {
				func(f *lookupCacheFixture) SectionsRequest { return inBucket(f.wholeRangeRequest(`app=a`), 3) },
				func(f *lookupCacheFixture) SectionsRequest { return inBucket(f.wholeRangeRequest(`app=b`), 3) },
			},
			"predicate sets": {
				func(f *lookupCacheFixture) SectionsRequest {
					return withPredicates(f.wholeRangeRequest(`app=a`), `detected_level=error`)
				},
				func(f *lookupCacheFixture) SectionsRequest {
					return withPredicates(f.wholeRangeRequest(`app=a`), `detected_level=error trace_id=t-obj-1-3`)
				},
			},
		} {
			t.Run(name, func(t *testing.T) {
				fixture := newLookupCacheFixture(t)
				withoutCache := fixture.newMetastore(nil)
				requestA, requestB := requests[0](fixture), requests[1](fixture)
				wantA, wantB := resolvedSections(ctx, t, withoutCache, requestA), resolvedSections(ctx, t, withoutCache, requestB)
				require.NotEqual(t, wantA, wantB)

				m := fixture.newSlowMetastore(cache.NewMockCache())
				responses, errs := sectionsConcurrently(ctx, m, requestA, requestB)

				require.NoError(t, errs[0])
				require.NoError(t, errs[1])
				require.Equal(t, wantA, describeSections(responses[0]))
				require.Equal(t, wantB, describeSections(responses[1]))
			})
		}
	})

	t.Run("does not let a caller that changes its descriptors change the ones of a concurrent caller", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		request := fixture.wholeRangeRequest(`app=a`)
		want := resolvedSections(ctx, t, fixture.newMetastore(nil), request)
		require.NotEmpty(t, want)

		m := fixture.newSlowMetastore(cache.NewMockCache())
		responses, errs := sectionsConcurrently(ctx, m, request, request)
		require.NoError(t, errs[0])
		require.NoError(t, errs[1])

		for _, section := range responses[0].Sections {
			for i := range section.StreamIDs {
				section.StreamIDs[i] = -1
			}
		}
		require.Equal(t, want, describeSections(responses[1]))
	})
}

func TestIndexLookupCacheFill(t *testing.T) {
	newCache := func() *indexLookupCache {
		return newIndexLookupCache(nil, cache.NewMockCache(), 0, log.NewNopLogger(), nil)
	}

	t.Run("runs one fill for concurrent calls with the same key", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var (
				c       = newCache()
				calls   atomic.Int64
				release = make(chan struct{})
				wg      sync.WaitGroup
				values  = make([]any, 8)
				errs    = make([]error, 8)
			)
			for i := range values {
				wg.Add(1)
				go func() {
					defer wg.Done()
					values[i], errs[i] = c.fill(t.Context(), "index", "key", func(context.Context) (any, error) {
						calls.Inc()
						<-release
						return 42, nil
					})
				}()
			}
			synctest.Wait()
			close(release)
			wg.Wait()

			require.Equal(t, int64(1), calls.Load())
			for i := range values {
				require.NoError(t, errs[i])
				require.Equal(t, 42, values[i])
			}
		})
	})

	t.Run("lets a call return on its own cancel without stopping the fill for the others", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c := newCache()
			release := make(chan struct{})
			fill := func(ctx context.Context) (any, error) {
				return c.fill(ctx, "index", "key", func(ctx context.Context) (any, error) {
					select {
					case <-release:
						return 42, nil
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				})
			}

			canceled, cancel := context.WithCancel(t.Context())
			firstDone := make(chan error, 1)
			go func() {
				_, err := fill(canceled)
				firstDone <- err
			}()
			synctest.Wait()

			var (
				secondValue any
				secondErr   error
				secondDone  = make(chan struct{})
			)
			go func() {
				defer close(secondDone)
				secondValue, secondErr = fill(t.Context())
			}()
			synctest.Wait()

			cancel()
			synctest.Wait()
			require.ErrorIs(t, <-firstDone, context.Canceled)

			close(release)
			<-secondDone
			require.NoError(t, secondErr)
			require.Equal(t, 42, secondValue)
		})
	})

	t.Run("starts no fill for a call that is already canceled", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c := newCache()
			var calls atomic.Int64

			canceled, cancel := context.WithCancel(t.Context())
			cancel()

			_, err := c.fill(canceled, "index", "key", func(context.Context) (any, error) {
				calls.Inc()
				return nil, nil
			})
			synctest.Wait()

			require.ErrorIs(t, err, context.Canceled)
			require.Zero(t, calls.Load())
		})
	})
}

func TestIndexLookupCacheMetrics(t *testing.T) {
	ctx := user.InjectOrgID(t.Context(), tenantID)

	newRequest := func(fixture *lookupCacheFixture) SectionsRequest {
		request := fixture.wholeRangeRequest(`app=a`)
		request.Predicates = parseMatchersText(`detected_level=error`)
		return request
	}
	requests := func(reg *prometheus.Registry, kind, result string) float64 {
		return counterOf(t, reg, "loki_dataobj_index_cache_requests_total", map[string]string{"kind": kind, "result": result})
	}
	stores := func(reg *prometheus.Registry, kind, result string) float64 {
		return counterOf(t, reg, "loki_dataobj_index_cache_stores_total", map[string]string{"kind": kind, "result": result})
	}

	t.Run("counts a miss and a store for each entry of the first request, and a hit for each entry of a repeated one", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		reg := prometheus.NewRegistry()
		m := NewObjectMetastore(fixture.bucket, Config{}, log.NewNopLogger(), reg, WithIndexLookupCache(cache.NewMockCache(), 0))

		_ = resolvedSections(ctx, t, m, newRequest(fixture))
		require.Equal(t, 2.0, requests(reg, cacheKindSelectorMatches, "miss"))
		require.Equal(t, 2.0, requests(reg, cacheKindPredicateAdmission, "miss"))
		require.Equal(t, 2.0, stores(reg, cacheKindSelectorMatches, "stored"))
		require.Equal(t, 2.0, stores(reg, cacheKindPredicateAdmission, "stored"))
		require.Equal(t, 4.0, counterOf(t, reg, "loki_dataobj_index_cache_entry_bytes", nil))

		_ = resolvedSections(ctx, t, m, newRequest(fixture))
		require.Equal(t, 2.0, requests(reg, cacheKindSelectorMatches, "hit"))
		require.Equal(t, 2.0, requests(reg, cacheKindPredicateAdmission, "hit"))
		require.Equal(t, 2.0, requests(reg, cacheKindSelectorMatches, "miss"))
	})

	t.Run("counts an entry that does not decode as invalid and not as a hit", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		reg := prometheus.NewRegistry()
		backend := cache.NewMockCache()
		m := NewObjectMetastore(fixture.bucket, Config{}, log.NewNopLogger(), reg, WithIndexLookupCache(backend, 0))

		_ = resolvedSections(ctx, t, m, newRequest(fixture))
		for key := range backend.GetInternal() {
			backend.GetInternal()[key] = []byte("not a protobuf message")
		}
		_ = resolvedSections(ctx, t, m, newRequest(fixture))

		require.Equal(t, 2.0, requests(reg, cacheKindSelectorMatches, "invalid"))
		require.Equal(t, 2.0, requests(reg, cacheKindPredicateAdmission, "invalid"))
		require.Zero(t, requests(reg, cacheKindSelectorMatches, "hit"))
	})

	t.Run("counts an error for each entry when the backend fails", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		reg := prometheus.NewRegistry()
		backend := cache.NewMockCache()
		backend.SetErr(fmt.Errorf("store failed"), fmt.Errorf("fetch failed"))
		m := NewObjectMetastore(fixture.bucket, Config{}, log.NewNopLogger(), reg, WithIndexLookupCache(backend, 0))

		_ = resolvedSections(ctx, t, m, newRequest(fixture))

		require.Equal(t, 2.0, requests(reg, cacheKindSelectorMatches, "error"))
		require.Zero(t, requests(reg, cacheKindSelectorMatches, "miss"))
		require.Equal(t, 2.0, stores(reg, cacheKindSelectorMatches, "error"))
		require.Zero(t, stores(reg, cacheKindSelectorMatches, "stored"))
	})

	t.Run("counts an entry over the size limit as oversize and not as stored", func(t *testing.T) {
		fixture := newLookupCacheFixture(t)
		reg := prometheus.NewRegistry()
		m := NewObjectMetastore(fixture.bucket, Config{}, log.NewNopLogger(), reg, WithIndexLookupCache(cache.NewMockCache(), 1))

		_ = resolvedSections(ctx, t, m, newRequest(fixture))

		require.Equal(t, 2.0, stores(reg, cacheKindSelectorMatches, "oversize"))
		require.Zero(t, stores(reg, cacheKindSelectorMatches, "stored"))
	})
}

func TestNewIndexLookupCacheBackend(t *testing.T) {
	memcachedConfig := func(prefix string, maxItemSize int) cache.Config {
		return cache.Config{
			Prefix:         prefix,
			MemcacheClient: cache.MemcachedClientConfig{Addresses: "localhost:11211", UpdateInterval: time.Minute, MaxItemSize: maxItemSize},
		}
	}

	t.Run("limits an entry to the max item size of the memcached client when it is set", func(t *testing.T) {
		backend, maxEntryBytes, err := NewIndexLookupCacheBackend(memcachedConfig("dataobj.index-cache.", 2048), prometheus.NewRegistry(), log.NewNopLogger())
		require.NoError(t, err)
		t.Cleanup(backend.Stop)

		require.Equal(t, 2048, maxEntryBytes)
	})

	t.Run("limits an entry to the default size when the max item size of the memcached client is zero", func(t *testing.T) {
		backend, maxEntryBytes, err := NewIndexLookupCacheBackend(memcachedConfig("dataobj.index-cache.", 0), prometheus.NewRegistry(), log.NewNopLogger())
		require.NoError(t, err)
		t.Cleanup(backend.Stop)

		require.Equal(t, defaultIndexLookupCacheMaxEntryBytes, maxEntryBytes)
	})

	t.Run("registers its metrics next to the ones of the memcached metadata cache", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		metadataBackend, _, err := metadatacache.NewBackend(memcachedConfig("dataobj.metadata-cache.", 0), reg, log.NewNopLogger())
		require.NoError(t, err)
		t.Cleanup(metadataBackend.Stop)

		require.NotPanics(t, func() {
			backend, _, err := NewIndexLookupCacheBackend(memcachedConfig("dataobj.index-cache.", 0), reg, log.NewNopLogger())
			require.NoError(t, err)
			t.Cleanup(backend.Stop)
		})
	})
}

// lookupCacheFixture is a bucket with index objects for the tests of the index lookup cache. Two index
// objects list three log objects: obj-1 with three sections, obj-2 with two, and obj-3 with one. The
// second index object lists section 0 of obj-1 again.
type lookupCacheFixture struct {
	bucket *getRecordingBucket
}

func newLookupCacheFixture(t *testing.T) *lookupCacheFixture {
	t.Helper()

	inMem := objstore.NewInMemBucket()

	// An index object lists a log object by its path. The same section in two index objects must list
	// the same streams, so the duplicate comes from the same function.
	firstLines, firstBlooms := lookupCacheLines("obj-1", 0, 1, 2)
	otherLines, otherBlooms := lookupCacheLines("obj-3", 0)
	duplicateLines, duplicateBlooms := lookupCacheLines("obj-1", 0)
	secondLines, secondBlooms := lookupCacheLines("obj-2", 0, 1)

	addIndexObjectWithBlooms(t, inMem, append(firstLines, otherLines...), append(firstBlooms, otherBlooms...))
	addIndexObjectWithBlooms(t, inMem, append(secondLines, duplicateLines...), append(secondBlooms, duplicateBlooms...))

	return &lookupCacheFixture{bucket: &getRecordingBucket{Bucket: inMem}}
}

// lookupCacheLines returns the lines and the bloom filters that an index object records for the
// sections of one log object. Each section has ten streams, whose times are spread over ten minutes
// from the start of the section. Section 0 starts three hours before now, section 1 two hours, and
// section 2 one hour.
func lookupCacheLines(objectPath string, sections ...int64) ([]indexedLine, []indexedBloom) {
	var (
		lines  []indexedLine
		blooms []indexedBloom
	)
	for _, section := range sections {
		for i := range int64(10) {
			streamID := section*10 + i + 1
			timestamp := now.Add(-time.Duration(3-section) * time.Hour).Add(time.Duration(i) * time.Minute)

			streamLabels := fmt.Sprintf(`{app=%q, env=%q, pod="pod-%s-%d"`, []string{"a", "b", "c"}[i%3], []string{"prod", "dev"}[(i/3)%2], objectPath, streamID)
			if i%4 == 0 {
				streamLabels += `, team="x"`
			}
			lines = append(lines, indexedLine{objectPath: objectPath, section: section, streamID: streamID, labels: streamLabels + "}", timestamp: timestamp})

			level := "info"
			if i%3 == 0 {
				level = "error"
			}
			blooms = append(blooms, indexedBloom{objectPath: objectPath, section: section, streamID: streamID, timestamp: timestamp, column: "detected_level", values: []string{level}})
			if i%2 == 0 {
				blooms = append(blooms, indexedBloom{objectPath: objectPath, section: section, streamID: streamID, timestamp: timestamp, column: "trace_id", values: []string{fmt.Sprintf("t-%s-%d", objectPath, streamID)}})
			}
		}
	}
	return lines, blooms
}

// newMetastore returns a metastore over the fixture. It has no cache when backend is nil.
func (f *lookupCacheFixture) newMetastore(backend cache.Cache) *ObjectMetastore {
	var opts []ObjectMetastoreOption
	if backend != nil {
		opts = append(opts, WithIndexLookupCache(backend, 0))
	}
	return NewObjectMetastore(f.bucket, Config{}, log.NewNopLogger(), prometheus.NewRegistry(), opts...)
}

// newSlowMetastore returns a metastore with the cache backend over the fixture, and a delay for each
// read, so that two requests that start together overlap.
func (f *lookupCacheFixture) newSlowMetastore(backend cache.Cache) *ObjectMetastore {
	slow := newGatedBucket(f.bucket)
	slow.latency = 100 * time.Millisecond
	return NewObjectMetastore(slow, Config{}, log.NewNopLogger(), prometheus.NewRegistry(), WithIndexLookupCache(backend, 0))
}

type timeWindowCase struct {
	name       string
	start, end time.Time
}

// windows returns time ranges that keep different sections of the fixture. Every range overlaps the
// index objects, which the ToC lists for the whole time of the sections.
func (f *lookupCacheFixture) windows() []timeWindowCase {
	return []timeWindowCase{
		{"all sections", now.Add(-4 * time.Hour), now.Add(time.Hour)},
		{"the first section only", now.Add(-3 * time.Hour), now.Add(-3*time.Hour + 30*time.Minute)},
		{"a gap between two sections", now.Add(-2*time.Hour - 30*time.Minute), now.Add(-2*time.Hour - 20*time.Minute)},
		{"a few minutes of the last section", now.Add(-time.Hour + 3*time.Minute), now.Add(-time.Hour + 5*time.Minute)},
		{"one minute later than the first section", now.Add(-3*time.Hour + time.Minute), now.Add(-3*time.Hour + 31*time.Minute)},
	}
}

func (f *lookupCacheFixture) wholeRangeRequest(selector string) SectionsRequest {
	window := f.windows()[0]
	return SectionsRequest{Start: window.start, End: window.end, Matchers: parseMatchersText(selector)}
}

// requests returns requests that differ in selector, predicates, time range and shard.
func (f *lookupCacheFixture) requests() []SectionsRequest {
	layout := streams.DefaultShardLayout()
	shards := []*streams.HashRange{
		nil,
		hashRangeOfBuckets(layout, &postings.ShardBucketRange{From: 3, To: 3}),
		hashRangeOfBuckets(layout, &postings.ShardBucketRange{From: 17, To: 17}),
		hashRangeOfBuckets(layout, &postings.ShardBucketRange{From: 0, To: 15}),
		hashRangeOfBuckets(layout, &postings.ShardBucketRange{From: 0, To: layout.Buckets() - 1}),
	}

	var requests []SectionsRequest
	for _, selector := range []string{`app=a`, `app=a env=prod`, `app=~a|b team!=x`, `app=b env!=dev`, `env=prod`} {
		for _, predicates := range []string{``, `detected_level=error`, `env=prod`} {
			for _, window := range f.windows() {
				for _, shard := range shards {
					requests = append(requests, SectionsRequest{
						Start:           window.start,
						End:             window.end,
						Matchers:        parseMatchersText(selector),
						Predicates:      parseMatchersText(predicates),
						StreamHashRange: shard,
					})
				}
			}
		}
	}
	return requests
}

// resolvedSections returns what the metastore resolves for the request, one line for each section,
// in order.
func resolvedSections(ctx context.Context, t *testing.T, m *ObjectMetastore, request SectionsRequest) []string {
	t.Helper()

	response, err := m.Sections(ctx, request)
	require.NoError(t, err)
	return describeSections(response)
}

// describeSections returns one line for each section of the response, in order.
func describeSections(response SectionsResponse) []string {
	lines := make([]string, 0, len(response.Sections))
	for _, section := range response.Sections {
		lines = append(lines, fmt.Sprintf("%s#%d streams=%v start=%d end=%d ambiguous=%v",
			section.ObjectPath, section.SectionIdx, section.StreamIDs, section.Start.UnixNano(), section.End.UnixNano(), section.AmbiguousPredicates))
	}
	slices.Sort(lines)
	return lines
}

// sectionsConcurrently resolves the two requests at the same time, and returns their responses and
// errors in the order of the requests.
func sectionsConcurrently(ctx context.Context, m *ObjectMetastore, first, second SectionsRequest) (responses [2]SectionsResponse, errs [2]error) {
	var wg sync.WaitGroup
	for i, request := range []SectionsRequest{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			responses[i], errs[i] = m.Sections(ctx, request)
		}()
	}
	wg.Wait()
	return responses, errs
}

func describeRequest(request SectionsRequest) string {
	return fmt.Sprintf("selector=%v predicates=%v start=%s end=%s shard=%v", request.Matchers, request.Predicates, request.Start.Format(time.TimeOnly), request.End.Format(time.TimeOnly), request.StreamHashRange)
}

// parseMatchersText parses matchers written as names, operators and values separated by spaces,
// such as `app=a env!=dev`. A value holds no space.
func parseMatchersText(text string) []*labels.Matcher {
	var matchers []*labels.Matcher
	for _, field := range strings.Fields(text) {
		matchers = append(matchers, parseMatcherText(field))
	}
	return matchers
}

func parseMatcherText(text string) *labels.Matcher {
	for _, operator := range []struct {
		text string
		kind labels.MatchType
	}{{"!~", labels.MatchNotRegexp}, {"=~", labels.MatchRegexp}, {"!=", labels.MatchNotEqual}, {"=", labels.MatchEqual}} {
		if name, value, ok := strings.Cut(text, operator.text); ok {
			return labels.MustNewMatcher(operator.kind, name, value)
		}
	}
	panic("not a matcher: " + text)
}

// cacheKeysWithPrefix returns the keys of the backend that start with the prefix, in order.
func cacheKeysWithPrefix(backend cache.MockCache, prefix string) []string {
	var keys []string
	for key := range backend.GetInternal() {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return keys
}

// changeVersionOfEntries stores each entry of the kind with another version. The kind is "m/" for
// selector matches and "a/" for predicate admissions.
func changeVersionOfEntries(t *testing.T, backend cache.MockCache, kind string) {
	t.Helper()

	keys := cacheKeysWithPrefix(backend, indexLookupCacheKeyPrefix+kind)
	require.NotEmpty(t, keys)
	for _, key := range keys {
		var (
			changed []byte
			err     error
		)
		switch kind {
		case "m/":
			var entry CachedSelectorMatches
			require.NoError(t, entry.Unmarshal(backend.GetInternal()[key]))
			entry.Version++
			changed, err = entry.Marshal()
		case "a/":
			var entry CachedPredicateAdmission
			require.NoError(t, entry.Unmarshal(backend.GetInternal()[key]))
			entry.Version++
			changed, err = entry.Marshal()
		default:
			t.Fatalf("unknown kind of entry %q", kind)
		}
		require.NoError(t, err)
		backend.GetInternal()[key] = changed
	}
}

// counterOf returns the sum of the counters, or of the histogram sample counts, that are called name
// and have all the labels in wantLabels.
func counterOf(t *testing.T, reg *prometheus.Registry, name string, wantLabels map[string]string) float64 {
	t.Helper()

	families, err := reg.Gather()
	require.NoError(t, err)

	var sum float64
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
	metric:
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if want, ok := wantLabels[label.GetName()]; ok && want != label.GetValue() {
					continue metric
				}
			}
			if metric.GetCounter() != nil {
				sum += metric.GetCounter().GetValue()
			}
			if metric.GetHistogram() != nil {
				sum += float64(metric.GetHistogram().GetSampleCount())
			}
		}
	}
	return sum
}

// failFirstReadBucket fails the first read of each index object and lets the next ones through.
type failFirstReadBucket struct {
	objstore.Bucket

	mtx         sync.Mutex
	failedPaths map[string]bool
}

func (b *failFirstReadBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	if strings.Contains(name, "/objects/") {
		b.mtx.Lock()
		failed := b.failedPaths[name]
		b.failedPaths[name] = true
		b.mtx.Unlock()

		if !failed {
			return nil, fmt.Errorf("transient read failure")
		}
	}
	return b.Bucket.GetRange(ctx, name, off, length)
}

// deadlineBlockingBucket makes every read whose context has a deadline wait until the deadline. It
// models an object store that is slower than the time limit of a fill, while a request without a
// deadline still reads.
type deadlineBlockingBucket struct {
	objstore.Bucket

	blockedReads atomic.Int64
}

func (b *deadlineBlockingBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	if err := b.blockUntilDeadline(ctx); err != nil {
		return nil, err
	}
	return b.Bucket.Get(ctx, name)
}

func (b *deadlineBlockingBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	if err := b.blockUntilDeadline(ctx); err != nil {
		return nil, err
	}
	return b.Bucket.GetRange(ctx, name, off, length)
}

func (b *deadlineBlockingBucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	if err := b.blockUntilDeadline(ctx); err != nil {
		return objstore.ObjectAttributes{}, err
	}
	return b.Bucket.Attributes(ctx, name)
}

func (b *deadlineBlockingBucket) blockUntilDeadline(ctx context.Context) error {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		return nil
	}
	b.blockedReads.Add(1)
	<-ctx.Done()
	return ctx.Err()
}
