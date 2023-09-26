package uploads

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type mockIndex struct {
	*os.File
}

func newMockIndex(t *testing.T, path string) *mockIndex {
	fl, err := os.Create(path)
	require.NoError(t, err)
	return &mockIndex{fl}
}

func (m *mockIndex) Name() string {
	return filepath.Base(m.File.Name())
}

func (m *mockIndex) Path() string {
	return m.File.Name()
}

func (m *mockIndex) Reader() (io.ReadSeeker, error) {
	return m.File, nil
}

func buildTestIndexes(t *testing.T, path string, numIndexes int) map[string]*mockIndex {
	testIndexes := make(map[string]*mockIndex)
	for i := 0; i < numIndexes; i++ {
		fileName := fmt.Sprintf("index-%d", i)
		indexPath := filepath.Join(path, fileName)

		index := newMockIndex(t, indexPath)
		_, err := index.WriteString(fileName)
		require.NoError(t, err)

		testIndexes[indexPath] = index
	}

	return testIndexes
}
