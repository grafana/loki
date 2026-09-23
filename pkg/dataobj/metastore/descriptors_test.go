package metastore

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// sectionDescriptor returns a descriptor for one section of an object, listing the given streams.
func sectionDescriptor(objectPath string, sectionIdx int64, streamIDs ...int64) *DataobjSectionDescriptor {
	return &DataobjSectionDescriptor{
		SectionKey: SectionKey{ObjectPath: objectPath, SectionIdx: sectionIdx},
		StreamIDs:  streamIDs,
	}
}

func TestDataobjSectionDescriptors_ByObject(t *testing.T) {
	tests := map[string]struct {
		descriptors DataobjSectionDescriptors

		// wantSectionsPerObject is the number of sections expected in each object's group.
		wantSectionsPerObject map[string]int
	}{
		"no descriptors group into nothing": {
			descriptors:           nil,
			wantSectionsPerObject: map[string]int{},
		},
		"every section of one object groups together": {
			descriptors: DataobjSectionDescriptors{
				sectionDescriptor("objects/a", 0, 1),
				sectionDescriptor("objects/a", 1, 2),
			},
			wantSectionsPerObject: map[string]int{"objects/a": 2},
		},
		"sections of different objects group apart, however they are interleaved": {
			descriptors: DataobjSectionDescriptors{
				sectionDescriptor("objects/a", 0, 1),
				sectionDescriptor("objects/b", 0, 1),
				sectionDescriptor("objects/a", 1, 2),
				sectionDescriptor("objects/c", 0, 1),
				sectionDescriptor("objects/b", 1, 2),
			},
			wantSectionsPerObject: map[string]int{"objects/a": 2, "objects/b": 2, "objects/c": 1},
		},
		"a repeated section is kept, because grouping does not deduplicate": {
			descriptors: DataobjSectionDescriptors{
				sectionDescriptor("objects/a", 0, 1),
				sectionDescriptor("objects/a", 0, 1),
			},
			wantSectionsPerObject: map[string]int{"objects/a": 2},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			byObject := test.descriptors.ByObject()

			require.Len(t, byObject, len(test.wantSectionsPerObject))
			for path, wantSections := range test.wantSectionsPerObject {
				require.Len(t, byObject[path], wantSections, "sections grouped under %q", path)
				for _, descriptor := range byObject[path] {
					require.Equal(t, path, descriptor.ObjectPath, "a group holds only its own object's sections")
				}
			}
		})
	}
}

func TestDataobjSectionDescriptors_StreamIDs(t *testing.T) {
	tests := map[string]struct {
		descriptors DataobjSectionDescriptors
		want        []int64
	}{
		"no descriptors list no stream": {
			descriptors: nil,
			want:        []int64{},
		},
		"a section with no stream lists none": {
			descriptors: DataobjSectionDescriptors{sectionDescriptor("objects/a", 0)},
			want:        []int64{},
		},
		"one section's streams come back in the order it lists them": {
			descriptors: DataobjSectionDescriptors{sectionDescriptor("objects/a", 0, 3, 1, 2)},
			want:        []int64{3, 1, 2},
		},
		"a stream listed by several sections of the object comes back once": {
			descriptors: DataobjSectionDescriptors{
				sectionDescriptor("objects/a", 0, 1, 2),
				sectionDescriptor("objects/a", 1, 2, 3),
			},
			want: []int64{1, 2, 3},
		},
		"a section listing one stream twice comes back once": {
			descriptors: DataobjSectionDescriptors{sectionDescriptor("objects/a", 0, 1, 1, 2)},
			want:        []int64{1, 2},
		},
		// Each object's builder assigns its own stream IDs, so IDs of different objects name
		// different streams. Pooling them merges those streams, which is why a caller groups by
		// object first and calls this per group.
		"IDs of different objects merge, so a caller must group by object first": {
			descriptors: DataobjSectionDescriptors{
				sectionDescriptor("objects/a", 0, 1, 2),
				sectionDescriptor("objects/b", 0, 1, 2),
			},
			want: []int64{1, 2},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, test.want, test.descriptors.StreamIDs())
		})
	}
}
