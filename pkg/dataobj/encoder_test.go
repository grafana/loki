package dataobj

import (
	"fmt"
	"io"
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/scratch"
)

func Test_encoder_typeRefs(t *testing.T) {
	tt := []struct {
		name  string
		input SectionType

		expectRef     uint32
		expectNameRef *filemd.SectionType_NameRef
	}{
		{
			name:  "invalid",
			input: SectionType{},

			expectRef:     0,
			expectNameRef: nil,
		},
		{
			name: "streams",
			input: SectionType{
				Namespace: "github.com/grafana/loki",
				Kind:      "streams",
			},

			expectRef:     1,
			expectNameRef: &filemd.SectionType_NameRef{NamespaceRef: 1, KindRef: 2},
		},
		{
			name: "logs",
			input: SectionType{
				Namespace: "github.com/grafana/loki",
				Kind:      "logs",
			},

			expectRef:     2,
			expectNameRef: &filemd.SectionType_NameRef{NamespaceRef: 1, KindRef: 3},
		},
		{
			name: "existing namespace, new kind",
			input: SectionType{
				Namespace: "github.com/grafana/loki",
				Kind:      "section-kind-1",
			},

			expectRef:     3,
			expectNameRef: &filemd.SectionType_NameRef{NamespaceRef: 1, KindRef: 4},
		},
		{
			name: "existing namespace, existing kind",
			input: SectionType{
				Namespace: "github.com/grafana/loki",
				Kind:      "section-kind-1",
			},

			expectRef:     3,
			expectNameRef: &filemd.SectionType_NameRef{NamespaceRef: 1, KindRef: 4},
		},
		{
			name: "new namespace, existing kind",
			input: SectionType{
				Namespace: "new-namespace",
				Kind:      "section-kind-1",
			},

			expectRef:     4,
			expectNameRef: &filemd.SectionType_NameRef{NamespaceRef: 5, KindRef: 4},
		},
		{
			name: "new namespace, new kind",
			input: SectionType{
				Namespace: "new-namespace-2",
				Kind:      "section-kind-2",
			},

			expectRef:     5,
			expectNameRef: &filemd.SectionType_NameRef{NamespaceRef: 6, KindRef: 7},
		},
		{
			name: "existing type, new version",
			input: SectionType{
				Namespace: "github.com/grafana/loki",
				Kind:      "streams",
				Version:   math.MaxUint32,
			},

			expectRef:     6,
			expectNameRef: &filemd.SectionType_NameRef{NamespaceRef: 1, KindRef: 2},
		},
	}

	enc := newEncoder(scratch.NewMemory(), nil, nil)

	// Test are run sequentially so we can check the behaviour of streaming types
	// in.
	for _, tc := range tt {
		typeRef := enc.getTypeRef(tc.input)
		nameRef := enc.rawTypes[typeRef].NameRef

		assert.Equal(t, tc.expectRef, typeRef, "unexpected type ref for %s", tc.name)
		assert.Equal(t, tc.expectNameRef, nameRef, "unexpected name ref for %s", tc.name)
	}

}

