package testdata

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type LogGenerator struct {
	f *os.File
	s *bufio.Scanner
}

func (g *LogGenerator) Next() (bool, []byte) {
	if g.s.Scan() {
		return true, g.s.Bytes()
	}
	g.reset()
	return g.s.Scan(), g.s.Bytes()
}

func (g *LogGenerator) Close() {
	if g.f != nil {
		g.f.Close()
	}
	g.f = nil
}

func (g *LogGenerator) reset() {
	_, _ = g.f.Seek(0, 0)
	g.s = bufio.NewScanner(g.f)
}

func NewLogGenerator(t testing.TB, filename string) *LogGenerator {
	t.Helper()
	file, err := os.Open(filename)
	require.NoError(t, err)

	return &LogGenerator{
		f: file,
		s: bufio.NewScanner(file),
	}
}

func Files() []string {
	testdataDir := "./testdata"
	files, err := os.ReadDir(testdataDir)
	if err != nil && !os.IsNotExist(err) {
		if !os.IsNotExist(err) {
			panic(err)
		}
		testdataDir = "../testdata"
		files, err = os.ReadDir(testdataDir)
		if err != nil {
			panic(err)
		}
	}

	var fileNames []string
	for _, file := range files {
		if !file.IsDir() {
			filePath := filepath.Join(testdataDir, file.Name())
			fileNames = append(fileNames, filePath)
		}
	}

	return fileNames
}
