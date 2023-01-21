package main

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"
)

func parseCWEvent(ctx context.Context, b *batch, ev *events.CloudwatchLogsEvent) error {
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

	labels = applyExtraLabels(labels)

	for _, event := range data.LogEvents {
		timestamp := time.UnixMilli(event.Timestamp)

		if err := b.add(ctx, entry{labels, logproto.Entry{
			Line:      event.Message,
			Timestamp: timestamp,
		}}); err != nil {
			return err
		}
	}

	return nil
}

func processCWEvent(ctx context.Context, ev *events.CloudwatchLogsEvent) error {
	batch, err := newBatch(ctx)
	if err != nil {
		return err
	}

	err = parseCWEvent(ctx, batch, ev)
	if err != nil {
		return err
	}

	err = sendToPromtail(ctx, batch)
	if err != nil {
		return err
	}

	return nil
}
