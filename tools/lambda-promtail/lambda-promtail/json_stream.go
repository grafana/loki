package main

import (
	"encoding/json"
	"fmt"
	"io"
)

// Stream helps transmit each recordss within a channel.
type Stream struct {
	records chan Record
}

// Represents a Record in the Json Stream. If there is not error, error is nil
type Record struct {
	Error   error
	Content map[string]any
}

// Creates a new Stream of records
func NewJSONStream(recordChan chan Record) Stream {
	return Stream{
		records: recordChan,
	}
}

// Streams the JSON file starting from the target token, record by record.
func (s Stream) Start(r io.ReadCloser, tokenCountToTarget int) {
	defer r.Close()
	defer close(s.records)
	decoder := json.NewDecoder(r)

	// Skip the provided count of JSON tokens to get the the target array, ex: "{" "Record"
	for i := 0; i < tokenCountToTarget; i++ {
		_, err := decoder.Token()
		if err != nil {
			s.records <- Record{Error: fmt.Errorf("failed decoding beginning token: %w", err)}
			return
		}
	}

	// Read the JSON token content
	i := 1
	for decoder.More() {
		var content map[string]any
		if err := decoder.Decode(&content); err != nil {
			s.records <- Record{Error: fmt.Errorf("failed decoding record %d: %w", i, err)}
			return
		}
		s.records <- Record{Content: content}
		i++
	}
}
