// Copyright 2020 Amazon.com, Inc. or its affiliates. All Rights Reserved.

package events

type KafkaEvent struct {
	EventSource      string                   `json:"eventSource"`
	EventSourceARN   string                   `json:"eventSourceArn"`
	Records          map[string][]KafkaRecord `json:"records"`
	BootstrapServers string                   `json:"bootstrapServers"`
}

type KafkaRecord struct {
	Topic         string                `json:"topic"`
	Partition     int64                 `json:"partition"`
	Offset        int64                 `json:"offset"`
	Timestamp     MilliSecondsEpochTime `json:"timestamp"`
	TimestampType string                `json:"timestampType"`
	Key           string                `json:"key,omitempty"`
	Value         string                `json:"value,omitempty"`
	Headers       []map[string][]byte   `json:"headers"`
}
