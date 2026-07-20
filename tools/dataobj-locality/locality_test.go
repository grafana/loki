package main

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// fakeSource yields a fixed list of postings sections with caller-supplied
// object paths, so the collector's fold can be exercised without object
// storage.
type fakeSource struct {
	items []fakeItem
}

type fakeItem struct {
	sec     *dataobj.Section
	objPath string
}

func (f *fakeSource) each(_ context.Context, fn func(sec *dataobj.Section, objPath string, sectionIdx int64) error) error {
	for i, it := range f.items {
		if err := fn(it.sec, it.objPath, int64(i)); err != nil {
			return err
		}
	}
	return nil
}

// buildPostingsSection builds a single postings section from the given label
// observations and returns it together with a closer.
func buildPostingsSection(t *testing.T, obs []postings.LabelObservation) (*dataobj.Section, func()) {
	t.Helper()

	b := postings.NewBuilder(nil, 0, 0, 1<<20)
	for _, o := range obs {
		b.ObserveLabelPosting(o)
	}

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)

	for _, s := range obj.Sections() {
		if postings.CheckSection(s) {
			return s, func() { _ = closer.Close() }
		}
	}
	closer.Close()
	t.Fatal("no postings section produced")
	return nil, nil
}

func TestIndexObjectSource_AllTenants(t *testing.T) {
	ctx := t.Context()
	rawBucket := objstore.NewInMemBucket()
	indexBucket := objstore.NewPrefixedBucket(rawBucket, "index/v0")
	window := time.Now().UTC().Truncate(metastore.MetastoreWindowSize)
	objectPath := "indexes/ab/cdef"

	objBuilder := dataobj.NewBuilder(nil)
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		builder := postings.NewBuilder(nil, 0, 0, 1<<20)
		builder.SetTenant(tenant)
		builder.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath:       "logs/" + tenant,
			SectionIndex:     0,
			ColumnName:       "service_name",
			LabelValue:       tenant,
			StreamID:         1,
			Timestamp:        window,
			UncompressedSize: 100,
		})
		require.NoError(t, objBuilder.Append(builder))
	}
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()
	reader, err := obj.Reader(ctx)
	require.NoError(t, err)
	require.NoError(t, indexBucket.Upload(ctx, objectPath, reader))
	require.NoError(t, reader.Close())

	toc := metastore.NewTableOfContentsWriter(indexBucket, log.NewNopLogger())
	require.NoError(t, toc.WriteEntry(ctx, objectPath, []multitenancy.TimeRange{
		{Tenant: "tenant-a", MinTime: window, MaxTime: window.Add(time.Hour)},
		{Tenant: "tenant-b", MinTime: window, MaxTime: window.Add(time.Hour)},
	}))

	for _, tc := range []struct {
		name       string
		tenant     string
		wantTenant []string
	}{
		{name: "all tenants", wantTenant: []string{"tenant-a", "tenant-b"}},
		{name: "one tenant", tenant: "tenant-a", wantTenant: []string{"tenant-a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := newIndexObjectSource(
				rawBucket,
				metastore.Config{IndexStoragePrefix: "index/v0", PartitionRatio: 10},
				tc.tenant,
				window,
				window.Add(time.Hour),
				log.NewNopLogger(),
				2,
			)
			var tenants []string
			require.NoError(t, source.each(ctx, func(sec *dataobj.Section, _ string, _ int64) error {
				tenants = append(tenants, sec.Tenant)
				return nil
			}))
			require.ElementsMatch(t, tc.wantTenant, tenants)
		})
	}
}

// TestCollector_Fold verifies the two-level fold across sections and objects
// for both the name-value and name rollups.
func TestCollector_Fold(t *testing.T) {
	ctx := context.Background()
	ts := time.Unix(0, 0).UTC()

	// One postings section: env=prod referenced from two logs sections, and
	// app=api from one. postingEntries counts rows (= logs-section refs), so
	// env=prod yields two rows here.
	sec, closer := buildPostingsSection(t, []postings.LabelObservation{
		{ObjectPath: "/logs/a", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 1, Timestamp: ts, UncompressedSize: 100},
		{ObjectPath: "/logs/a", SectionIndex: 1, ColumnName: "env", LabelValue: "prod", StreamID: 2, Timestamp: ts, UncompressedSize: 100},
		{ObjectPath: "/logs/a", SectionIndex: 0, ColumnName: "app", LabelValue: "api", StreamID: 1, Timestamp: ts, UncompressedSize: 50},
	})
	defer closer()

	// Feed the same physical section under two distinct object paths so the key
	// spans two sections in two objects.
	src := &fakeSource{items: []fakeItem{
		{sec: sec, objPath: "index/A"},
		{sec: sec, objPath: "index/B"},
	}}

	c := newCollector(collectorOptions{byNameValue: true, byName: true})
	require.NoError(t, c.collect(ctx, src))

	prod := c.nameValue[labelValue{name: "env", value: "prod"}]
	require.NotNil(t, prod)
	require.Equal(t, int64(2), prod.sectionSpread, "env=prod appears in both fed sections")
	require.Equal(t, int64(2), prod.objectSpread, "env=prod spans both objects")
	require.Equal(t, int64(4), prod.postingEntries, "2 rows per section, 2 sections")

	api := c.nameValue[labelValue{name: "app", value: "api"}]
	require.NotNil(t, api)
	require.Equal(t, int64(2), api.sectionSpread)
	require.Equal(t, int64(2), api.objectSpread)
	require.Equal(t, int64(2), api.postingEntries)

	// The name rollup for "env" mirrors env=prod because prod is its only value.
	env := c.name["env"]
	require.NotNil(t, env)
	require.Equal(t, prod.sectionSpread, env.sectionSpread)
	require.Equal(t, prod.postingEntries, env.postingEntries)
}

