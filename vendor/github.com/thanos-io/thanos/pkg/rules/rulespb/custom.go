// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package rulespb

import (
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/thanos-io/thanos/pkg/store/labelpb"
)

const (
	RuleRecordingType = "recording"
	RuleAlertingType  = "alerting"
)

func NewRuleGroupRulesResponse(rg *RuleGroup) *RulesResponse {
	return &RulesResponse{
		Result: &RulesResponse_Group{
			Group: rg,
		},
	}
}

func NewWarningRulesResponse(warning error) *RulesResponse {
	return &RulesResponse{
		Result: &RulesResponse_Warning{
			Warning: warning.Error(),
		},
	}
}

func NewRecordingRule(r *RecordingRule) *Rule {
	return &Rule{
		Result: &Rule_Recording{Recording: r},
	}
}

// Compare compares equal recording rules r1 and r2 and returns:
//
//   < 0 if r1 < r2  if rule r1 is lexically before rule r2
//     0 if r1 == r2
//   > 0 if r1 > r2  if rule r1 is lexically after rule r2
//
// More formally, the ordering is determined in the following order:
//
// 1. recording rule last evaluation (earlier evaluation comes first)
//
// Note: This method assumes r1 and r2 are logically equal as per Rule#Compare.
func (r1 *RecordingRule) Compare(r2 *RecordingRule) int {
	if r1.LastEvaluation.Before(r2.LastEvaluation) {
		return 1
	}

	if r1.LastEvaluation.After(r2.LastEvaluation) {
		return -1
	}

	return 0
}

func NewAlertingRule(a *Alert) *Rule {
	return &Rule{
		Result: &Rule_Alert{Alert: a},
	}
}

func (r *Rule) GetLabels() labels.Labels {
	switch {
	case r.GetRecording() != nil:
		return r.GetRecording().Labels.PromLabels()
	case r.GetAlert() != nil:
		return r.GetAlert().Labels.PromLabels()
	default:
		return nil
	}
}

func (r *Rule) SetLabels(ls labels.Labels) {
	var result labelpb.ZLabelSet

	if len(ls) > 0 {
		result = labelpb.ZLabelSet{Labels: labelpb.ZLabelsFromPromLabels(ls)}
	}

	switch {
	case r.GetRecording() != nil:
		r.GetRecording().Labels = result
	case r.GetAlert() != nil:
		r.GetAlert().Labels = result
	}
}

func (r *Rule) GetName() string {
	switch {
	case r.GetRecording() != nil:
		return r.GetRecording().Name
	case r.GetAlert() != nil:
		return r.GetAlert().Name
	default:
		return ""
	}
}

func (r *Rule) GetQuery() string {
	switch {
	case r.GetRecording() != nil:
		return r.GetRecording().Query
	case r.GetAlert() != nil:
		return r.GetAlert().Query
	default:
		return ""
	}
}

func (r *Rule) GetLastEvaluation() time.Time {
	switch {
	case r.GetRecording() != nil:
		return r.GetRecording().LastEvaluation
	case r.GetAlert() != nil:
		return r.GetAlert().LastEvaluation
	default:
		return time.Time{}
	}
}

// Compare compares recording and alerting rules r1 and r2 and returns:
//
//   < 0 if r1 < r2  if rule r1 is not equal and lexically before rule r2
//     0 if r1 == r2 if rule r1 is logically equal to r2 (r1 and r2 are the "same" rules)
//   > 0 if r1 > r2  if rule r1 is not equal and lexically after rule r2
//
// More formally, ordering and equality is determined in the following order:
//
// 1. rule type (alerting rules come before recording rules)
// 2. rule name
// 3. rule labels
// 4. rule query
// 5. for alerting rules: duration
//
// Note: this can still leave ordering undetermined for equal rules (x == y).
// For determining ordering of equal rules, use Alert#Compare or RecordingRule#Compare.
func (r1 *Rule) Compare(r2 *Rule) int {
	if r1.GetAlert() != nil && r2.GetRecording() != nil {
		return -1
	}

	if r1.GetRecording() != nil && r2.GetAlert() != nil {
		return 1
	}

	if d := strings.Compare(r1.GetName(), r2.GetName()); d != 0 {
		return d
	}

	if d := labels.Compare(r1.GetLabels(), r2.GetLabels()); d != 0 {
		return d
	}

	if d := strings.Compare(r1.GetQuery(), r2.GetQuery()); d != 0 {
		return d
	}

	if r1.GetAlert() != nil && r2.GetAlert() != nil {
		if d := big.NewFloat(r1.GetAlert().DurationSeconds).Cmp(big.NewFloat(r2.GetAlert().DurationSeconds)); d != 0 {
			return d
		}
	}

	return 0
}

