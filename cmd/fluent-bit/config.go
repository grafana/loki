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
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/promtail/client"
	lokiflag "github.com/grafana/loki/pkg/util/flagext"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/logging"
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

type config struct {
	clientConfig  client.Config
	logLevel      logging.Level
	removeKeys    []string
	labelKeys     []string
	lineFormat    format
	dropSingleKey bool
	labeMap       map[string]interface{}
}

func parseConfig(cfg ConfigGetter) (*config, error) {
	res := &config{}

	res.clientConfig = defaultClientCfg

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

	batchWait := cfg.Get("BatchWait")
	if batchWait != "" {
		batchWaitValue, err := strconv.Atoi(batchWait)
		if err != nil {
			return nil, fmt.Errorf("failed to parse BatchWait: %s", batchWait)
		}
		res.clientConfig.BatchWait = time.Duration(batchWaitValue) * time.Second
	}

	batchSize := cfg.Get("BatchSize")
	if batchSize != "" {
		batchSizeValue, err := strconv.Atoi(batchSize)
		if err != nil {
			return nil, fmt.Errorf("failed to parse BatchSize: %s", batchSize)
		}
		res.clientConfig.BatchSize = batchSizeValue
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
	case "false":
		res.dropSingleKey = false
	case "true", "":
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
		if err := json.Unmarshal(content, &res.labeMap); err != nil {
			return nil, fmt.Errorf("failed to Unmarshal LabelMap file: %s", err)
		}
		res.labelKeys = nil
	}
	return res, nil
}
