package logql

import (
	"strconv"
	"time"

	"github.com/grafana/loki/pkg/logql/log/labelfilter"

	"github.com/prometheus/prometheus/pkg/labels"
)

var (
	ExtractBytes = bytesSampleExtractor{}
	ExtractCount = countSampleExtractor{}
)

// SampleExtractor transforms a log entry into a sample.
// In case of failure the second return value will be false.
type SampleExtractor interface {
	Extract(line []byte, lbs labels.Labels) (bool, float64, labels.Labels)
}

type countSampleExtractor struct{}

func (countSampleExtractor) Extract(line []byte, lbs labels.Labels) (bool, float64, labels.Labels) {
	return true, 1., lbs
}

type bytesSampleExtractor struct{}

func (bytesSampleExtractor) Extract(line []byte, lbs labels.Labels) (bool, float64, labels.Labels) {
	return true, float64(len(line)), lbs
}

type labelSampleExtractor struct {
	labelName  string
	gr         *grouping
	postFilter labelfilter.Filterer
	conversion string // the sample conversion operation to attempt

	builder *labels.Builder
}

func newLabelSampleExtractor(labelName, conversion string, postFilters []labelfilter.Filterer, gr *grouping) *labelSampleExtractor {
	return &labelSampleExtractor{
		labelName:  labelName,
		conversion: conversion,
		gr:         gr,
		postFilter: labelfilter.ReduceAnd(postFilters),
		builder:    labels.NewBuilder(nil),
	}
}

func (l *labelSampleExtractor) Extract(_ []byte, lbs labels.Labels) (bool, float64, labels.Labels) {
	stringValue := lbs.Get(l.labelName)
	l.builder.Reset(lbs)
	if stringValue == "" {
		l.builder.Set(errorLabel, errSampleExtraction)
		return true, 0, lbs
	}
	var f float64
	var err error
	switch l.conversion {
	case OpConvDuration, OpConvDurationSeconds:
		f, err = convertDuration(stringValue)
	default:
		f, err = convertFloat(stringValue)
	}
	if err != nil {
		l.builder.Set(errorLabel, errSampleExtraction)
		return true, 0, lbs
	}
	return true, f, l.groupLabels(lbs)
}

func (l *labelSampleExtractor) groupLabels(lbs labels.Labels) labels.Labels {
	if l.gr != nil {
		if l.gr.without {
			return lbs.WithoutLabels(append(l.gr.groups, l.labelName)...)
		}
		return lbs.WithLabels(l.gr.groups...)
	}
	return lbs.WithoutLabels(l.labelName)
}

func convertFloat(v string) (float64, error) {
	return strconv.ParseFloat(v, 64)
}

func convertDuration(v string) (float64, error) {
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, err
	}
	return d.Seconds(), nil
}
