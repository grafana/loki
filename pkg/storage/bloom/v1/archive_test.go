package v1

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/chunkenc"
)

func TestArchive(t *testing.T) {
	// for writing files to two dirs for comparison and ensuring they're equal
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	numSeries := 100
	numKeysPerSeries := 10000
	data, _ := mkBasicSeriesWithBlooms(numSeries, numKeysPerSeries, 0, 0xffff, 0, 10000)

	builder, err := NewBlockBuilder(
		BlockOptions{
			schema: Schema{
				version:  DefaultSchemaVersion,
				encoding: chunkenc.EncSnappy,
			},
			SeriesPageSize: 100,
			BloomPageSize:  10 << 10,
		},
		NewDirectoryBlockWriter(dir1),
	)

	require.Nil(t, err)
	itr := NewSliceIter[SeriesWithBloom](data)
	_, err = builder.BuildFrom(itr)
	require.Nil(t, err)

	reader := NewDirectoryBlockReader(dir1)

	w := bytes.NewBuffer(nil)
	require.Nil(t, TarGz(w, reader))

	require.Nil(t, UnTarGz(dir2, w))

	reader2 := NewDirectoryBlockReader(dir2)

	// Check Index is byte for byte equivalent
	srcIndex, err := reader.Index()
	require.Nil(t, err)
	_, err = srcIndex.Seek(0, io.SeekStart)
	require.Nil(t, err)
	dstIndex, err := reader2.Index()
	require.Nil(t, err)
	_, err = dstIndex.Seek(0, io.SeekStart)
	require.Nil(t, err)

	srcIndexBytes, err := io.ReadAll(srcIndex)
	require.Nil(t, err)
	dstIndexBytes, err := io.ReadAll(dstIndex)
	require.Nil(t, err)
	require.Equal(t, srcIndexBytes, dstIndexBytes)

	// Check Blooms is byte for byte equivalent
	srcBlooms, err := reader.Blooms()
	require.Nil(t, err)
	_, err = srcBlooms.Seek(0, io.SeekStart)
	require.Nil(t, err)
	dstBlooms, err := reader2.Blooms()
	require.Nil(t, err)
	_, err = dstBlooms.Seek(0, io.SeekStart)
	require.Nil(t, err)

	srcBloomsBytes, err := io.ReadAll(srcBlooms)
	require.Nil(t, err)
	dstBloomsBytes, err := io.ReadAll(dstBlooms)
	require.Nil(t, err)
	require.Equal(t, srcBloomsBytes, dstBloomsBytes)
}

// Test to validate correctness of the UnGzData function.
// We use the gzip command line tool to compress a file and then
// uncompress the file contents using the UnGzData function
func TestUnGzData(t *testing.T) {
	// Create a temporary file to gzip
	content := []byte("Hello World!")
	filePath := "testfile.txt"
	err := os.WriteFile(filePath, content, 0644)
	require.Nil(t, err)

	defer os.Remove(filePath)

	// Compress the file using the gzip command line tool
	gzipFileName := "testfile.txt.gz"
	cmd := exec.Command("gzip", filePath)
	err = cmd.Run()
	require.Nil(t, err)
	defer os.Remove(gzipFileName)

	// Read the gzipped file using the compress/gzip package
	gzipFile, err := os.Open(gzipFileName)
	require.Nil(t, err)
	defer gzipFile.Close()

	fileContent, err := os.ReadFile(gzipFileName)
	require.Nil(t, err)

	uncompressedContent := UnGzData(fileContent)

	// Check if the uncompressed data matches the original content
	require.Equal(t, uncompressedContent, content)
}

// Test to validate correctness of the GzData function.
// We use the GzData function to compress data, and then write it to a file
// Then we use the gunzip commandline tool to uncompress the file and verify
// the contents match the original data
func TestGzData(t *testing.T) {
	// Create a temporary file to gzip
	content := []byte("Hello World!")
	compressedContent := GzData(content)
	baseFileName := "testfile.txt"
	filePath := "testfile.txt.gz"
	err := os.WriteFile(filePath, compressedContent, 0644)
	require.Nil(t, err)

	defer os.Remove(filePath)

	// Uncompress the file using the gunzip command line tool
	cmd := exec.Command("gunzip", filePath)
	err = cmd.Run()
	require.Nil(t, err)

	defer os.Remove(baseFileName)

	require.Nil(t, err)

	uncompressedContent, err := os.ReadFile(baseFileName)
	require.Nil(t, err)

	// Check if the uncompressed data matches the original content
	require.Equal(t, uncompressedContent, content)
}

// Test to validate that the GzData and UnGzData functions are inverses of each other
func TestGzUnGzData(t *testing.T) {
	testString := []byte("Hello World!")
	compressed := GzData(testString)
	uncompressed := UnGzData(compressed)
	require.Equal(t, testString, uncompressed)
}
