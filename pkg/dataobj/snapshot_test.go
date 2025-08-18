package dataobj

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/scratch"
)

func Test_snapshot(t *testing.T) {
	store := scratch.NewMemory()
	sections := []sectionInfo{
		{
			Type:     SectionType{},
			Data:     store.Put([]byte("example data 1")),
			Metadata: store.Put([]byte("example metadata 1")),

			DataSize:     len("example data 1"),
			MetadataSize: len("example metadata 1"),
		},
		{
			Type:     SectionType{},
			Data:     store.Put([]byte("example data 2")),
			Metadata: store.Put([]byte("example metadata 2")),

			DataSize:     len("example data 2"),
			MetadataSize: len("example metadata 2"),
		},
		{
			Type:     SectionType{},
			Data:     store.Put([]byte("example data 3")),
			Metadata: store.Put([]byte("example metadata 3")),

			DataSize:     len("example data 3"),
			MetadataSize: len("example metadata 3"),
		},
	}

	var (
		header = []byte("example header")
		tailer = []byte("example tailer")
	)

	snapshot, err := newSnapshot(store, header, sections, tailer)
	require.NoError(t, err)

	t.Run("full region", func(t *testing.T) {
		headerData := make([]byte, len(header))
		n, err := snapshot.ReadAt(headerData, 0)
		require.NoError(t, err)
		require.Equal(t, len(headerData), n)
		require.Equal(t, header, headerData)

		// Test reading the first section's data region completely
		sectionData := make([]byte, 14)           // "example data 1" is 14 bytes
		n, err = snapshot.ReadAt(sectionData, 14) // offset after header
		require.NoError(t, err)
		require.Equal(t, 14, n)
		require.Equal(t, "example data 1", string(sectionData))
	})

	t.Run("full section", func(t *testing.T) {
		fullSectionData := make([]byte, sections[0].DataSize)
		n, err := snapshot.ReadAt(fullSectionData, int64(sections[0].DataSize))
		require.NoError(t, err)
		require.Equal(t, sections[0].DataSize, n)
		require.Equal(t, "example data 1", string(fullSectionData))
	})

	t.Run("partial region (start)", func(t *testing.T) {
		expect := "example"
		partialData := make([]byte, len(expect))
		n, err := snapshot.ReadAt(partialData, 0)
		require.NoError(t, err)
		require.Equal(t, len(expect), n)
		require.Equal(t, expect, string(partialData))

	})

	t.Run("partial region (middle)", func(t *testing.T) {
		expect := "data"

		// Read "data" from "example data 1"
		data := make([]byte, len(expect))
		n, err := snapshot.ReadAt(data, int64(len(header)+len("example ")))
		require.NoError(t, err)
		require.Equal(t, len(expect), n)
		require.Equal(t, expect, string(data))
	})

	t.Run("multiple regions", func(t *testing.T) {
		expected := "example data 1example metadata 1"

		// Test reading two entire sections.
		data := make([]byte, len(expected))
		n, err := snapshot.ReadAt(data, int64(len(header)))
		require.NoError(t, err)
		require.Equal(t, len(expected), n)
		require.Equal(t, expected, string(data))
	})

	t.Run("cross region", func(t *testing.T) {
		// Test reading across multiple regions with ReadAt, where we start
		// partially in the starting region and end partially in the ending
		// region.

		// Read from middle of header through start of section 1 data
		expect := " headerexamp" // " header" + "examp" from "example data 1"

		data := make([]byte, len(expect))
		n, err := snapshot.ReadAt(data, int64(len("example"))) // Start after "example" from header
		require.NoError(t, err)
		require.Equal(t, len(expect), n)
		require.Equal(t, expect, string(data))
	})

	t.Run("full range", func(t *testing.T) {
		expect := "example header" +
			"example data 1" + "example metadata 1" +
			"example data 2" + "example metadata 2" +
			"example data 3" + "example metadata 3" +
			"example tailer"

		// Test reading the entire range with ReadAt.
		totalSize := snapshot.Size()
		require.Equal(t, len(expect), int(totalSize))

		data := make([]byte, len(expect))
		n, err := snapshot.ReadAt(data, 0)
		require.NoError(t, err)
		require.Equal(t, len(expect), n)
		require.Equal(t, expect, string(data))
	})

	t.Run("full range (io.SectionReader)", func(t *testing.T) {
		// This tests that our implementation of [io.ReaderAt] is compatible
		// with [io.SectionReader]. If it fails, we likely have a bug fulfilling
		// the contract of [io.ReaderAt].

		expect := "example header" +
			"example data 1" + "example metadata 1" +
			"example data 2" + "example metadata 2" +
			"example data 3" + "example metadata 3" +
			"example tailer"

		totalSize := snapshot.Size()

		sr := io.NewSectionReader(snapshot, 0, totalSize)
		bb, err := io.ReadAll(sr)
		require.NoError(t, err)
		require.Equal(t, expect, string(bb))
	})

	t.Run("at EOF", func(t *testing.T) {
		totalSize := snapshot.Size()

		// Reading at EOF should produce no bytes and an EOF error.
		n, err := snapshot.ReadAt(make([]byte, 1), totalSize)
		require.Zero(t, n, "reading at EOF should produce no bytes")
		require.ErrorIs(t, err, io.EOF, "reading at EOF should produce an EOF error")
	})

	t.Run("beyond EOF", func(t *testing.T) {
		totalSize := snapshot.Size()

		// Reading fully beyond EOF should produce no bytes and an EOF error.
		n, err := snapshot.ReadAt(make([]byte, 10), totalSize+10)
		require.Equal(t, 0, n, "reading beyond EOF should produce no bytes")
		require.ErrorIs(t, err, io.EOF, "reading beyond EOF should produce an EOF error")
	})

	t.Run("through EOF", func(t *testing.T) {
		totalSize := snapshot.Size()

		expect := "tailer" // End of "example tailer"

		// Request more bytes than available should return available data and
		// EOF error.
		data := make([]byte, 20)
		n, err := snapshot.ReadAt(data, totalSize-int64(len(expect)))
		require.Equal(t, len(expect), n, "reading through EOF should return available data")
		require.ErrorIs(t, err, io.EOF, "reading through EOF should still return EOF")
		require.Equal(t, expect, string(data[:n]))
	})

	require.NoError(t, snapshot.Close())

	for _, section := range sections {
		require.ErrorAs(t, store.Remove(section.Data), new(scratch.HandleNotFoundError), "section should be deleted after closing a snapshot")
		require.ErrorAs(t, store.Remove(section.Metadata), new(scratch.HandleNotFoundError), "section should be deleted after closing a snapshot")
	}
}
