package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/model"
)

var reECSAppConfig = regexp.MustCompile(`/ecs/(?P<cloud_region>[^/]+)/(?P<deployment_environment>[^/]+)/(?P<service_name>[^/]+)-(?P<lithic_deployment_app_config>(live|sandbox|dev))`)
var reECSNoAppConfig = regexp.MustCompile(`/ecs/(?P<cloud_region>[^/]+)/(?P<deployment_environment>[^/]+)/(?P<service_name>[^/]+)`)

const (
	LabelCloudRegion           = "cloud_region"
	LabelDeploymentEnvironment = "deployment_environment"
	LabelServiceName           = "service_name"
	LabelAppConfig             = "lithic_deployment_app_config"
)

func parseCWEvent(ctx context.Context, b *batch, ev *events.CloudwatchLogsEvent) error {
	data, err := ev.AWSLogs.Parse()
	if err != nil {
		fmt.Println("error parsing log event: ", err)
		return err
	}

	labels := model.LabelSet{
		model.LabelName("cloudwatch_log_group"): model.LabelValue(data.LogGroup),
		model.LabelName("cloudwatch_owner"):     model.LabelValue(data.Owner),
	}

	if keepStream {
		labels[model.LabelName("cloudwatch_log_stream")] = model.LabelValue(data.LogStream)
	}

	if richLabels := parseECSTask(data.LogGroup); richLabels != nil {
		labels = labels.Merge(richLabels)
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

func parseECSTask(logGroup string) model.LabelSet {
	if !strings.HasPrefix(logGroup, "/ecs") {
		return nil
	}

	var labels = model.LabelSet{}
	for _, re := range []*regexp.Regexp{reECSAppConfig, reECSNoAppConfig} {
		match := re.FindStringSubmatch(logGroup)
		if match != nil {
			for i, name := range re.SubexpNames() {
				if i != 0 && name != "" {
					labels[model.LabelName(name)] = model.LabelValue(match[i])
				}
			}
			return labels
		}
	}
	return labels
}
