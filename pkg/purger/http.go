package purger

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk/purger"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logql"
)

type DeleteRequestHandler struct {
	*purger.DeleteRequestHandler
	deleteStore *purger.DeleteStore
}

// NewDeleteRequestHandler creates a DeleteRequestHandler
func NewDeleteRequestHandler(deleteStore *purger.DeleteStore, deleteRequestCancelPeriod time.Duration, registerer prometheus.Registerer) *DeleteRequestHandler {
	return &DeleteRequestHandler{
		purger.NewDeleteRequestHandler(deleteStore, deleteRequestCancelPeriod, registerer),
		deleteStore,
	}
}

func (dm *DeleteRequestHandler) AddDeleteRequestHandler(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	match := params["match[]"]
	if len(match) == 0 {
		http.Error(w, "selectors not set", http.StatusBadRequest)
		return
	}

	params.Del("match[]")

	for i := range match {
		lbls, err := logql.ParseMatchers(match[i])
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		lbls = append(lbls, &labels.Matcher{
			Type:  labels.MatchEqual,
			Name:  labels.MetricName,
			Value: "logs",
		})

		params.Set("match[]", convertMatchersToString(lbls))
	}

	r.URL.RawQuery = params.Encode()
	dm.DeleteRequestHandler.AddDeleteRequestHandler(w, r)
}

func (dm *DeleteRequestHandler) GetAllDeleteRequestsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	deleteRequests, err := dm.deleteStore.GetAllDeleteRequestsForUser(ctx, userID)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error getting delete requests from the store", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for i := range deleteRequests {
		for j, selector := range deleteRequests[i].Selectors {

			matchers, err := logql.ParseMatchers(selector)
			if err != nil {
				level.Error(util_log.Logger).Log("msg", "error parsing matchers", "err", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			for i, matcher := range matchers {
				if matcher.Name == labels.MetricName {
					matchers = append(matchers[:i], matchers[i+1:]...)
					break
				}
			}

			deleteRequests[i].Selectors[j] = convertMatchersToString(matchers)
		}
	}

	if err := json.NewEncoder(w).Encode(deleteRequests); err != nil {
		level.Error(util_log.Logger).Log("msg", "error marshalling response", "err", err)
		http.Error(w, fmt.Sprintf("Error marshalling response: %v", err), http.StatusInternalServerError)
	}
}

func convertMatchersToString(matchers []*labels.Matcher) string {
	out := strings.Builder{}
	out.WriteRune('{')

	for idx, m := range matchers {
		if idx > 0 {
			out.WriteRune(',')
		}

		out.WriteString(m.String())
	}

	out.WriteRune('}')
	return out.String()
}