func TestBuilder_SectionOrder(t *testing.T) {
	var (
		logsType               = SectionType{Namespace: "github.com/grafana/loki", Kind: "logs"}
		streamsType            = SectionType{Namespace: "github.com/grafana/loki", Kind: "streams", Version: 2}
		unversionedStreamsType = SectionType{Namespace: "github.com/grafana/loki", Kind: "streams"}
		statsType              = SectionType{Namespace: "github.com/grafana/loki", Kind: "stats"}
	)

	type testSection struct {
		typ            SectionType
		data, metadata string
	}

	newSection := func(typ SectionType, name string) testSection {
		return testSection{typ: typ, data: name + " data", metadata: name + " metadata"}
	}

	// The streams section is appended last, as logsobj.Builder does when logs
	// sections fill before Flush.
	appended := []testSection{
		newSection(logsType, "logs-1"),
		newSection(statsType, "stats"),
		newSection(logsType, "logs-2"),
		newSection(streamsType, "streams"),
	}

	flush := func(t *testing.T, appended []testSection, opts ...BuilderOption) *Object {
		t.Helper()

		b := NewBuilder(nil, opts...)
		for _, sec := range appended {
			w := builderSectionWriter{typ: sec.typ, enc: b.encoder}
			_, err := w.WriteSection(nil, []byte(sec.data), []byte(sec.metadata))
			require.NoError(t, err)
		}

		obj, closer, err := b.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })
		return obj
	}

	readAll := func(t *testing.T, open func() (io.ReadCloser, error)) string {
		t.Helper()
		rc, err := open()
		require.NoError(t, err)
		b, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		return string(b)
	}

	// requireOrder checks that obj lists the sections of appended in the order
	// of the indexes in want. It also checks that the metadata regions, then
	// the data regions, are contiguous in that order.
	requireOrder := func(t *testing.T, obj *Object, appended []testSection, want []int) {
		t.Helper()

		require.Len(t, obj.Sections(), len(want))
		for i, sec := range obj.Sections() {
			expect := appended[want[i]]
			require.Equal(t, expect.typ, sec.Type, "section %d has the wrong type", i)
			require.Equal(t, expect.metadata, readAll(t, func() (io.ReadCloser, error) {
				return sec.Reader.MetadataRange(t.Context(), 0, sec.Reader.MetadataSize())
			}))
			require.Equal(t, expect.data, readAll(t, func() (io.ReadCloser, error) {
				return sec.Reader.DataRange(t.Context(), 0, sec.Reader.DataSize())
			}))
		}

		var regions []*filemd.Region
		for _, sec := range obj.metadata.Sections {
			regions = append(regions, sec.Layout.Metadata)
		}
		for _, sec := range obj.metadata.Sections {
			regions = append(regions, sec.Layout.Data)
		}
		require.Zero(t, regions[0].Offset)
		for i := 1; i < len(regions); i++ {
			prev := regions[i-1]
			require.Equal(t, prev.Offset+prev.Length, regions[i].Offset, "region %d must start where region %d ends", i, i-1)
		}
	}

	t.Run("puts sections of listed types first in list order and matches types without the version", func(t *testing.T) {
		obj := flush(t, appended, WithPrioritySections(unversionedStreamsType, statsType))

		requireOrder(t, obj, appended, []int{3, 1, 0, 2})
	})

	t.Run("keeps the append order when no types are listed", func(t *testing.T) {
		obj := flush(t, appended)

		requireOrder(t, obj, appended, []int{0, 1, 2, 3})
	})

	t.Run("combines several options and uses the first position of a repeated type", func(t *testing.T) {
		obj := flush(t, appended,
			WithPrioritySections(streamsType),
			WithPrioritySections(statsType, streamsType),
		)

		requireOrder(t, obj, appended, []int{3, 1, 0, 2})
	})

	t.Run("keeps the append order within each type for many sections", func(t *testing.T) {
		kinds := []SectionType{logsType, statsType, logsType, streamsType, logsType}

		var many []testSection
		var wantStreams, wantStats, wantLogs []int
		for i := range 24 {
			typ := kinds[i%len(kinds)]
			many = append(many, newSection(typ, fmt.Sprintf("%s-%d", typ.Kind, i)))
			switch typ {
			case streamsType:
				wantStreams = append(wantStreams, i)
			case statsType:
				wantStats = append(wantStats, i)
			default:
				wantLogs = append(wantLogs, i)
			}
		}

		obj := flush(t, many, WithPrioritySections(streamsType, statsType))

		requireOrder(t, obj, many, slices.Concat(wantStreams, wantStats, wantLogs))
	})
}

func TestObject_SizeLeveledCompaction(t *testing.T) {
	t.Run("reports no size-leveled compaction when the compaction info holds only a field of another strategy", func(t *testing.T) {
		// Field 2 of CompactionInfo, wire type varint, value 5.
		otherStrategy := []byte{0x10, 0x05}
		// Field 5 of Metadata, wire type length-delimited.
		raw := append([]byte{0x2a, byte(len(otherStrategy))}, otherStrategy...)

		var md filemd.Metadata
		require.NoError(t, md.Unmarshal(raw))
		require.NotNil(t, md.Compaction)

		_, ok := (&Object{metadata: &md}).SizeLeveledCompaction()
		require.False(t, ok)
	})
}
