package labelaccess

import (
	"context"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/labelaccess/types"

	//"github.com/grafana/backend-enterprise-libs/enterprise/admin/types"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func WrapStore(wrapped storage.Store) storage.Store {
	w := StoreWrapper{
		Store: wrapped,
	}

	return &w
}

type StoreWrapper struct {
	storage.Store
}

func (w *StoreWrapper) LabelValuesForMetricName(ctx context.Context, orgID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	_ = level.Debug(util_log.Logger).Log("msg", "using store wrapper")
	instancePolicyMap, err := ExtractLabelMatchersContext(ctx)
	if err != nil {
		if err == errNoMatcherSource {
			// No lbac header, so return the given labelValues
			return w.Store.LabelValuesForMetricName(ctx, orgID, from, through, metricName, labelName, matchers...)
		}
		_ = level.Debug(util_log.Logger).Log("msg", "unable to find instance policy map in context", "err", err)
		return nil, err
	}

	policies, exist := instancePolicyMap[orgID]
	if !exist {
		return w.Store.LabelValuesForMetricName(ctx, orgID, from, through, metricName, labelName, matchers...)
	}

	var result util.UniqueStrings
	for _, p := range policies {
		policyMatchers := make([]*labels.Matcher, len(p.Selector))
		for i, lm := range p.Selector {
			matcher, err := types.LabelMatcherToPromLabel(lm)
			policyMatchers[i] = matcher
			if err != nil {
				// This shouldn't happen: log it and do not match
				_ = level.Debug(util_log.Logger).Log("msg", "unable to encode matcher as Prometheus matcher", "label", labelName, "err", err)
				break
			}
		}
		policyMatchers = append(policyMatchers, matchers...)
		_ = level.Debug(util_log.Logger).Log("msg", "fetching label values", "org_id", orgID, "label", labelName, "matchers", len(policyMatchers))

		labelValues, err := w.Store.LabelValuesForMetricName(ctx, orgID, from, through, metricName, labelName, policyMatchers...)
		if err != nil {
			return nil, err
		}
		_ = level.Debug(util_log.Logger).Log("msg", "fetched label values", "org_id", orgID, "label", labelName, "labels", len(labelValues))
		result.Add(labelValues...)
	}

	return result.Strings(), nil
}
