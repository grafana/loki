package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/common/model"
	prommodel "github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	// We use snappy-encoded protobufs over http by default.
	contentType = "application/x-protobuf"

	maxErrMsgLen = 1024

	invalidExtraLabelsError = "invalid value for environment variable EXTRA_LABELS. Expected a comma separated list with an even number of entries. "
)

var (
	writeAddress                                                             *url.URL
	username, password, extraLabelsRaw, dropLabelsRaw, tenantID, bearerToken string
	keepStream                                                               bool
	batchSize                                                                int
	s3Clients                                                                map[string]*s3.Client
	extraLabels                                                              model.LabelSet
	dropLabels                                                               []model.LabelName
	skipTlsVerify                                                            bool
	printLogLine                                                             bool
	relabelConfigs                                                           []*relabel.Config
)

func setupArguments() {
	addr := os.Getenv("WRITE_ADDRESS")
	if addr == "" {
		panic(errors.New("required environmental variable WRITE_ADDRESS not present, format: https://<hostname>/loki/api/v1/push"))
	}

	var err error
	writeAddress, err = url.Parse(addr)
	if err != nil {
		panic(err)
	}

	fmt.Println("write address: ", writeAddress.String())

	omitExtraLabelsPrefix := os.Getenv("OMIT_EXTRA_LABELS_PREFIX")
	extraLabelsRaw = os.Getenv("EXTRA_LABELS")
	extraLabels, err = parseExtraLabels(extraLabelsRaw, strings.EqualFold(omitExtraLabelsPrefix, "true"))
	if err != nil {
		panic(err)
	}

	dropLabels, err = getDropLabels()
	if err != nil {
		panic(err)
	}

	username = os.Getenv("USERNAME")
	password = os.Getenv("PASSWORD")
	// If either username or password is set then both must be.
	if (username != "" && password == "") || (username == "" && password != "") {
		panic("both username and password must be set if either one is set")
	}

	bearerToken = os.Getenv("BEARER_TOKEN")
	// If username and password are set, bearer token is not allowed
	if username != "" && bearerToken != "" {
		panic("both username and bearerToken are not allowed")
	}

	skipTls := os.Getenv("SKIP_TLS_VERIFY")
	// Anything other than case-insensitive 'true' is treated as 'false'.
	if strings.EqualFold(skipTls, "true") {
		skipTlsVerify = true
	}

	tenantID = os.Getenv("TENANT_ID")

	keep := os.Getenv("KEEP_STREAM")
	// Anything other than case-insensitive 'true' is treated as 'false'.
	if strings.EqualFold(keep, "true") {
		keepStream = true
	}
	fmt.Println("keep stream: ", keepStream)

	batch := os.Getenv("BATCH_SIZE")
	batchSize = 131072
	if batch != "" {
		batchSize, _ = strconv.Atoi(batch)
	}

	print := os.Getenv("PRINT_LOG_LINE")
	printLogLine = true
	if strings.EqualFold(print, "false") {
		printLogLine = false
	}
	s3Clients = make(map[string]*s3.Client)

	// Parse relabel configs from environment variable
	if relabelConfigsRaw := os.Getenv("RELABEL_CONFIGS"); relabelConfigsRaw != "" {
		if err := json.Unmarshal([]byte(relabelConfigsRaw), &relabelConfigs); err != nil {
			panic(fmt.Errorf("failed to parse RELABEL_CONFIGS: %v", err))
		}
	}
}

func parseExtraLabels(extraLabelsRaw string, omitPrefix bool) (model.LabelSet, error) {
	prefix := "__extra_"
	if omitPrefix {
		prefix = ""
	}
	extractedLabels := model.LabelSet{}
	extraLabelsSplit := strings.Split(extraLabelsRaw, ",")

	if len(extraLabelsRaw) < 1 {
		return extractedLabels, nil
	}

	if len(extraLabelsSplit)%2 != 0 {
		return nil, fmt.Errorf(invalidExtraLabelsError)
	}
	for i := 0; i < len(extraLabelsSplit); i += 2 {
		extractedLabels[model.LabelName(prefix+extraLabelsSplit[i])] = model.LabelValue(extraLabelsSplit[i+1])
	}
	err := extractedLabels.Validate()
	if err != nil {
		return nil, err
	}
	fmt.Println("extra labels:", extractedLabels)
	return extractedLabels, nil
}

