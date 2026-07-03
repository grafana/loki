package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
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

func (f *fakeSource) each(_ context.Context, fn func(sec *dataobj.Section, objPath string, ordinal int) error) error {
	for i, it := range f.items {
		if err := fn(it.sec, it.objPath, i); err != nil {
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

	prod := c.nameValue[postingKey{name: "env", value: "prod"}]
	require.NotNil(t, prod)
	require.Equal(t, int64(2), prod.sectionSpread, "env=prod appears in both fed sections")
	require.Equal(t, int64(2), prod.objectSpread, "env=prod spans both objects")
	require.Equal(t, int64(4), prod.postingEntries, "2 rows per section, 2 sections")

	api := c.nameValue[postingKey{name: "app", value: "api"}]
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

	prod := c.nameValue[postingKey{name: "env", value: "prod"}]
	require.NotNil(t, prod)
	require.Equal(t, int64(3), prod.sectionSpread)
	require.Equal(t, int64(2), prod.objectSpread, "three sections but only two distinct objects")
}

// TestReport_Smoke verifies report emits summary and top-N tables without
// panicking and includes the observed keys.
func TestReport_Smoke(t *testing.T) {
	ctx := context.Background()
	ts := time.Unix(0, 0).UTC()

	sec, closer := buildPostingsSection(t, []postings.LabelObservation{
		{ObjectPath: "/logs/a", SectionIndex: 0, ColumnName: "env", LabelValue: "prod", StreamID: 1, Timestamp: ts, UncompressedSize: 100},
		{ObjectPath: "/logs/a", SectionIndex: 0, ColumnName: "app", LabelValue: "api", StreamID: 2, Timestamp: ts, UncompressedSize: 50},
	})
	defer closer()

	c := newCollector(collectorOptions{byNameValue: true, byName: true})
	require.NoError(t, c.collect(ctx, &fakeSource{items: []fakeItem{{sec: sec, objPath: "index/A"}}}))

	var buf bytes.Buffer
	c.report(&buf, 20)
	out := buf.String()

	require.Contains(t, out, "name-value locality")
	require.Contains(t, out, "name locality")
	require.Contains(t, out, "env=prod")
	require.Contains(t, out, "app=api")
	require.Contains(t, out, "Top 20 by sectionSpread")
	require.Contains(t, out, "sectionSpread:")
}
