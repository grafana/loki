package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/logging"

	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/promtail/client"
	lokiflag "github.com/grafana/loki/pkg/util/flagext"
)

var defaultClientCfg = client.Config{}

func init() {
	// Init everything with default values.
	flagext.RegisterFlags(&defaultClientCfg)
}

type ConfigGetter interface {
	Get(key string) string
}

type format int

const (
	jsonFormat format = iota
	kvPairFormat
)

const (
	falseStr = "false"
	trueStr  = "true"
)

type config struct {
	clientConfig         client.Config
	bufferConfig         bufferConfig
	logLevel             logging.Level
	autoKubernetesLabels bool
	removeKeys           []string
	labelKeys            []string
	lineFormat           format
	dropSingleKey        bool
	labelMap             map[string]interface{}
}

func parseConfig(cfg ConfigGetter) (*config, error) {
	res := &config{}

	res.clientConfig = defaultClientCfg
	res.bufferConfig = defaultBufferConfig

	url := cfg.Get("URL")
	var clientURL flagext.URLValue
	if url == "" {
		url = "http://localhost:3100/loki/api/v1/push"
	}
	err := clientURL.Set(url)
	if err != nil {
		return nil, errors.New("failed to parse client URL")
	}
	res.clientConfig.URL = clientURL

	// cfg.Get will return empty string if not set, which is handled by the client library as no tenant
	res.clientConfig.TenantID = cfg.Get("TenantID")

	batchWait := cfg.Get("BatchWait")
	if batchWait != "" {
		batchWaitValue, err := time.ParseDuration(batchWait)
		if err != nil {
			return nil, fmt.Errorf("failed to parse BatchWait: %s", batchWait)
		}
		res.clientConfig.BatchWait = batchWaitValue
	}

	batchSize := cfg.Get("BatchSize")
	if batchSize != "" {
		batchSizeValue, err := strconv.Atoi(batchSize)
		if err != nil {
			return nil, fmt.Errorf("failed to parse BatchSize: %s", batchSize)
		}
		res.clientConfig.BatchSize = batchSizeValue
	}

	timeout := cfg.Get("Timeout")
	if timeout != "" {
		timeoutValue, err := time.ParseDuration(timeout)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Timeout: %s", timeout)
		}
		res.clientConfig.Timeout = timeoutValue
	}

	minBackoff := cfg.Get("MinBackoff")
	if minBackoff != "" {
		minBackoffValue, err := time.ParseDuration(minBackoff)
		if err != nil {
			return nil, fmt.Errorf("failed to parse MinBackoff: %s", minBackoff)
		}
		res.clientConfig.BackoffConfig.MinBackoff = minBackoffValue
	}

	maxBackoff := cfg.Get("MaxBackoff")
	if maxBackoff != "" {
		maxBackoffValue, err := time.ParseDuration(maxBackoff)
		if err != nil {
			return nil, fmt.Errorf("failed to parse MaxBackoff: %s", maxBackoff)
		}
		res.clientConfig.BackoffConfig.MaxBackoff = maxBackoffValue
	}

	maxRetries := cfg.Get("MaxRetries")
	if maxRetries != "" {
		maxRetriesValue, err := strconv.Atoi(maxRetries)
		if err != nil {
			return nil, fmt.Errorf("failed to parse MaxRetries: %s", maxRetries)
		}
		res.clientConfig.BackoffConfig.MaxRetries = maxRetriesValue
	}

	labels := cfg.Get("Labels")
	if labels == "" {
		labels = `{job="fluent-bit"}`
	}
	matchers, err := logql.ParseMatchers(labels)
	if err != nil {
		return nil, err
	}
	labelSet := make(model.LabelSet)
	for _, m := range matchers {
		labelSet[model.LabelName(m.Name)] = model.LabelValue(m.Value)
	}
	res.clientConfig.ExternalLabels = lokiflag.LabelSet{LabelSet: labelSet}

	logLevel := cfg.Get("LogLevel")
	if logLevel == "" {
		logLevel = "info"
	}
	var level logging.Level
	if err := level.Set(logLevel); err != nil {
		return nil, fmt.Errorf("invalid log level: %v", logLevel)
	}
	res.logLevel = level

	autoKubernetesLabels := cfg.Get("AutoKubernetesLabels")
	switch autoKubernetesLabels {
	case falseStr, "":
		res.autoKubernetesLabels = false
	case trueStr:
		res.autoKubernetesLabels = true
	default:
		return nil, fmt.Errorf("invalid boolean AutoKubernetesLabels: %v", autoKubernetesLabels)
	}

	removeKey := cfg.Get("RemoveKeys")
	if removeKey != "" {
		res.removeKeys = strings.Split(removeKey, ",")
	}

	labelKeys := cfg.Get("LabelKeys")
	if labelKeys != "" {
		res.labelKeys = strings.Split(labelKeys, ",")
	}

	dropSingleKey := cfg.Get("DropSingleKey")
	switch dropSingleKey {
	case falseStr:
		res.dropSingleKey = false
	case trueStr, "":
		res.dropSingleKey = true
	default:
		return nil, fmt.Errorf("invalid boolean DropSingleKey: %v", dropSingleKey)
	}

	lineFormat := cfg.Get("LineFormat")
	switch lineFormat {
	case "json", "":
		res.lineFormat = jsonFormat
	case "key_value":
		res.lineFormat = kvPairFormat
	default:
		return nil, fmt.Errorf("invalid format: %s", lineFormat)
	}

	labelMapPath := cfg.Get("LabelMapPath")
	if labelMapPath != "" {
		content, err := ioutil.ReadFile(labelMapPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open LabelMap file: %s", err)
		}
		if err := json.Unmarshal(content, &res.labelMap); err != nil {
			return nil, fmt.Errorf("failed to Unmarshal LabelMap file: %s", err)
		}
		res.labelKeys = nil
	}

	// enable loki plugin buffering
	buffer := cfg.Get("Buffer")
	switch buffer {
	case falseStr, "":
		res.bufferConfig.buffer = false
	case trueStr:
		res.bufferConfig.buffer = true
	default:
		return nil, fmt.Errorf("invalid boolean Buffer: %v", buffer)
	}

	// buffering type
	bufferType := cfg.Get("BufferType")
	if bufferType != "" {
		res.bufferConfig.bufferType = bufferType
	}

	// dque directory
	queueDir := cfg.Get("DqueDir")
	if queueDir != "" {
		res.bufferConfig.dqueConfig.queueDir = queueDir
	}

	// dque segment size (queueEntry unit)
	queueSegmentSize := cfg.Get("DqueSegmentSize")
	if queueSegmentSize != "" {
		res.bufferConfig.dqueConfig.queueSegmentSize, err = strconv.Atoi(queueSegmentSize)
		if err != nil {
			return nil, fmt.Errorf("impossible to convert string to integer DqueSegmentSize: %v", queueSegmentSize)
		}
	}

	// dque control file change sync to disk as they happen aka dque.turbo mode
	queueSync := cfg.Get("DqueSync")
	switch queueSync {
	case "normal", "":
		res.bufferConfig.dqueConfig.queueSync = false
	case "full":
		res.bufferConfig.dqueConfig.queueSync = true
	default:
		return nil, fmt.Errorf("invalid string queueSync: %v", queueSync)
	}

	// dque name
	queueName := cfg.Get("DqueName")
	if queueName != "" {
		res.bufferConfig.dqueConfig.queueName = queueName
	}

	return res, nil
}
