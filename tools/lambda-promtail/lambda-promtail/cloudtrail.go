package main

import (
	"encoding/json"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// Parses a Cloudtrail Record and returns a logproto.Entry
func parseCloudtrailRecord(record Record) (logproto.Entry, error) {
	timestamp := time.Now()
	if record.Error != nil {
		return logproto.Entry{}, record.Error
	}
	document, err := json.Marshal(record.Content)
	if err != nil {
		return logproto.Entry{}, err
	}
	if val, ok := record.Content["eventTime"]; ok {
		time, err := time.Parse(time.RFC3339, val.(string))
		if err != nil {
			return logproto.Entry{}, err
		} else {
			timestamp = time
		}
	}
	return logproto.Entry{
		Line:      string(document),
		Timestamp: timestamp,
	}, nil
}