func (r *RuleGroups) MarshalJSON() ([]byte, error) {
	if r.Groups == nil {
		// Ensure that empty slices are marshaled as '[]' and not 'null'.
		return []byte(`{"groups":[]}`), nil
	}
	type plain RuleGroups
	return json.Marshal((*plain)(r))
}

// Compare compares rule group x and y and returns:
//
//   < 0 if x < y   if rule group r1 is not equal and lexically before rule group r2
//     0 if x == y  if rule group r1 is logically equal to r2 (r1 and r2 are the "same" rule groups)
//   > 0 if x > y   if rule group r1 is not equal and lexically after rule group r2
func (r1 *RuleGroup) Compare(r2 *RuleGroup) int {
	return strings.Compare(r1.Key(), r2.Key())
}

// Key returns the group key similar resembling Prometheus logic.
// See https://github.com/prometheus/prometheus/blob/869f1bc587e667b79721852d5badd9f70a39fc3f/rules/manager.go#L1062-L1065
func (r *RuleGroup) Key() string {
	if r == nil {
		return ""
	}

	return r.File + ";" + r.Name
}

func (m *Rule) UnmarshalJSON(entry []byte) error {
	decider := struct {
		Type string `json:"type"`
	}{}
	if err := json.Unmarshal(entry, &decider); err != nil {
		return errors.Wrapf(err, "rule: type field unmarshal: %v", string(entry))
	}

	switch strings.ToLower(decider.Type) {
	case "recording":
		r := &RecordingRule{}
		if err := json.Unmarshal(entry, r); err != nil {
			return errors.Wrapf(err, "rule: recording rule unmarshal: %v", string(entry))
		}

		m.Result = &Rule_Recording{Recording: r}
	case "alerting":
		r := &Alert{}
		if err := json.Unmarshal(entry, r); err != nil {
			return errors.Wrapf(err, "rule: alerting rule unmarshal: %v", string(entry))
		}

		m.Result = &Rule_Alert{Alert: r}
	case "":
		return errors.Errorf("rule: no type field provided: %v", string(entry))
	default:
		return errors.Errorf("rule: unknown type field provided %s; %v", decider.Type, string(entry))
	}
	return nil
}

func (m *Rule) MarshalJSON() ([]byte, error) {
	if r := m.GetRecording(); r != nil {
		return json.Marshal(struct {
			*RecordingRule
			Type string `json:"type"`
		}{
			RecordingRule: r,
			Type:          RuleRecordingType,
		})
	}
	a := m.GetAlert()
	if a.Alerts == nil {
		// Ensure that empty slices are marshaled as '[]' and not 'null'.
		a.Alerts = make([]*AlertInstance, 0)
	}
	return json.Marshal(struct {
		*Alert
		Type string `json:"type"`
	}{
		Alert: a,
		Type:  RuleAlertingType,
	})
}

func (r *RuleGroup) MarshalJSON() ([]byte, error) {
	if r.Rules == nil {
		// Ensure that empty slices are marshaled as '[]' and not 'null'.
		r.Rules = make([]*Rule, 0)
	}
	type plain RuleGroup
	return json.Marshal((*plain)(r))
}

func (x *AlertState) UnmarshalJSON(entry []byte) error {
	fieldStr, err := strconv.Unquote(string(entry))
	if err != nil {
		return errors.Wrapf(err, "alertState: unquote %v", string(entry))
	}

	if len(fieldStr) == 0 {
		return errors.New("empty alertState")
	}

	state, ok := AlertState_value[strings.ToUpper(fieldStr)]
	if !ok {
		return errors.Errorf("unknown alertState: %v", string(entry))
	}
	*x = AlertState(state)
	return nil
}

func (x *AlertState) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(strings.ToLower(x.String()))), nil
}

// Compare compares alert state x and y and returns:
//
//   < 0 if x < y  (alert state x is more critical than alert state y)
//     0 if x == y
//   > 0 if x > y  (alert state x is less critical than alert state y)
//
// For sorting this makes sure that more "critical" alert states come first.
func (x AlertState) Compare(y AlertState) int {
	return int(y) - int(x)
}

// Compare compares two equal alerting rules a1 and a2 and returns:
//
//   < 0 if a1 < a2  if rule a1 is lexically before rule a2
//     0 if a1 == a2
//   > 0 if a1 > a2  if rule a1 is lexically after rule a2
//
// More formally, the ordering is determined in the following order:
//
// 1. alert state
// 2. alert last evaluation (earlier evaluation comes first)
//
// Note: This method assumes a1 and a2 are logically equal as per Rule#Compare.
func (a1 *Alert) Compare(a2 *Alert) int {
	if d := a1.State.Compare(a2.State); d != 0 {
		return d
	}

	if a1.LastEvaluation.Before(a2.LastEvaluation) {
		return 1
	}

	if a1.LastEvaluation.After(a2.LastEvaluation) {
		return -1
	}

	return 0
}