func getDropLabels() ([]model.LabelName, error) {
	var result []model.LabelName

	if dropLabelsRaw = os.Getenv("DROP_LABELS"); dropLabelsRaw != "" {
		dropLabelsRawSplit := strings.Split(dropLabelsRaw, ",")
		for _, dropLabelRaw := range dropLabelsRawSplit {
			dropLabel := model.LabelName(dropLabelRaw)
			if !dropLabel.IsValid() {
				return []model.LabelName{}, fmt.Errorf("invalid label name %s", dropLabelRaw)
			}
			result = append(result, dropLabel)
		}
	}

	return result, nil
}

func applyRelabelConfigs(labels model.LabelSet) model.LabelSet {
	if len(relabelConfigs) == 0 {
		return labels
	}

	// Convert model.LabelSet to prommodel.Labels
	promLabels := make([]prommodel.Label, 0, len(labels))
	for name, value := range labels {
		promLabels = append(promLabels, prommodel.Label{
			Name:  string(name),
			Value: string(value),
		})
	}

	// Sort labels as required by Process
	promLabels = prommodel.New(promLabels...)

	// Apply relabeling
	processedLabels, keep := relabel.Process(promLabels, relabelConfigs...)
	if !keep {
		return model.LabelSet{}
	}

	// Convert back to model.LabelSet
	result := make(model.LabelSet)
	for _, l := range processedLabels {
		result[model.LabelName(l.Name)] = model.LabelValue(l.Value)
	}

	return result
}

func applyLabels(labels model.LabelSet) model.LabelSet {
	finalLabels := labels.Merge(extraLabels)

	for _, dropLabel := range dropLabels {
		delete(finalLabels, dropLabel)
	}

	// Apply relabeling after merging extra labels and dropping labels
	finalLabels = applyRelabelConfigs(finalLabels)

	// Skip entries with no labels after relabeling
	if len(finalLabels) == 0 {
		return nil
	}

	return finalLabels
}

func checkEventType(ev map[string]interface{}) (interface{}, error) {
	var s3Event events.S3Event
	var s3TestEvent events.S3TestEvent
	var cwEvent events.CloudwatchLogsEvent
	var kinesisEvent events.KinesisEvent
	var sqsEvent events.SQSEvent
	var snsEvent events.SNSEvent
	var eventBridgeEvent events.CloudWatchEvent

	types := [...]interface{}{&s3Event, &s3TestEvent, &cwEvent, &kinesisEvent, &sqsEvent, &snsEvent, &eventBridgeEvent}

	j, _ := json.Marshal(ev)
	reader := strings.NewReader(string(j))
	d := json.NewDecoder(reader)
	d.DisallowUnknownFields()

	for _, t := range types {
		err := d.Decode(t)

		if err == nil {
			return t, nil
		}

		reader.Seek(0, 0)
	}

	return nil, fmt.Errorf("unknown event type")
}

func handler(ctx context.Context, ev map[string]interface{}) error {
	lvl, ok := os.LookupEnv("LOG_LEVEL")
	if !ok {
		lvl = "info"
	}
	log := NewLogger(lvl)
	pClient := NewPromtailClient(&promtailClientConfig{
		backoff: &backoff.Config{
			MinBackoff: minBackoff,
			MaxBackoff: maxBackoff,
			MaxRetries: maxRetries,
		},
		http: &httpClientConfig{
			timeout:       timeout,
			skipTlsVerify: skipTlsVerify,
		},
	}, log)

	event, err := checkEventType(ev)
	if err != nil {
		level.Error(*pClient.log).Log("err", fmt.Errorf("invalid event: %s", ev))
		return err
	}

	switch evt := event.(type) {
	case *events.CloudWatchEvent:
		err = processEventBridgeEvent(ctx, evt, pClient, pClient.log, processS3Event)
	case *events.S3Event:
		err = processS3Event(ctx, evt, pClient, pClient.log)
	case *events.CloudwatchLogsEvent:
		err = processCWEvent(ctx, evt, pClient)
	case *events.KinesisEvent:
		err = processKinesisEvent(ctx, evt, pClient)
	case *events.SQSEvent:
		err = processSQSEvent(ctx, evt, handler)
	case *events.SNSEvent:
		err = processSNSEvent(ctx, evt, handler)
	// When setting up S3 Notification on a bucket, a test event is first sent, see: https://docs.aws.amazon.com/AmazonS3/latest/userguide/notification-content-structure.html
	case *events.S3TestEvent:
		return nil
	}

	if err != nil {
		level.Error(*pClient.log).Log("err", fmt.Errorf("error processing event: %v", err))
	}
	return err
}

func main() {
	setupArguments()
	lambda.Start(handler)
}
