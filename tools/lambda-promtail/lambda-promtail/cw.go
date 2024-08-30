package main

import (
	"context"
	"encoding/json"
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
var reOTELCollectorMessage = regexp.MustCompile(`/otel-collector/(?P<syslog_host>[^/]+)`)

const (
	LabelCloudRegion           = "cloud_region"
	LabelDeploymentEnvironment = "deployment_environment"
	LabelServiceName           = "service_name"
	LabelAppConfig             = "lithic_deployment_app_config"
	LabelSource                = "source"
	LabelHost                  = "host"
)

type SyslogAttributes struct {
	Appname             string `json:"appname"`
	Hostname            string `json:"hostname"`
	Job                 string `json:"job"`
	LithicCollectorName string `json:"lithic.collector.name"`
	Message             string `json:"message"`
}

type SyslogMessage struct {
	SeverityText string           `json:"severity_text"`
	Attributes   SyslogAttributes `json:"attributes"`
}

func parseCWEvent(ctx context.Context, b *batch, ev *events.CloudwatchLogsEvent) error {
	data, err := ev.AWSLogs.Parse()
	if err != nil {
		fmt.Println("error parsing log event: ", err)
		return err
	}
	logStream := model.LabelValue(data.LogStream)

	labels := model.LabelSet{
		model.LabelName("cloudwatch_log_group"): model.LabelValue(data.LogGroup),
		model.LabelName("cloudwatch_owner"):     model.LabelValue(data.Owner),
	}

	if keepStream {
		labels[model.LabelName("cloudwatch_log_stream")] = logStream
	}

	if richESCLabels := parseECSTask(data.LogGroup); richESCLabels != nil {
		labels = labels.Merge(richESCLabels)
	}

	isSyslog := false
	if richSyslogLabels := parseSyslogTask(data.LogGroup, data.LogStream); richSyslogLabels != nil {
		isSyslog = true
		labels = labels.Merge(richSyslogLabels)
	}

	for _, event := range data.LogEvents {
		timestamp := time.UnixMilli(event.Timestamp)
		logMsg := event.Message

		if isSyslog {
			if msg := parseSyslogMessage(event.Message); msg != nil {
				labels = labels.Merge(model.LabelSet{
					model.LabelName("appname"):               model.LabelValue(msg.Attributes.Appname),
					model.LabelName("hostname"):              model.LabelValue(msg.Attributes.Hostname),
					model.LabelName("job"):                   model.LabelValue(msg.Attributes.Job),
					model.LabelName("lithic.collector.name"): model.LabelValue(msg.Attributes.LithicCollectorName),
				})
				logMsg = msg.Attributes.Message
			}
		}

		labels = labels.Merge(extraLabels)
		logEntry := entry{labels, logproto.Entry{
			Line:      logMsg,
			Timestamp: timestamp,
		}}

		if err := b.add(ctx, logEntry); err != nil {
			return err
		}
	}

	return nil
}

func parseSyslogMessage(msg string) *SyslogMessage {
	var syslog SyslogMessage
	if err := json.Unmarshal([]byte(msg), &syslog); err != nil {
		fmt.Println("WARN: got error parsing syslog message: ", err)
		return nil
	}
	return &syslog
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

func parseSyslogTask(logGroup, logStream string) model.LabelSet {
	if logStream != "syslog" || !strings.HasPrefix(logGroup, "/otel-collector/") {
		return nil
	}

	var labels = model.LabelSet{}
	for _, re := range []*regexp.Regexp{reOTELCollectorMessage} {
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
