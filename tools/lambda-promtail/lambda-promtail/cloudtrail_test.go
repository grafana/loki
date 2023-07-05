package main

import (
	"compress/gzip"
	"os"
	"testing"
)

func TestParseJson(t *testing.T) {
	records := make(chan Record)
	jsonStream := NewJSONStream(records)
	file, err := os.Open("../testdata/cloudtrail-log-file.json.gz")
	if err != nil {
		t.Error(err)
	}
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Error(err)
	}
	go jsonStream.Start(gzipReader, 3)

	for record := range jsonStream.records {
		if record.Error != nil {
			t.Error(record.Error)
		}
		_, err := parseCloudtrailRecord(record)
		if err != nil {
			t.Error(err)
		}
	}
}
