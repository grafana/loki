package format

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTreeFormatter(t *testing.T) {
	f := NewTreeFormatter()

	// Build a toy file system tree
	dirF := f.WriteNode(Node{
		Singletons: []string{"Directory"},
		Tuples: []ContentTuple{{
			Key:   "path",
			Value: SingleContent("/home/user"),
		}},
	})

	// Add some files
	docsF := dirF.WriteNode(Node{
		Singletons: []string{"Directory"},
		Tuples: []ContentTuple{{
			Key:   "path",
			Value: SingleContent("documents"),
		}, {
			Key:   "files",
			Value: ListContent{SingleContent("report.pdf"), SingleContent("notes.txt")},
		}},
	})

	// Add a subdirectory with files
	docsF.WriteNode(Node{
		Singletons: []string{"Directory"},
		Tuples: []ContentTuple{{
			Key:   "path",
			Value: SingleContent("images"),
		}, {
			Key:   "files",
			Value: ListContent{SingleContent("photo1.jpg"), SingleContent("photo2.jpg")},
		}},
	})

	// Add another top-level directory
	dirF.WriteNode(Node{
		Singletons: []string{"Directory"},
		Tuples: []ContentTuple{{
			Key:   "path",
			Value: SingleContent("downloads"),
		}, {
			Key:   "files",
			Value: ListContent{SingleContent("movie.mp4")},
		}},
	})

	expected := `Directory path=/home/user
├── Directory path=documents files=(report.pdf, notes.txt)
│   └── Directory path=images files=(photo1.jpg, photo2.jpg)
└── Directory path=downloads files=(movie.mp4)`

	require.Equal(t, expected, f.Format())
}
