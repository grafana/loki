// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package rulespb

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/thanos-io/thanos/pkg/store/storepb"
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

func NewAlertingRule(a *Alert) *Rule {
	return &Rule{
		Result: &Rule_Alert{Alert: a},
	}
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
	return json.Marshal(struct {
		*Alert
		Type string `json:"type"`
	}{
		Alert: a,
		Type:  RuleAlertingType,
	})
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
	return []byte(strconv.Quote(x.String())), nil
}

func (m *PromLabels) UnmarshalJSON(entry []byte) error {
	lbls := labels.Labels{}
	if err := lbls.UnmarshalJSON(entry); err != nil {
		return errors.Wrapf(err, "labels: labels field unmarshal: %v", string(entry))
	}
	m.Labels = storepb.PromLabelsToLabels(lbls)
	sort.Slice(m.Labels, func(i, j int) bool {
		return m.Labels[i].Name < m.Labels[j].Name
	})
	return nil
}

func (m *PromLabels) MarshalJSON() ([]byte, error) {
	return storepb.LabelsToPromLabels(m.Labels).MarshalJSON()
}
