package logproto

import (
	"fmt"
	"math/rand"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"
)

func TestEncodingsAreMutuallyUndecodable(t *testing.T) {
	oneEntry := []push.Entry{entry(1, "x")}
	oneGroup := []ResourceLogs{{ScopeLogs: []ScopeLogs{{Entries: oneEntry}}}}

	tests := []struct {
		name          string
		record        interface{ Marshal() ([]byte, error) }
		decodesFlat   bool
		decodesNested bool
	}{
		{
			name:        "flat with entries and a hash",
			record:      &Stream{Labels: `{a="b"}`, Hash: 7, Entries: oneEntry},
			decodesFlat: true,
		},
		{
			name:        "flat with entries and a zero hash",
			record:      &Stream{Labels: `{a="b"}`, Entries: oneEntry},
			decodesFlat: true,
		},
		{
			name:        "flat with a hash and no entries",
			record:      &Stream{Labels: `{a="b"}`, Hash: 7},
			decodesFlat: true,
		},
		{
			name:        "flat with a single zero valued entry",
			record:      &Stream{Labels: `{a="b"}`, Entries: []push.Entry{{}}},
			decodesFlat: true,
		},
		{
			name:          "nested with groups and a hash",
			record:        &InternalStreamAdapter{Labels: `{a="b"}`, Hash: 7, ResourceLogs: oneGroup},
			decodesNested: true,
		},
		{
			name:          "nested with groups and a zero hash",
			record:        &InternalStreamAdapter{Labels: `{a="b"}`, ResourceLogs: oneGroup},
			decodesNested: true,
		},
		{
			name:          "nested with a single empty group",
			record:        &InternalStreamAdapter{Labels: `{a="b"}`, ResourceLogs: []ResourceLogs{{}}},
			decodesNested: true,
		},
		{
			// The only shape both accept: it carries no field they disagree about.
			name:          "labels alone",
			record:        &Stream{Labels: `{a="b"}`},
			decodesFlat:   true,
			decodesNested: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := tt.record.Marshal()
			require.NoError(t, err)

			var flat Stream
			flatErr := flat.Unmarshal(data)
			var nested InternalStreamAdapter
			nestedErr := nested.Unmarshal(data)

			require.Equal(t, tt.decodesFlat, flatErr == nil, "flat decode: %v", flatErr)
			require.Equal(t, tt.decodesNested, nestedErr == nil, "nested decode: %v", nestedErr)
			require.True(t, tt.decodesFlat || tt.decodesNested, "a record must decode as one of the two")

			if tt.decodesFlat && tt.decodesNested {
				var fromNested Stream
				nested.ToStream(&fromNested)
				require.Equal(t, flat, fromNested,
					"a record both encodings accept must mean the same thing either way")
			}
		})
	}
}

func TestFromPushRequest(t *testing.T) {
	t.Run("empty request preserves format", func(t *testing.T) {
		converted := FromPushRequest(&PushRequest{Format: "otlp"})
		require.Empty(t, converted.Streams)
		require.Equal(t, "otlp", converted.Format)
	})

	t.Run("streams share entries but keep their groups independent", func(t *testing.T) {
		req := PushRequest{Format: "otlp", Streams: []Stream{
			{Labels: `{app="first"}`, Hash: 1, Entries: []push.Entry{entry(1, "first", attrs("key", "value")...)}},
			{Labels: `{app="second"}`, Hash: 2, Entries: []push.Entry{entry(2, "second")}},
			{Labels: `{app="empty"}`, Hash: 3},
		}}
		converted := FromPushRequest(&req)
		require.Equal(t, req.Format, converted.Format)
		require.Len(t, converted.Streams, len(req.Streams))
		for i := range converted.Streams {
			stream := &converted.Streams[i]
			require.Len(t, stream.ResourceLogs, 1)
			require.Empty(t, stream.ResourceLogs[0].Attrs)
			require.Len(t, stream.ResourceLogs[0].ScopeLogs, 1)
			require.Empty(t, stream.ResourceLogs[0].ScopeLogs[0].Attrs)
			require.Equal(t, req.Streams[i], stream.FlatView())
			if len(req.Streams[i].Entries) > 0 {
				require.Same(t, &req.Streams[i].Entries[0], &stream.ResourceLogs[0].ScopeLogs[0].Entries[0])
			}
		}

		first := &converted.Streams[0]
		first.ResourceLogs[0].ScopeLogs = append(first.ResourceLogs[0].ScopeLogs, ScopeLogs{
			Entries: []push.Entry{entry(3, "new scope")},
		})
		require.Equal(t, req.Streams[1], converted.Streams[1].FlatView())
		first.ResourceLogs = append(first.ResourceLogs, ResourceLogs{
			ScopeLogs: []ScopeLogs{{Entries: []push.Entry{entry(4, "new resource")}}},
		})
		require.Equal(t, req.Streams[1], converted.Streams[1].FlatView())
		require.Equal(t, req.Streams[2], converted.Streams[2].FlatView())
	})
}

