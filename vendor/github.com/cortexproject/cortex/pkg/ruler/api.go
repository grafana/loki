package ruler

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/user"
	"gopkg.in/yaml.v2"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	rulefmt "github.com/cortexproject/cortex/pkg/ruler/legacy_rulefmt"
	"github.com/cortexproject/cortex/pkg/ruler/rules"
	store "github.com/cortexproject/cortex/pkg/ruler/rules"
	"github.com/cortexproject/cortex/pkg/util"
)

// In order to reimplement the prometheus rules API, a large amount of code was copied over
// This is required because the prometheus api implementation does not pass a context to
// the rule retrieval function.
// https://github.com/prometheus/prometheus/blob/2aacd807b3ec6ddd90ae55f3a42f4cffed561ea9/web/api/v1/api.go#L108
// https://github.com/prometheus/prometheus/pull/4999

type response struct {
	Status    string       `json:"status"`
	Data      interface{}  `json:"data"`
	ErrorType v1.ErrorType `json:"errorType"`
	Error     string       `json:"error"`
}

// AlertDiscovery has info for all active alerts.
type AlertDiscovery struct {
	Alerts []*Alert `json:"alerts"`
}

// Alert has info for an alert.
type Alert struct {
	Labels      labels.Labels `json:"labels"`
	Annotations labels.Labels `json:"annotations"`
	State       string        `json:"state"`
	ActiveAt    *time.Time    `json:"activeAt"`
	Value       string        `json:"value"`
}

// RuleDiscovery has info for all rules
type RuleDiscovery struct {
	RuleGroups []*RuleGroup `json:"groups"`
}

// RuleGroup has info for rules which are part of a group
type RuleGroup struct {
	Name string `json:"name"`
	File string `json:"file"`
	// In order to preserve rule ordering, while exposing type (alerting or recording)
	// specific properties, both alerting and recording rules are exposed in the
	// same array.
	Rules          []rule    `json:"rules"`
	Interval       float64   `json:"interval"`
	LastEvaluation time.Time `json:"lastEvaluation"`
	EvaluationTime float64   `json:"evaluationTime"`
}

type rule interface{}

type alertingRule struct {
	// State can be "pending", "firing", "inactive".
	State          string        `json:"state"`
	Name           string        `json:"name"`
	Query          string        `json:"query"`
	Duration       float64       `json:"duration"`
	Labels         labels.Labels `json:"labels"`
	Annotations    labels.Labels `json:"annotations"`
	Alerts         []*Alert      `json:"alerts"`
	Health         string        `json:"health"`
	LastError      string        `json:"lastError"`
	Type           v1.RuleType   `json:"type"`
	LastEvaluation time.Time     `json:"lastEvaluation"`
	EvaluationTime float64       `json:"evaluationTime"`
}

type recordingRule struct {
	Name           string        `json:"name"`
	Query          string        `json:"query"`
	Labels         labels.Labels `json:"labels"`
	Health         string        `json:"health"`
	LastError      string        `json:"lastError"`
	Type           v1.RuleType   `json:"type"`
	LastEvaluation time.Time     `json:"lastEvaluation"`
	EvaluationTime float64       `json:"evaluationTime"`
}

