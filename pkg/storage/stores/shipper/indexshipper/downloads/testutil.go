package downloads

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

type mockIndex struct {
	*os.File
}

func openMockIndexFile(t *testing.T, path string) *mockIndex {
	fl, err := os.Open(path)
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

func setupIndexesAtPath(t *testing.T, userID, path string, start, end int) []string {
	require.NoError(t, os.MkdirAll(path, 0750))
	var testIndexes []string
	for ; start < end; start++ {
		fileName := buildIndexFilename(userID, start)
		indexPath := filepath.Join(path, fileName)

		require.NoError(t, os.WriteFile(indexPath, []byte(fileName), 0640)) // #nosec G306 -- this is fencing off the "other" permissions
		testIndexes = append(testIndexes, indexPath)
	}

	return testIndexes
}

func buildIndexFilename(userID string, indexNum int) string {
	if userID == "" {
		return strconv.Itoa(indexNum)
	}

	return userID + "-" + strconv.Itoa(indexNum)
}