func BenchmarkFromPushRequest(b *testing.B) {
	for _, count := range []int{1, 100, 1000} {
		b.Run(fmt.Sprintf("streams=%d", count), func(b *testing.B) {
			req := PushRequest{Streams: make([]Stream, count)}
			for i := range req.Streams {
				req.Streams[i] = Stream{Labels: `{app="test"}`, Hash: uint64(i), Entries: []push.Entry{entry(1, "line")}}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				converted := FromPushRequest(&req)
				runtime.KeepAlive(converted)
			}
		})
	}
}

func TestToStream(t *testing.T) {
	tests := []struct {
		name   string
		nested InternalStreamAdapter
		want   Stream
	}{
		{
			name:   "labels alone",
			nested: InternalStreamAdapter{Labels: `{a="b"}`},
			want:   Stream{Labels: `{a="b"}`},
		},
		{
			name:   "an empty group",
			nested: InternalStreamAdapter{Labels: `{a="b"}`, ResourceLogs: []ResourceLogs{{}}},
			want:   Stream{Labels: `{a="b"}`},
		},
		{
			name:   "an empty scope",
			nested: InternalStreamAdapter{Labels: `{a="b"}`, ResourceLogs: []ResourceLogs{{ScopeLogs: []ScopeLogs{{}}}}},
			want:   Stream{Labels: `{a="b"}`},
		},
		{
			name: "entries with nothing lifted off them",
			nested: InternalStreamAdapter{
				Labels: `{a="b"}`,
				Hash:   7,
				ResourceLogs: []ResourceLogs{
					resource(
						attrs(),
						scope(
							attrs(),
							entry(1, "x", attrs("trace_id", "1")...),
							entry(2, "y"),
						),
					),
				},
			},
			want: Stream{Labels: `{a="b"}`, Hash: 7, Entries: []push.Entry{
				entry(1, "x", attrs("trace_id", "1")...),
				entry(2, "y"),
			}},
		},
		{
			name: "resource and scope attributes resolved onto each entry",
			nested: InternalStreamAdapter{
				Labels: `{a="b"}`,
				Hash:   7,
				ResourceLogs: []ResourceLogs{
					resource(
						attrs("host", "host-1", "shared", "resource"),
						scope(
							attrs("scope", "lib", "shared", "scope"),
							entry(1, "x", attrs("shared", "entry")...),
							entry(2, "y"),
						),
					),
				},
			},
			want: Stream{Labels: `{a="b"}`, Hash: 7, Entries: []push.Entry{
				entry(1, "x", attrs("shared", "entry", "scope", "lib", "host", "host-1")...),
				entry(2, "y", attrs("scope", "lib", "shared", "scope", "host", "host-1")...),
			}},
		},
		{
			name: "entries under separate groups",
			nested: InternalStreamAdapter{
				Labels: `{a="b"}`,
				ResourceLogs: []ResourceLogs{
					resource(attrs("host", "host-1"), scope(attrs(), entry(1, "one"))),
					resource(attrs("host", "host-2"), scope(attrs(), entry(2, "two"))),
				},
			},
			want: Stream{Labels: `{a="b"}`, Entries: []push.Entry{
				entry(1, "one", attrs("host", "host-1")...),
				entry(2, "two", attrs("host", "host-2")...),
			}},
		},
		{
			name: "attributes priority",
			nested: InternalStreamAdapter{
				Labels: `{a="b"}`,
				ResourceLogs: []ResourceLogs{
					resource(
						attrs("host", "resource-1"),
						scope(
							attrs("host", "scope-11"),
							entry(111, "e111", attrs("host", "entry-111")...),
							entry(112, "e112"),
						),
						scope(
							attrs(),
							entry(121, "e121", attrs("host", "entry-121")...),
							entry(122, "e122"),
						),
					),
					resource(
						attrs(),
						scope(
							attrs("host", "scope-21"),
							entry(211, "e211", attrs("host", "entry-211")...),
							entry(212, "e212"),
						),
						scope(
							attrs(),
							entry(221, "e221", attrs("host", "entry-221")...),
							entry(222, "e222"),
						),
					),
					resource(
						attrs(),
						scope(
							attrs(),
							entry(311, "e311", attrs("host", "entry-311")...),
							entry(312, "e312"),
						),
					),
				},
			},
			want: Stream{Labels: `{a="b"}`, Entries: []push.Entry{
				// resource 1
				// -> scope 11
				entry(111, "e111", attrs("host", "entry-111")...),
				entry(112, "e112", attrs("host", "scope-11")...),
				// -> scope 12
				entry(121, "e121", attrs("host", "entry-121")...),
				entry(122, "e122", attrs("host", "resource-1")...),

				// resource 2
				// -> scope 21
				entry(211, "e211", attrs("host", "entry-211")...),
				entry(212, "e212", attrs("host", "scope-21")...),
				// -> scope 22
				entry(221, "e221", attrs("host", "entry-221")...),
				entry(222, "e222"),

				// resource 3
				entry(311, "e311", attrs("host", "entry-311")...),
				entry(312, "e312"),
			}},
		},
	}

	// Shared by all cases, so every call gets the previous result's leftovers.
	// The decoder reuses the stream the same way.
	var reused Stream

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before, err := tt.nested.Marshal()
			require.NoError(t, err)

			var fresh Stream
			tt.nested.ToStream(&fresh)
			require.Equal(t, tt.want, fresh)

			tt.nested.ToStream(&reused)
			requireSameStream(t, tt.want, reused)

			after, err := tt.nested.Marshal()
			require.NoError(t, err)
			require.Equal(t, before, after, "flattening a record must not modify it")
		})
	}
}