func respondError(logger log.Logger, w http.ResponseWriter, msg string) {
	b, err := json.Marshal(&response{
		Status:    "error",
		ErrorType: v1.ErrServer,
		Error:     msg,
		Data:      nil,
	})

	if err != nil {
		level.Error(logger).Log("msg", "error marshaling json response", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusInternalServerError)
	if n, err := w.Write(b); err != nil {
		level.Error(logger).Log("msg", "error writing response", "bytesWritten", n, "err", err)
	}
}

func (r *Ruler) PrometheusRules(w http.ResponseWriter, req *http.Request) {
	logger := util.WithContext(req.Context(), util.Logger)
	userID, ctx, err := user.ExtractOrgIDFromHTTPRequest(req)
	if err != nil || userID == "" {
		level.Error(logger).Log("msg", "error extracting org id from context", "err", err)
		respondError(logger, w, "no valid org id found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	rgs, err := r.GetRules(ctx)

	if err != nil {
		respondError(logger, w, err.Error())
		return
	}

	groups := make([]*RuleGroup, 0, len(rgs))

	for _, g := range rgs {
		grp := RuleGroup{
			Name:           g.Group.Name,
			File:           g.Group.Namespace,
			Rules:          make([]rule, len(g.ActiveRules)),
			Interval:       g.Group.Interval.Seconds(),
			LastEvaluation: g.GetEvaluationTimestamp(),
			EvaluationTime: g.GetEvaluationDuration().Seconds(),
		}

		for i, rl := range g.ActiveRules {
			if g.ActiveRules[i].Rule.Alert != "" {
				alerts := make([]*Alert, 0, len(rl.Alerts))
				for _, a := range rl.Alerts {
					alerts = append(alerts, &Alert{
						Labels:      client.FromLabelAdaptersToLabels(a.Labels),
						Annotations: client.FromLabelAdaptersToLabels(a.Annotations),
						State:       a.GetState(),
						ActiveAt:    &a.ActiveAt,
						Value:       strconv.FormatFloat(a.Value, 'e', -1, 64),
					})
				}
				grp.Rules[i] = alertingRule{
					State:          rl.GetState(),
					Name:           rl.Rule.GetAlert(),
					Query:          rl.Rule.GetExpr(),
					Duration:       rl.Rule.For.Seconds(),
					Labels:         client.FromLabelAdaptersToLabels(rl.Rule.Labels),
					Annotations:    client.FromLabelAdaptersToLabels(rl.Rule.Annotations),
					Alerts:         alerts,
					Health:         rl.GetHealth(),
					LastError:      rl.GetLastError(),
					LastEvaluation: rl.GetEvaluationTimestamp(),
					EvaluationTime: rl.GetEvaluationDuration().Seconds(),
					Type:           v1.RuleTypeAlerting,
				}
			} else {
				grp.Rules[i] = recordingRule{
					Name:           rl.Rule.GetRecord(),
					Query:          rl.Rule.GetExpr(),
					Labels:         client.FromLabelAdaptersToLabels(rl.Rule.Labels),
					Health:         rl.GetHealth(),
					LastError:      rl.GetLastError(),
					LastEvaluation: rl.GetEvaluationTimestamp(),
					EvaluationTime: rl.GetEvaluationDuration().Seconds(),
					Type:           v1.RuleTypeRecording,
				}
			}
		}
		groups = append(groups, &grp)
	}

	// keep data.groups are in order
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].File < groups[j].File
	})

	b, err := json.Marshal(&response{
		Status: "success",
		Data:   &RuleDiscovery{RuleGroups: groups},
	})
	if err != nil {
		level.Error(logger).Log("msg", "error marshaling json response", "err", err)
		respondError(logger, w, "unable to marshal the requested data")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if n, err := w.Write(b); err != nil {
		level.Error(logger).Log("msg", "error writing response", "bytesWritten", n, "err", err)
	}
}

func (r *Ruler) PrometheusAlerts(w http.ResponseWriter, req *http.Request) {
	logger := util.WithContext(req.Context(), util.Logger)
	userID, ctx, err := user.ExtractOrgIDFromHTTPRequest(req)
	if err != nil || userID == "" {
		level.Error(logger).Log("msg", "error extracting org id from context", "err", err)
		respondError(logger, w, "no valid org id found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	rgs, err := r.GetRules(ctx)

	if err != nil {
		respondError(logger, w, err.Error())
		return
	}

	alerts := []*Alert{}

	for _, g := range rgs {
		for _, rl := range g.ActiveRules {
			if rl.Rule.Alert != "" {
				for _, a := range rl.Alerts {
					alerts = append(alerts, &Alert{
						Labels:      client.FromLabelAdaptersToLabels(a.Labels),
						Annotations: client.FromLabelAdaptersToLabels(a.Annotations),
						State:       a.GetState(),
						ActiveAt:    &a.ActiveAt,
						Value:       strconv.FormatFloat(a.Value, 'e', -1, 64),
					})
				}
			}
		}
	}

	b, err := json.Marshal(&response{
		Status: "success",
		Data:   &AlertDiscovery{Alerts: alerts},
	})
	if err != nil {
		level.Error(logger).Log("msg", "error marshaling json response", "err", err)
		respondError(logger, w, "unable to marshal the requested data")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if n, err := w.Write(b); err != nil {
		level.Error(logger).Log("msg", "error writing response", "bytesWritten", n, "err", err)
	}
}

var (
	// ErrNoNamespace signals that no namespace was specified in the request
	ErrNoNamespace = errors.New("a namespace must be provided in the request")
	// ErrNoGroupName signals a group name url parameter was not found
	ErrNoGroupName = errors.New("a matching group name must be provided in the request")
	// ErrNoRuleGroups signals the rule group requested does not exist
	ErrNoRuleGroups = errors.New("no rule groups found")
	// ErrBadRuleGroup is returned when the provided rule group can not be unmarshalled
	ErrBadRuleGroup = errors.New("unable to decoded rule group")
)

// ValidateRuleGroup validates a rulegroup
func ValidateRuleGroup(g rulefmt.RuleGroup) []error {
	var errs []error
	for i, r := range g.Rules {
		for _, err := range r.Validate() {
			var ruleName string
			if r.Alert != "" {
				ruleName = r.Alert
			} else {
				ruleName = r.Record
			}
			errs = append(errs, &rulefmt.Error{
				Group:    g.Name,
				Rule:     i,
				RuleName: ruleName,
				Err:      err,
			})
		}
	}

	return errs
}

func marshalAndSend(output interface{}, w http.ResponseWriter, logger log.Logger) {
	d, err := yaml.Marshal(&output)
	if err != nil {
		level.Error(logger).Log("msg", "error marshalling yaml rule groups", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/yaml")
	if _, err := w.Write(d); err != nil {
		level.Error(logger).Log("msg", "error writing yaml response", "err", err)
		return
	}
}

func respondAccepted(w http.ResponseWriter, logger log.Logger) {
	b, err := json.Marshal(&response{
		Status: "success",
	})
	if err != nil {
		level.Error(logger).Log("msg", "error marshaling json response", "err", err)
		respondError(logger, w, "unable to marshal the requested data")
		return
	}
	w.Header().Set("Content-Type", "application/json")

	// Return a status accepted because the rule has been stored and queued for polling, but is not currently active
	w.WriteHeader(http.StatusAccepted)
	if n, err := w.Write(b); err != nil {
		level.Error(logger).Log("msg", "error writing response", "bytesWritten", n, "err", err)
	}
}

// parseNamespace parses the namespace from the provided set of params, in this
// api these params are derived from the url path
func parseNamespace(params map[string]string) (string, error) {
	namespace, exists := params["namespace"]
	if !exists {
		return "", ErrNoNamespace
	}

	namespace, err := url.PathUnescape(namespace)
	if err != nil {
		return "", err
	}

	return namespace, nil
}

// parseGroupName parses the group name from the provided set of params, in this
// api these params are derived from the url path
func parseGroupName(params map[string]string) (string, error) {
	groupName, exists := params["groupName"]
	if !exists {
		return "", ErrNoGroupName
	}

	groupName, err := url.PathUnescape(groupName)
	if err != nil {
		return "", err
	}

	return groupName, nil
}

// parseRequest parses the incoming request to parse out the userID, rules namespace, and rule group name
// and returns them in that order. It also allows users to require a namespace or group name and return
// an error if it they can not be parsed.
func parseRequest(req *http.Request, requireNamespace, requireGroup bool) (string, string, string, error) {
	userID, err := user.ExtractOrgID(req.Context())
	if err != nil {
		return "", "", "", user.ErrNoOrgID
	}

	vars := mux.Vars(req)

	namespace, err := parseNamespace(vars)
	if err != nil {
		if err != ErrNoNamespace || requireNamespace {
			return "", "", "", err
		}
	}

	group, err := parseGroupName(vars)
	if err != nil {
		if err != ErrNoGroupName || requireGroup {
			return "", "", "", err
		}
	}

	return userID, namespace, group, nil
}

func (r *Ruler) ListRules(w http.ResponseWriter, req *http.Request) {
	logger := util.WithContext(req.Context(), util.Logger)

	userID, namespace, _, err := parseRequest(req, false, false)
	if err != nil {
		respondError(logger, w, err.Error())
		return
	}

	level.Debug(logger).Log("msg", "retrieving rule groups with namespace", "userID", userID, "namespace", namespace)
	rgs, err := r.store.ListRuleGroups(req.Context(), userID, namespace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	level.Debug(logger).Log("msg", "retrieved rule groups from rule store", "userID", userID, "num_namespaces", len(rgs))

	if len(rgs) == 0 {
		level.Info(logger).Log("msg", "no rule groups found", "userID", userID)
		http.Error(w, ErrNoRuleGroups.Error(), http.StatusNotFound)
		return
	}

	formatted := rgs.Formatted()
	marshalAndSend(formatted, w, logger)
}

func (r *Ruler) GetRuleGroup(w http.ResponseWriter, req *http.Request) {
	logger := util.WithContext(req.Context(), util.Logger)
	userID, namespace, groupName, err := parseRequest(req, true, true)
	if err != nil {
		respondError(logger, w, err.Error())
		return
	}

	rg, err := r.store.GetRuleGroup(req.Context(), userID, namespace, groupName)
	if err != nil {
		if err == store.ErrGroupNotFound {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	formatted := store.FromProto(rg)
	marshalAndSend(formatted, w, logger)
}

func (r *Ruler) CreateRuleGroup(w http.ResponseWriter, req *http.Request) {
	logger := util.WithContext(req.Context(), util.Logger)
	userID, namespace, _, err := parseRequest(req, true, false)
	if err != nil {
		respondError(logger, w, err.Error())
		return
	}

	payload, err := ioutil.ReadAll(req.Body)
	if err != nil {
		level.Error(logger).Log("msg", "unable to read rule group payload", "err", err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	level.Debug(logger).Log("msg", "attempting to unmarshal rulegroup", "userID", userID, "group", string(payload))

	rg := rulefmt.RuleGroup{}
	err = yaml.Unmarshal(payload, &rg)
	if err != nil {
		level.Error(logger).Log("msg", "unable to unmarshal rule group payload", "err", err.Error())
		http.Error(w, ErrBadRuleGroup.Error(), http.StatusBadRequest)
		return
	}

	errs := ValidateRuleGroup(rg)
	if len(errs) > 0 {
		for _, err := range errs {
			level.Error(logger).Log("msg", "unable to validate rule group payload", "err", err.Error())
		}
		http.Error(w, errs[0].Error(), http.StatusBadRequest)
		return
	}

	rgProto := store.ToProto(userID, namespace, rg)

	level.Debug(logger).Log("msg", "attempting to store rulegroup", "userID", userID, "group", rgProto.String())
	err = r.store.SetRuleGroup(req.Context(), userID, namespace, rgProto)
	if err != nil {
		level.Error(logger).Log("msg", "unable to store rule group", "err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondAccepted(w, logger)
}

func (r *Ruler) DeleteRuleGroup(w http.ResponseWriter, req *http.Request) {
	logger := util.WithContext(req.Context(), util.Logger)

	userID, namespace, groupName, err := parseRequest(req, true, true)
	if err != nil {
		respondError(logger, w, err.Error())
		return
	}

	err = r.store.DeleteRuleGroup(req.Context(), userID, namespace, groupName)
	if err != nil {
		if err == rules.ErrGroupNotFound {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		respondError(logger, w, err.Error())
		return
	}

	respondAccepted(w, logger)
}
