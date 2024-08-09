package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func parseKinesisEvent(ctx context.Context, b *batch, ev *events.KinesisEvent) error {
	if ev == nil {
		return nil
	}

	var data []byte
	var recordData events.CloudwatchLogsData
	var err error

	for _, record := range ev.Records {
		if isGzipped(record.Kinesis.Data) {
			data, err = ungzipData(record.Kinesis.Data)
			if err != nil {
				log.Printf("Error decompressing data: %v", err)
			}
		} else {
			data = record.Kinesis.Data
		}

		recordData, err = unmarshalData(data)
		if err != nil {
			log.Printf("Error unmarshalling data: %v", err)
		}

		labels := createLabels(record, recordData)

		if err := processLogEvents(ctx, b, recordData.LogEvents, labels); err != nil {
			return err
		}
	}

	return nil
}

func processKinesisEvent(ctx context.Context, ev *events.KinesisEvent, pClient Client) error {
	batch, _ := newBatch(ctx, pClient)

	err := parseKinesisEvent(ctx, batch, ev)
	if err != nil {
		return err
	}

	err = pClient.sendToPromtail(ctx, batch)
	if err != nil {
		return err
	}
	return nil
}

// isGzipped checks if the input data is gzipped
func isGzipped(data []byte) bool {
	return len(data) >= 2 && data[0] == 0x1F && data[1] == 0x8B
}

// unzipData decompress the gzipped data
func ungzipData(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

func unmarshalData(data []byte) (events.CloudwatchLogsData, error) {
	var recordData events.CloudwatchLogsData
	err := json.Unmarshal(data, &recordData)
	return recordData, err
}

func createLabels(record events.KinesisEventRecord, recordData events.CloudwatchLogsData) model.LabelSet {
	labels := model.LabelSet{
		model.LabelName("__aws_log_type"):                 model.LabelValue("kinesis"),
		model.LabelName("__aws_kinesis_event_source_arn"): model.LabelValue(record.EventSourceArn),
		model.LabelName("__aws_cloudwatch_log_group"):     model.LabelValue(recordData.LogGroup),
		model.LabelName("__aws_cloudwatch_owner"):         model.LabelValue(recordData.Owner),
	}

	if keepStream {
		labels[model.LabelName("__aws_cloudwatch_log_stream")] = model.LabelValue(recordData.LogStream)
	}

	return applyLabels(labels)
}

func processLogEvents(ctx context.Context, b *batch, logEvents []events.CloudwatchLogsLogEvent, labels model.LabelSet) error {
	for _, logEvent := range logEvents {
		timestamp := time.UnixMilli(logEvent.Timestamp)

		if err := b.add(ctx, entry{labels, logproto.Entry{
			Line:      logEvent.Message,
			Timestamp: timestamp,
		}}); err != nil {
			return err
		}
	}

	return nil
}
