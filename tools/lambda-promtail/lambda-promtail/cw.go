package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/model"
)

func checkIfCWEvent(ev map[string]interface{}) (events.CloudwatchLogsEvent, error) {
	var cw events.CloudwatchLogsEvent

	j, _ := json.Marshal(ev)

	d := json.NewDecoder(strings.NewReader(string(j)))
	d.DisallowUnknownFields()

	err := d.Decode(&cw)

	return cw, err
}

func createCWStream(ctx context.Context, b *batch, ev events.CloudwatchLogsEvent) error {
	data, err := ev.AWSLogs.Parse()
	if err != nil {
		fmt.Println("error parsing log event: ", err)
		return err
	}

	labels := model.LabelSet{
		model.LabelName("__aws_cloudwatch_log_group"): model.LabelValue(data.LogGroup),
		model.LabelName("__aws_cloudwatch_owner"):     model.LabelValue(data.Owner),
	}
	if keepStream {
		labels[model.LabelName("__aws_cloudwatch_log_stream")] = model.LabelValue(data.LogStream)
	}

	for _, event := range data.LogEvents {
		timestamp := time.UnixMilli(event.Timestamp)

		b.add(entry{labels, logproto.Entry{
			Line:      event.Message,
			Timestamp: timestamp,
		}})
	}

	return nil
}

func processCW(ctx context.Context, ev events.CloudwatchLogsEvent) error {
	batch := newBatch()

	err := createCWStream(ctx, batch, ev)
	if err != nil {
		return err
	}

	err = sendToPromtail(ctx, batch)
	if err != nil {
		return err
	}
	return nil
}
