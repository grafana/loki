package metrics

import (
	"net/http"
	"sort"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/gorilla/mux"
	"github.com/grafana/agent/pkg/metrics/cluster/configapi"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

// WireAPI adds API routes to the provided mux router.
func (a *Agent) WireAPI(r *mux.Router) {
	a.cluster.WireAPI(r)

	r.HandleFunc("/agent/api/v1/instances", a.ListInstancesHandler).Methods("GET")
	r.HandleFunc("/agent/api/v1/targets", a.ListTargetsHandler).Methods("GET")
}

// ListInstancesHandler writes the set of currently running instances to the http.ResponseWriter.
func (a *Agent) ListInstancesHandler(w http.ResponseWriter, _ *http.Request) {
	cfgs := a.mm.ListConfigs()
	instanceNames := make([]string, 0, len(cfgs))
	for k := range cfgs {
		instanceNames = append(instanceNames, k)
	}
	sort.Strings(instanceNames)

	err := configapi.WriteResponse(w, http.StatusOK, instanceNames)
	if err != nil {
		level.Error(a.logger).Log("msg", "failed to write response", "err", err)
	}
}

// ListTargetsHandler retrieves the full set of targets across all instances and shows
// information on them.
func (a *Agent) ListTargetsHandler(w http.ResponseWriter, _ *http.Request) {
	instances := a.mm.ListInstances()
	resp := ListTargetsResponse{}

	for instName, inst := range instances {
		tps := inst.TargetsActive()

		for key, targets := range tps {
			for _, tgt := range targets {
				var lastError string
				if scrapeError := tgt.LastError(); scrapeError != nil {
					lastError = scrapeError.Error()
				}

				resp = append(resp, TargetInfo{
					InstanceName: instName,
					TargetGroup:  key,

					Endpoint:         tgt.URL().String(),
					State:            string(tgt.Health()),
					DiscoveredLabels: tgt.DiscoveredLabels(),
					Labels:           tgt.Labels(),
					LastScrape:       tgt.LastScrape(),
					ScrapeDuration:   tgt.LastScrapeDuration().Milliseconds(),
					ScrapeError:      lastError,
				})
			}
		}
	}

	sort.Slice(resp, func(i, j int) bool {
		// sort by instance, then target group, then job label, then instance label
		var (
			iInstance      = resp[i].InstanceName
			iTargetGroup   = resp[i].TargetGroup
			iJobLabel      = resp[i].Labels.Get(model.JobLabel)
			iInstanceLabel = resp[i].Labels.Get(model.InstanceLabel)

			jInstance      = resp[j].InstanceName
			jTargetGroup   = resp[j].TargetGroup
			jJobLabel      = resp[j].Labels.Get(model.JobLabel)
			jInstanceLabel = resp[j].Labels.Get(model.InstanceLabel)
		)

		switch {
		case iInstance != jInstance:
			return iInstance < jInstance
		case iTargetGroup != jTargetGroup:
			return iTargetGroup < jTargetGroup
		case iJobLabel != jJobLabel:
			return iJobLabel < jJobLabel
		default:
			return iInstanceLabel < jInstanceLabel
		}
	})

	err := configapi.WriteResponse(w, http.StatusOK, resp)
	if err != nil {
		level.Error(a.logger).Log("msg", "failed to write response", "err", err)
	}
}

// ListTargetsResponse is returned by the ListTargetsHandler.
type ListTargetsResponse []TargetInfo

// TargetInfo describes a specific target.
type TargetInfo struct {
	InstanceName string `json:"instance"`
	TargetGroup  string `json:"target_group"`

	Endpoint         string        `json:"endpoint"`
	State            string        `json:"state"`
	Labels           labels.Labels `json:"labels"`
	DiscoveredLabels labels.Labels `json:"discovered_labels"`
	LastScrape       time.Time     `json:"last_scrape"`
	ScrapeDuration   int64         `json:"scrape_duration_ms"`
	ScrapeError      string        `json:"scrape_error"`
}
