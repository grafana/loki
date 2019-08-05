package main

import "github.com/cortexproject/cortex/pkg/util/flagext"
import "github.com/prometheus/common/model"

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

type lokiConfig struct {
	url       flagext.URLValue
	batchWait time.Duration
	batchSize int
	labelSet  model.LabelSet
}

type labelSetJSON struct {
	Labels []struct {
		Key   string `json:"key"`
		Label string `json:"label"`
	} `json:"labels"`
}

func getLokiConfig(url string, batchWait string, batchSize string, labels string) (*lokiConfig, error) {
	lc := &lokiConfig{}
	var clientURL flagext.URLValue
	if url == "" {
		url = "http://localhost:3100/api/prom/push"
	}
	err := clientURL.Set(url)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse client URL")
	}
	lc.url = clientURL

	batchWaitValue, err := strconv.Atoi(batchWait)
	if err != nil || batchWait == "" {
		batchWaitValue = 10
	}
	lc.batchWait = time.Duration(batchWaitValue) * time.Millisecond

	batchSizeValue, err := strconv.Atoi(batchSize)
	if err != nil || batchSize == "" {
		batchSizeValue = 10
	}
	lc.batchSize = batchSizeValue * 1024

	var labelValues labelSetJSON
	if labels == "" {
		labels = `
{"labels": [{"key": "job", "label": "fluent-bit"}]}
`
	}

	err = json.Unmarshal(([]byte)(labels), &labelValues)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse Labels")
	}
	labelSet := make(model.LabelSet)
	for _, v := range labelValues.Labels {
		labelSet[model.LabelName(v.Key)] = model.LabelValue(v.Label)
	}
	lc.labelSet = labelSet

	return lc, nil
}