// TestCollector_ObjectSpreadContiguous verifies objectSpread does not
// double-count when a key appears in multiple sections of the same object.
func TestCollector_ObjectSpreadContiguous(t *testing.T) {
	ctx := context.Background()
	ts := time.Unix(0, 0).UTC()

	sec, closer := buildPostingsSection(t, []postings.LabelObservation{
		{ObjectPath: "/logs/a", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 1, Timestamp: ts, UncompressedSize: 100},
	})
	defer closer()

	// Two sections of the same object, followed by one section of another.
	src := &fakeSource{items: []fakeItem{
		{sec: sec, objPath: "index/A"},
		{sec: sec, objPath: "index/A"},
		{sec: sec, objPath: "index/B"},
	}}

	c := newCollector(collectorOptions{byNameValue: true})
	require.NoError(t, c.collect(ctx, src))

	prod := c.nameValue[labelValue{name: "env", value: "prod"}]
	require.NotNil(t, prod)
	require.Equal(t, int64(3), prod.sectionSpread)
	require.Equal(t, int64(2), prod.objectSpread, "three sections but only two distinct objects")
}

// TestCollector_LogsLocality verifies the logs-section rollup counts distinct
// logs sections, sums per-value uncompressed bytes, and tracks distinct logs
// objects for the sort-key label.
func TestCollector_LogsLocality(t *testing.T) {
	ctx := context.Background()
	ts := time.Unix(0, 0).UTC()

	// service_name=foo lives in three distinct logs sections spanning two logs
	// objects; bar lives in a single section. env=prod is present too and must
	// be ignored because it is not the sort key.
	sec, closer := buildPostingsSection(t, []postings.LabelObservation{
		{ObjectPath: "/logs/a", SectionIndex: 0, ColumnName: "service_name", LabelValue: "foo", StreamID: 1, Timestamp: ts, UncompressedSize: 100},
		{ObjectPath: "/logs/a", SectionIndex: 1, ColumnName: "service_name", LabelValue: "foo", StreamID: 2, Timestamp: ts, UncompressedSize: 200},
		{ObjectPath: "/logs/b", SectionIndex: 0, ColumnName: "service_name", LabelValue: "foo", StreamID: 3, Timestamp: ts, UncompressedSize: 300},
		{ObjectPath: "/logs/a", SectionIndex: 0, ColumnName: "service_name", LabelValue: "bar", StreamID: 4, Timestamp: ts, UncompressedSize: 50},
		{ObjectPath: "/logs/a", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 1, Timestamp: ts, UncompressedSize: 999},
	})
	defer closer()

	c := newCollector(collectorOptions{logsLocality: true, sortKey: "service_name", logsSectionTargetBytes: 200})
	require.NoError(t, c.collect(ctx, &fakeSource{items: []fakeItem{{sec: sec, objPath: "index/A"}}}))

	foo := c.logsBySortValue["foo"]
	require.NotNil(t, foo)
	require.Len(t, foo.sections, 3, "foo spans three distinct logs sections")
	require.Len(t, foo.objects, 2, "foo spans two distinct logs objects")

	var fooBytes int64
	for _, sz := range foo.sections {
		fooBytes += sz
	}
	require.Equal(t, int64(600), fooBytes)

	bar := c.logsBySortValue["bar"]
	require.NotNil(t, bar)
	require.Len(t, bar.sections, 1)

	snapshot := logsSnapshotFromCollector("window", "tenant", c)
	require.Equal(t, uint64(2), snapshot.sectionSpread.count)
	require.Equal(t, 4.0, snapshot.sectionSpread.sum)

	// env is not the sort key, so it is not tracked in the logs rollup.
	require.NotContains(t, c.logsBySortValue, "prod")
}

// TestCollector_LogsLocalityDedup verifies the same logs section referenced from
// multiple postings sections (e.g. overlapping index objects) is counted once.
func TestCollector_LogsLocalityDedup(t *testing.T) {
	ctx := context.Background()
	ts := time.Unix(0, 0).UTC()

	sec, closer := buildPostingsSection(t, []postings.LabelObservation{
		{ObjectPath: "/logs/a", SectionIndex: 0, ColumnName: "service_name", LabelValue: "foo", StreamID: 1, Timestamp: ts, UncompressedSize: 100},
		{ObjectPath: "/logs/a", SectionIndex: 1, ColumnName: "service_name", LabelValue: "foo", StreamID: 2, Timestamp: ts, UncompressedSize: 100},
	})
	defer closer()

	// Feed the same physical postings section under two index objects. The
	// underlying logs sections must not be double-counted.
	src := &fakeSource{items: []fakeItem{
		{sec: sec, objPath: "index/A"},
		{sec: sec, objPath: "index/B"},
	}}

	c := newCollector(collectorOptions{logsLocality: true, sortKey: "service_name", logsSectionTargetBytes: 200})
	require.NoError(t, c.collect(ctx, src))

	foo := c.logsBySortValue["foo"]
	require.NotNil(t, foo)
	require.Len(t, foo.sections, 2, "two logs sections despite two index objects referencing them")
	require.Len(t, foo.objects, 1, "both references point at the same logs object")
}