// TestToStreamIntoAReusedStream checks that a reused output gives the same result as a fresh
// one. Random shapes are needed here: a leak only shows when one record follows a different one.
func TestToStreamIntoAReusedStream(t *testing.T) {
	// Fixed seed, so a failure always repeats. To try other shapes:
	// go test -fuzz FuzzToStreamIntoAReusedStream ./pkg/logproto
	requireReuseIsInvisible(t, 1, 1000)
}

func FuzzToStreamIntoAReusedStream(f *testing.F) {
	f.Add(int64(1))
	f.Add(int64(-1))
	f.Fuzz(func(t *testing.T, seed int64) {
		requireReuseIsInvisible(t, seed, 50)
	})
}

func requireReuseIsInvisible(t *testing.T, seed int64, records int) {
	t.Helper()

	r := rand.New(rand.NewSource(seed))
	var reused Stream

	for i := range records {
		nested := randomStreamAdapter(r)
		where := fmt.Sprintf("record %d of seed %d", i, seed)

		before, err := nested.Marshal()
		require.NoError(t, err, where)

		var fresh Stream
		nested.ToStream(&fresh)

		nested.ToStream(&reused)
		requireSameStream(t, fresh, reused, where)

		// More entries than any random record has, so a leftover entry always shows up,
		// not only after a longer record.
		primed := Stream{Labels: "stale", Hash: 1 << 20, Entries: make([]push.Entry, 64)}
		nested.ToStream(&primed)
		requireSameStream(t, fresh, primed, where)

		want := flattenReference(nested)
		require.Equal(t, want.Labels, reused.Labels, where)
		require.Equal(t, want.Hash, reused.Hash, where)
		requireSameEntries(t, want.Entries, reused.Entries, where)

		after, err := nested.Marshal()
		require.NoError(t, err, where)
		require.Equal(t, before, after, "flattening a record must not modify it: %s", where)
	}
}

// requireSameStream compares streams field by field, not as whole structs. ToStream truncates
// the entries slice instead of replacing it, so a record with no entries leaves Entries empty on
// a reused stream but nil on a fresh one. Both mean "no entries", and the flat format does the
// same, so that difference is not worth asserting.
func requireSameStream(t *testing.T, want, got Stream, msgAndArgs ...any) {
	t.Helper()

	require.Equal(t, want.Labels, got.Labels, msgAndArgs...)
	require.Equal(t, want.Hash, got.Hash, msgAndArgs...)
	if len(want.Entries) == 0 {
		require.Empty(t, got.Entries, msgAndArgs...)
		return
	}
	require.Equal(t, want.Entries, got.Entries, msgAndArgs...)
}

