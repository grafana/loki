// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package targetspb

import (
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/thanos-io/thanos/pkg/store/labelpb"
)

func NewTargetsResponse(targets *TargetDiscovery) *TargetsResponse {
	return &TargetsResponse{
		Result: &TargetsResponse_Targets{
			Targets: targets,
		},
	}
}

func NewWarningTargetsResponse(warning error) *TargetsResponse {
	return &TargetsResponse{
		Result: &TargetsResponse_Warning{
			Warning: warning.Error(),
		},
	}
}

func (x *TargetHealth) UnmarshalJSON(entry []byte) error {
	fieldStr, err := strconv.Unquote(string(entry))
	if err != nil {
		return errors.Wrapf(err, "targetHealth: unquote %v", string(entry))
	}

	if len(fieldStr) == 0 {
		return errors.New("empty targetHealth")
	}

	state, ok := TargetHealth_value[strings.ToUpper(fieldStr)]
	if !ok {
		return errors.Errorf("unknown targetHealth: %v", string(entry))
	}
	*x = TargetHealth(state)
	return nil
}

func (x *TargetHealth) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(strings.ToLower(x.String()))), nil
}

func (x TargetHealth) Compare(y TargetHealth) int {
	return int(x) - int(y)
}

func (t1 *ActiveTarget) Compare(t2 *ActiveTarget) int {
	if d := strings.Compare(t1.ScrapeUrl, t2.ScrapeUrl); d != 0 {
		return d
	}

	if d := strings.Compare(t1.ScrapePool, t2.ScrapePool); d != 0 {
		return d
	}

	if d := labels.Compare(t1.DiscoveredLabels.PromLabels(), t2.DiscoveredLabels.PromLabels()); d != 0 {
		return d
	}

	if d := labels.Compare(t1.Labels.PromLabels(), t2.Labels.PromLabels()); d != 0 {
		return d
	}

	return 0
}

func (t1 *ActiveTarget) CompareState(t2 *ActiveTarget) int {
	if d := t1.Health.Compare(t2.Health); d != 0 {
		return d
	}

	if t1.LastScrape.Before(t2.LastScrape) {
		return 1
	}

	if t1.LastScrape.After(t2.LastScrape) {
		return -1
	}

	return 0
}

func (t1 *DroppedTarget) Compare(t2 *DroppedTarget) int {
	if d := labels.Compare(t1.DiscoveredLabels.PromLabels(), t2.DiscoveredLabels.PromLabels()); d != 0 {
		return d
	}

	return 0
}

func (t *ActiveTarget) SetLabels(ls labels.Labels) {
	var result labelpb.ZLabelSet

	if len(ls) > 0 {
		result = labelpb.ZLabelSet{Labels: labelpb.ZLabelsFromPromLabels(ls)}
	}

	t.Labels = result
}

func (t *ActiveTarget) SetDiscoveredLabels(ls labels.Labels) {
	var result labelpb.ZLabelSet

	if len(ls) > 0 {
		result = labelpb.ZLabelSet{Labels: labelpb.ZLabelsFromPromLabels(ls)}
	}

	t.DiscoveredLabels = result
}

func (t *DroppedTarget) SetDiscoveredLabels(ls labels.Labels) {
	var result labelpb.ZLabelSet

	if len(ls) > 0 {
		result = labelpb.ZLabelSet{Labels: labelpb.ZLabelsFromPromLabels(ls)}
	}

	t.DiscoveredLabels = result
}
