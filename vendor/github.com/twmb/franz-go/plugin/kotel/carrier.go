package kotel

import (
	"github.com/twmb/franz-go/pkg/kgo"
)

// RecordCarrier injects and extracts traces from a kgo.Record.
//
// This type exists to satisfy the otel/propagation.TextMapCarrier interface.
type RecordCarrier struct {
	record *kgo.Record
}

// NewRecordCarrier creates a new RecordCarrier.
func NewRecordCarrier(record *kgo.Record) RecordCarrier {
	return RecordCarrier{record: record}
}

// Get retrieves a single value for a given key if it exists.
func (c RecordCarrier) Get(key string) string {
	for _, h := range c.record.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

// Set sets a header.
func (c RecordCarrier) Set(key, val string) {
	// Check if key already exists.
	for i, h := range c.record.Headers {
		if h.Key == key {
			// Key exist, update the value.
			c.record.Headers[i].Value = []byte(val)
			return
		}
	}
	// Key does not exist, append new header.
	c.record.Headers = append(c.record.Headers, kgo.RecordHeader{
		Key:   key,
		Value: []byte(val),
	})
}

// Keys returns a slice of all key identifiers in the carrier.
func (c RecordCarrier) Keys() []string {
	out := make([]string, len(c.record.Headers))
	for i, h := range c.record.Headers {
		out[i] = h.Key
	}
	return out
}
