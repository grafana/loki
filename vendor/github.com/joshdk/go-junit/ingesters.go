// Copyright Josh Komoroske. All rights reserved.
// Use of this source code is governed by the MIT license,
// a copy of which can be found in the LICENSE.txt file.

package junit

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// IngestDir will search the given directory for XML files and return a slice
// of all contained JUnit test suite definitions.
func IngestDir(directory string) ([]Suite, error) {
	var filenames []string

	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Add all regular files that end with ".xml"
		if info.Mode().IsRegular() && strings.HasSuffix(info.Name(), ".xml") {
			filenames = append(filenames, path)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return IngestFiles(filenames)
}

// IngestFiles will parse the given XML files and return a slice of all
// contained JUnit test suite definitions.
func IngestFiles(filenames []string) ([]Suite, error) {
	var all = make([]Suite, 0)

	for _, filename := range filenames {
		suites, err := IngestFile(filename)
		if err != nil {
			return nil, err
		}
		all = append(all, suites...)
	}

	return all, nil
}

// IngestFile will parse the given XML file and return a slice of all contained
// JUnit test suite definitions.
func IngestFile(filename string) ([]Suite, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return IngestReader(file)
}

// IngestReader will parse the given XML reader and return a slice of all
// contained JUnit test suite definitions.
func IngestReader(reader io.Reader) ([]Suite, error) {
	var (
		suiteChan = make(chan Suite)
		suites    = make([]Suite, 0)
	)

	nodes, err := parse(reader)
	if err != nil {
		return nil, err
	}

	go func() {
		findSuites(nodes, suiteChan)
		close(suiteChan)
	}()

	for suite := range suiteChan {
		suites = append(suites, suite)
	}

	return suites, nil
}

// Ingest will parse the given XML data and return a slice of all contained
// JUnit test suite definitions.
func Ingest(data []byte) ([]Suite, error) {
	return IngestReader(bytes.NewReader(data))
}
