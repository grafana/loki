package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/grafana/loki/pkg/logql"
	"github.com/prometheus/common/model"
)

type lokiConfig struct {
	url       flagext.URLValue
	batchWait time.Duration
	batchSize int
	labelSet  model.LabelSet
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
		batchWaitValue = 1
	}
	lc.batchWait = time.Duration(batchWaitValue) * time.Second

	batchSizeValue, err := strconv.Atoi(batchSize)
	if err != nil || batchSize == "" {
		batchSizeValue = 100 * 1024
	}
	lc.batchSize = batchSizeValue

	if labels == "" {
		labels = `{job="fluent-bit"}`
	}

	ls, err := logql.ParseExpr(labels)
	if err != nil {
		return nil, err
	}
	matchers := ls.Matchers()
	labelSet := make(model.LabelSet)
	for _, m := range matchers {
		labelSet[model.LabelName(m.Name)] = model.LabelValue(m.Value)
	}
	lc.labelSet = labelSet

	return lc, nil
}