// requireSameEntries compares entries in full and treats nil and empty metadata as equal.
// ToStream leaves metadata alone when there is nothing to resolve and builds a new slice when
// there is, so two correct results can differ in nil-ness alone.
func requireSameEntries(t *testing.T, want, got []push.Entry, msgAndArgs ...any) {
	t.Helper()

	require.Len(t, got, len(want), msgAndArgs...)
	for i := range want {
		require.Equal(t, want[i].Timestamp, got[i].Timestamp, msgAndArgs...)
		require.Equal(t, want[i].Line, got[i].Line, msgAndArgs...)
		require.Equal(t, want[i].Parsed, got[i].Parsed, msgAndArgs...)

		if len(want[i].StructuredMetadata) == 0 {
			require.Empty(t, got[i].StructuredMetadata, msgAndArgs...)
			continue
		}
		require.Equal(t, want[i].StructuredMetadata, got[i].StructuredMetadata, msgAndArgs...)
	}
}

// flattenReference is a slow and obvious ToStream. It allocates everything and keeps no buffers,
// so its output can never carry anything from an earlier entry, scope or record.
//
// The test needs it because comparing two ToStream calls only catches leaks between calls. A
// buffer shared inside one record would look the same in both.
func flattenReference(nested InternalStreamAdapter) Stream {
	out := Stream{Labels: nested.Labels, Hash: nested.Hash}

	for _, res := range nested.ResourceLogs {
		for _, sc := range res.ScopeLogs {
			for _, e := range sc.Entries {
				// Entry wins over scope, scope wins over resource. Duplicates inside the
				// entry's own metadata stay as they are.
				md := slices.Clone(e.StructuredMetadata)
				taken := make(map[string]bool, len(md))
				for _, attr := range md {
					taken[attr.Name] = true
				}
				for _, attr := range slices.Concat(sc.Attrs, res.Attrs) {
					if taken[attr.Name] {
						continue
					}
					taken[attr.Name] = true
					md = append(md, attr)
				}

				out.Entries = append(out.Entries, push.Entry{
					Timestamp:          e.Timestamp,
					Line:               e.Line,
					StructuredMetadata: md,
					Parsed:             e.Parsed,
				})
			}
		}
	}

	return out
}

// randomStreamAdapter builds a record of a random shape. Any level can be empty, so some records
// come out with no entries at all.
func randomStreamAdapter(r *rand.Rand) InternalStreamAdapter {
	nested := InternalStreamAdapter{
		Labels: fmt.Sprintf(`{app="a-%d"}`, r.Intn(4)),
		Hash:   uint64(r.Intn(8)),
	}

	for range r.Intn(4) {
		res := resource(randomAttrs(r))
		for range r.Intn(4) {
			sc := scope(randomAttrs(r))
			for range r.Intn(5) {
				sc.Entries = append(sc.Entries, entry(
					int64(r.Intn(1000)+1),
					fmt.Sprintf("line-%d", r.Intn(1000)),
					randomAttrs(r)...,
				))
			}
			res.ScopeLogs = append(res.ScopeLogs, sc)
		}
		nested.ResourceLogs = append(nested.ResourceLogs, res)
	}

	return nested
}

// randomAttrs picks up to 3 attributes from a pool of 3 names, so names collide inside one set
// and across resource, scope and entry. It can also pick none, which is the case where ToStream
// copies entries as they are.
func randomAttrs(r *rand.Rand) []push.LabelAdapter {
	names := []string{"host", "scope", "shared"}

	var res []push.LabelAdapter
	for range r.Intn(4) {
		name := names[r.Intn(len(names))]
		res = append(res, push.LabelAdapter{Name: name, Value: fmt.Sprintf("%s-%d", name, r.Intn(3))})
	}
	return res
}

func TestToStreamOwnsItsEntriesWhereFlatViewSharesThem(t *testing.T) {
	source := func() *InternalStreamAdapter {
		return FromStream(Stream{Labels: `{app="a"}`, Entries: []push.Entry{
			entry(1, "first"), entry(2, "second"),
		}})
	}

	t.Run("ToStream fills a buffer the caller owns", func(t *testing.T) {
		nested := source()

		var out Stream
		nested.ToStream(&out)
		out.Entries[0].Line = "rewritten"

		require.Equal(t, "first", nested.ResourceLogs[0].ScopeLogs[0].Entries[0].Line,
			"writing to the result reached the stream")
	})

	t.Run("FlatView shares the stream's entries", func(t *testing.T) {
		nested := source()

		view := nested.FlatView()
		nested.ResourceLogs[0].ScopeLogs[0].Entries[0].Line = "rewritten"

		require.Equal(t, "rewritten", view.Entries[0].Line, "the view holds entries of its own")
	})

	t.Run("FlatView materialises a stream whose groups hold attributes", func(t *testing.T) {
		nested := InternalStreamAdapter{
			Labels: `{app="b"}`,
			ResourceLogs: []ResourceLogs{resource(attrs("service.name", "checkout"),
				scope(nil, entry(1, "first")))},
		}

		view := nested.FlatView()

		requireSameEntries(t, []push.Entry{entry(1, "first", attrs("service.name", "checkout")...)},
			view.Entries, "the attributes its resource holds are resolved onto it")
		require.Empty(t, nested.ResourceLogs[0].ScopeLogs[0].Entries[0].StructuredMetadata,
			"resolving them wrote back to the stream")
	})
}

