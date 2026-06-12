package labelaccess

import (
	"context"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/labelaccess/types"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type IngesterWrapper struct{}

func (iw *IngesterWrapper) Wrap(wrapped ingester.Interface) ingester.Interface {
	w := WrappedIngester{
		Interface: wrapped,
	}

	return &w
}

type WrappedIngester struct {
	ingester.Interface
}

// Label overrides the existing Label method.
// Label matchers in the context are applied to labels stored for the instance/org ID.
func (i *WrappedIngester) Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	_ = level.Debug(util_log.Logger).Log("msg", "using ingester wrapper")
	instancePolicyMap, err := ExtractLabelMatchersContext(ctx)
	if err != nil {
		_ = level.Debug(util_log.Logger).Log("msg", "unable to find instance policy map", "err", err)
		if err == errNoMatcherSource {
			// No lbac header, so return the given labelValues
			return i.Interface.Label(ctx, req)
		}
		return nil, err
	}

	instanceName, err := user.ExtractOrgID(ctx)
	if err != nil {
		_ = level.Debug(util_log.Logger).Log("msg", "unable to find instance name", "err", err)
		return nil, err
	}

	policies, exist := instancePolicyMap[instanceName]
	if !exist {
		_ = level.Debug(util_log.Logger).Log("msg", "no policies are defined", "orgID", instanceName)
		return i.Interface.Label(ctx, req)
	}

	instance, err := i.GetOrCreateInstance(instanceName)
	if err != nil {
		_ = level.Debug(util_log.Logger).Log("msg", "failed to get or create instance", "orgID", instanceName)
		return nil, err
	}

	var queryMatchers []*labels.Matcher
	if req.Query != "" {
		queryMatchers, err = syntax.ParseMatchers(req.Query, true)
		if err != nil {
			return nil, err
		}
	}

	var result util.UniqueStrings
	for _, p := range policies {
		matchers := make([]*labels.Matcher, len(p.Selector))
		for i, lm := range p.Selector {
			matcher, err := types.LabelMatcherToPromLabel(lm)
			matchers[i] = matcher
			if err != nil {
				// This shouldn't happen: log it and do not match
				_ = level.Debug(util_log.Logger).Log("msg", "unable to encode matcher as Prometheus matcher", "orgID", instanceName, "err", err)
				break
			}
		}
		matchers = append(matchers, queryMatchers...)
		_ = level.Debug(util_log.Logger).Log("msg", "fetching label values", "orgID", instanceName, "label", req.Name, "matchers", len(matchers))

		resp, err := instance.Label(ctx, req, matchers...)
		if err != nil {
			return nil, err
		}
		_ = level.Debug(util_log.Logger).Log("msg", "fetched label values", "orgID", instanceName, "label", req.Name, "values", len(resp.Values))
		result.Add(resp.Values...)
	}

	return &logproto.LabelResponse{
		Values: result.Strings(),
	}, nil
}
