//nolint // Ignore all warnings in this file. We keep the naming as-is for backwards compatibility with existing code.
package client

import (
	"github.com/cortexproject/cortex/pkg/cortexpb"
)

// Deprecated. Use cortexpb package instead.
type PreallocWriteRequest = cortexpb.PreallocWriteRequest

// Deprecated. Use cortexpb package instead.
type PreallocTimeseries = cortexpb.PreallocTimeseries

// Deprecated. Use cortexpb package instead.
type LabelAdapter = cortexpb.LabelAdapter

// Deprecated. Use cortexpb package instead.
type Sample = cortexpb.Sample

// Deprecated. Use cortexpb package instead.
type MetricMetadata = cortexpb.MetricMetadata

// Deprecated. Use cortexpb package instead.
type WriteRequest = cortexpb.WriteRequest

// Deprecated. Use cortexpb package instead.
type WriteRequest_SourceEnum = cortexpb.WriteRequest_SourceEnum

// Deprecated. Use cortexpb package instead.
type WriteResponse = cortexpb.WriteResponse

// Deprecated. Use cortexpb package instead.
type TimeSeries = cortexpb.TimeSeries

// Deprecated. Use cortexpb package instead.
type Metric = cortexpb.Metric

// Deprecated. Use cortexpb package instead.
type MetricMetadata_MetricType = cortexpb.MetricMetadata_MetricType

// Deprecated. Use cortexpb package instead.
var MetricMetadataMetricTypeToMetricType = cortexpb.MetricMetadataMetricTypeToMetricType

// Deprecated. Use cortexpb package instead.
var FromLabelAdaptersToLabels = cortexpb.FromLabelAdaptersToLabels

// Deprecated. Use cortexpb package instead.
var FromLabelAdaptersToLabelsWithCopy = cortexpb.FromLabelAdaptersToLabelsWithCopy

// Deprecated. Use cortexpb package instead.
var CopyLabels = cortexpb.CopyLabels

// Deprecated. Use cortexpb package instead.
var FromLabelsToLabelAdapters = cortexpb.FromLabelsToLabelAdapters

// Deprecated. Use cortexpb package instead.
var FromLabelAdaptersToMetric = cortexpb.FromLabelAdaptersToMetric

// Deprecated. Use cortexpb package instead.
var FromMetricsToLabelAdapters = cortexpb.FromMetricsToLabelAdapters

// Deprecated. Use cortexpb package instead.
var ReuseSlice = cortexpb.ReuseSlice

// Deprecated. Use cortexpb package instead.
var ToWriteRequest = cortexpb.ToWriteRequest

// Deprecated. Use cortexpb package instead.
const API = cortexpb.API

// Deprecated. Use cortexpb package instead.
const RULE = cortexpb.RULE

// Deprecated. Use cortexpb package instead.
const COUNTER = cortexpb.COUNTER

// Deprecated. Use cortexpb package instead.
const GAUGE = cortexpb.GAUGE

// Deprecated. Use cortexpb package instead.
const STATESET = cortexpb.STATESET