func TestEachGroupCarriesTheAttributesThatApplyToItsEntries(t *testing.T) {
	nested := InternalStreamAdapter{
		Labels: `{app="a"}`,
		ResourceLogs: []ResourceLogs{
			resource(attrs("service.name", "checkout"),
				scope(attrs("scope.name", "one"), entry(1, "first"), entry(2, "second")),
				scope(nil, entry(3, "third")),
			),
			resource(nil, scope(nil, entry(4, "fourth"))),
		},
	}

	type visit struct {
		resourceAttrs, scopeAttrs []push.LabelAdapter
		lines                     []string
	}

	var visits []visit
	nested.EachGroup(func(resourceAttrs, scopeAttrs []push.LabelAdapter, entries []push.Entry) {
		lines := make([]string, 0, len(entries))
		for i := range entries {
			lines = append(lines, entries[i].Line)
			entries[i].Line += " rewritten"
		}
		visits = append(visits, visit{resourceAttrs, scopeAttrs, lines})
	})

	require.Equal(t, []visit{
		{attrs("service.name", "checkout"), attrs("scope.name", "one"), []string{"first", "second"}},
		{attrs("service.name", "checkout"), nil, []string{"third"}},
		{nil, nil, []string{"fourth"}},
	}, visits)

	require.Equal(t, "first rewritten", nested.ResourceLogs[0].ScopeLogs[0].Entries[0].Line,
		"the entries handed over are the stream's own")
}

func TestEachEntryWithSharedGivesEveryEntryItsGroupsAttributes(t *testing.T) {
	nested := InternalStreamAdapter{
		Labels: `{app="a"}`,
		ResourceLogs: []ResourceLogs{
			resource(attrs("service.name", "checkout"),
				scope(attrs("scope.name", "one"), entry(1, "first")),
				scope(nil, entry(2, "second")),
			),
			resource(nil, scope(nil, entry(3, "third"))),
		},
	}

	var seen []string
	nested.EachEntryWithShared(func(entry *push.Entry, resourceAttrs, scopeAttrs []push.LabelAdapter) {
		seen = append(seen, fmt.Sprint(entry.Line, " ", resourceAttrs, " ", scopeAttrs))
		entry.Line += " rewritten"
	})

	require.Equal(t, []string{
		fmt.Sprint("first ", attrs("service.name", "checkout"), " ", attrs("scope.name", "one")),
		fmt.Sprint("second ", attrs("service.name", "checkout"), " ", []push.LabelAdapter(nil)),
		fmt.Sprint("third ", []push.LabelAdapter(nil), " ", []push.LabelAdapter(nil)),
	}, seen)

	require.Equal(t, "second rewritten", nested.ResourceLogs[0].ScopeLogs[1].Entries[0].Line,
		"the entry handed over is the stream's own")
}

func entry(ns int64, line string, md ...push.LabelAdapter) push.Entry {
	return push.Entry{
		Timestamp:          time.Unix(0, ns),
		Line:               line,
		StructuredMetadata: md,
	}
}

func resource(attrs []push.LabelAdapter, scopes ...ScopeLogs) ResourceLogs {
	return ResourceLogs{
		Attrs:     attrs,
		ScopeLogs: scopes,
	}
}

func attrs(keyValues ...string) []push.LabelAdapter {
	if len(keyValues)%2 != 0 {
		panic("odd number of keyValues")
	}

	res := make([]push.LabelAdapter, 0, len(keyValues)/2)

	for i := 0; i < len(keyValues); i += 2 {
		res = append(res, push.LabelAdapter{Name: keyValues[i], Value: keyValues[i+1]})
	}

	return res
}

func scope(attrs []push.LabelAdapter, entries ...push.Entry) ScopeLogs {
	return ScopeLogs{
		Attrs:   attrs,
		Entries: entries,
	}
}
