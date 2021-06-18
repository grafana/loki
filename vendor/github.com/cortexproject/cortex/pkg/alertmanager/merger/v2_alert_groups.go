package merger

import (
	"errors"
	"sort"

	"github.com/go-openapi/swag"
	v2 "github.com/prometheus/alertmanager/api/v2"
	v2_models "github.com/prometheus/alertmanager/api/v2/models"
	prom_model "github.com/prometheus/common/model"
)

// V2AlertGroups implements the Merger interface for GET /v2/alerts/groups. It returns
// the union of alert groups over all the responses. When the same alert exists in the same
// group for multiple responses, the instance of that alert with the most recent UpdatedAt
// timestamp is returned in that group within the response.
type V2AlertGroups struct{}

func (V2AlertGroups) MergeResponses(in [][]byte) ([]byte, error) {
	groups := make(v2_models.AlertGroups, 0)
	for _, body := range in {
		parsed := make(v2_models.AlertGroups, 0)
		if err := swag.ReadJSON(body, &parsed); err != nil {
			return nil, err
		}
		groups = append(groups, parsed...)
	}

	merged, err := mergeV2AlertGroups(groups)
	if err != nil {
		return nil, err
	}

	return swag.WriteJSON(merged)
}

func mergeV2AlertGroups(in v2_models.AlertGroups) (v2_models.AlertGroups, error) {
	// Gather lists of all alerts for each distinct group.
	groups := make(map[groupKey]*v2_models.AlertGroup)
	for _, group := range in {
		if group.Receiver == nil {
			return nil, errors.New("unexpected nil receiver")
		}
		if group.Receiver.Name == nil {
			return nil, errors.New("unexpected nil receiver name")
		}

		key := getGroupKey(group)
		if current, ok := groups[key]; ok {
			current.Alerts = append(current.Alerts, group.Alerts...)
		} else {
			groups[key] = group
		}
	}

	// Merge duplicates of the same alert within each group.
	for _, group := range groups {
		var err error
		group.Alerts, err = mergeV2Alerts(group.Alerts)
		if err != nil {
			return nil, err
		}
	}

	result := make(v2_models.AlertGroups, 0, len(groups))
	for _, group := range groups {
		result = append(result, group)
	}

	// Mimic Alertmanager which returns groups ordered by labels and receiver.
	sort.Sort(byGroup(result))

	return result, nil
}

// getGroupKey returns an identity for a group which can be used to match it against other groups.
// Only the receiver name is necessary to ensure grouping by receiver, and for the labels, we again
// use the same method for matching the group labels as used internally, generating the fingerprint.
func getGroupKey(group *v2_models.AlertGroup) groupKey {
	return groupKey{
		fingerprint: prom_model.LabelsToSignature(group.Labels),
		receiver:    *group.Receiver.Name,
	}
}

type groupKey struct {
	fingerprint uint64
	receiver    string
}

// byGroup implements the ordering of Alertmanager dispatch.AlertGroups on the OpenAPI type.
type byGroup v2_models.AlertGroups

func (ag byGroup) Swap(i, j int) { ag[i], ag[j] = ag[j], ag[i] }
func (ag byGroup) Less(i, j int) bool {
	iLabels := v2.APILabelSetToModelLabelSet(ag[i].Labels)
	jLabels := v2.APILabelSetToModelLabelSet(ag[j].Labels)

	if iLabels.Equal(jLabels) {
		return *ag[i].Receiver.Name < *ag[j].Receiver.Name
	}
	return iLabels.Before(jLabels)
}
func (ag byGroup) Len() int { return len(ag) }
