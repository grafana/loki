// Copyright 2018 Prometheus Team
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v2

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/go-openapi/loads"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/prometheus/client_golang/prometheus"
	prometheus_model "github.com/prometheus/common/model"
	"github.com/prometheus/common/version"
	"github.com/rs/cors"

	"github.com/prometheus/alertmanager/api/metrics"
	open_api_models "github.com/prometheus/alertmanager/api/v2/models"
	"github.com/prometheus/alertmanager/api/v2/restapi"
	"github.com/prometheus/alertmanager/api/v2/restapi/operations"
	alert_ops "github.com/prometheus/alertmanager/api/v2/restapi/operations/alert"
	alertgroup_ops "github.com/prometheus/alertmanager/api/v2/restapi/operations/alertgroup"
	general_ops "github.com/prometheus/alertmanager/api/v2/restapi/operations/general"
	receiver_ops "github.com/prometheus/alertmanager/api/v2/restapi/operations/receiver"
	silence_ops "github.com/prometheus/alertmanager/api/v2/restapi/operations/silence"
	"github.com/prometheus/alertmanager/cluster"
	"github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/dispatch"
	"github.com/prometheus/alertmanager/pkg/labels"
	"github.com/prometheus/alertmanager/provider"
	"github.com/prometheus/alertmanager/silence"
	"github.com/prometheus/alertmanager/silence/silencepb"
	"github.com/prometheus/alertmanager/types"
)

// API represents an Alertmanager API v2
type API struct {
	peer           *cluster.Peer
	silences       *silence.Silences
	alerts         provider.Alerts
	alertGroups    groupsFn
	getAlertStatus getAlertStatusFn
	uptime         time.Time

	// mtx protects alertmanagerConfig, setAlertStatus and route.
	mtx sync.RWMutex
	// resolveTimeout represents the default resolve timeout that an alert is
	// assigned if no end time is specified.
	alertmanagerConfig *config.Config
	route              *dispatch.Route
	setAlertStatus     setAlertStatusFn

	logger log.Logger
	m      *metrics.Alerts

	Handler http.Handler
}

type groupsFn func(func(*dispatch.Route) bool, func(*types.Alert, time.Time) bool) (dispatch.AlertGroups, map[prometheus_model.Fingerprint][]string)
type getAlertStatusFn func(prometheus_model.Fingerprint) types.AlertStatus
type setAlertStatusFn func(prometheus_model.LabelSet)

// NewAPI returns a new Alertmanager API v2
func NewAPI(
	alerts provider.Alerts,
	gf groupsFn,
	sf getAlertStatusFn,
	silences *silence.Silences,
	peer *cluster.Peer,
	l log.Logger,
	r prometheus.Registerer,
) (*API, error) {
	api := API{
		alerts:         alerts,
		getAlertStatus: sf,
		alertGroups:    gf,
		peer:           peer,
		silences:       silences,
		logger:         l,
		m:              metrics.NewAlerts("v2", r),
		uptime:         time.Now(),
	}

	// load embedded swagger file
	swaggerSpec, err := loads.Analyzed(restapi.SwaggerJSON, "")
	if err != nil {
		return nil, fmt.Errorf("failed to load embedded swagger file: %v", err.Error())
	}

	// create new service API
	openAPI := operations.NewAlertmanagerAPI(swaggerSpec)

	// Skip the  redoc middleware, only serving the OpenAPI specification and
	// the API itself via RoutesHandler. See:
	// https://github.com/go-swagger/go-swagger/issues/1779
	openAPI.Middleware = func(b middleware.Builder) http.Handler {
		return middleware.Spec("", swaggerSpec.Raw(), openAPI.Context().RoutesHandler(b))
	}

	openAPI.AlertGetAlertsHandler = alert_ops.GetAlertsHandlerFunc(api.getAlertsHandler)
	openAPI.AlertPostAlertsHandler = alert_ops.PostAlertsHandlerFunc(api.postAlertsHandler)
	openAPI.AlertgroupGetAlertGroupsHandler = alertgroup_ops.GetAlertGroupsHandlerFunc(api.getAlertGroupsHandler)
	openAPI.GeneralGetStatusHandler = general_ops.GetStatusHandlerFunc(api.getStatusHandler)
	openAPI.ReceiverGetReceiversHandler = receiver_ops.GetReceiversHandlerFunc(api.getReceiversHandler)
	openAPI.SilenceDeleteSilenceHandler = silence_ops.DeleteSilenceHandlerFunc(api.deleteSilenceHandler)
	openAPI.SilenceGetSilenceHandler = silence_ops.GetSilenceHandlerFunc(api.getSilenceHandler)
	openAPI.SilenceGetSilencesHandler = silence_ops.GetSilencesHandlerFunc(api.getSilencesHandler)
	openAPI.SilencePostSilencesHandler = silence_ops.PostSilencesHandlerFunc(api.postSilencesHandler)

	openAPI.Logger = func(s string, i ...interface{}) { level.Error(api.logger).Log(i...) }

	handleCORS := cors.Default().Handler
	api.Handler = handleCORS(openAPI.Serve(nil))

	return &api, nil
}

// Update sets the API struct members that may change between reloads of alertmanager.
func (api *API) Update(cfg *config.Config, setAlertStatus setAlertStatusFn) {
	api.mtx.Lock()
	defer api.mtx.Unlock()

	api.alertmanagerConfig = cfg
	api.route = dispatch.NewRoute(cfg.Route, nil)
	api.setAlertStatus = setAlertStatus
}

func (api *API) getStatusHandler(params general_ops.GetStatusParams) middleware.Responder {
	api.mtx.RLock()
	defer api.mtx.RUnlock()

	original := api.alertmanagerConfig.String()
	uptime := strfmt.DateTime(api.uptime)

	status := open_api_models.ClusterStatusStatusDisabled

	resp := open_api_models.AlertmanagerStatus{
		Uptime: &uptime,
		VersionInfo: &open_api_models.VersionInfo{
			Version:   &version.Version,
			Revision:  &version.Revision,
			Branch:    &version.Branch,
			BuildUser: &version.BuildUser,
			BuildDate: &version.BuildDate,
			GoVersion: &version.GoVersion,
		},
		Config: &open_api_models.AlertmanagerConfig{
			Original: &original,
		},
		Cluster: &open_api_models.ClusterStatus{
			Status: &status,
		},
	}

	// If alertmanager cluster feature is disabled, then api.peers == nil.
	if api.peer != nil {
		status := api.peer.Status()

		peers := []*open_api_models.PeerStatus{}
		for _, n := range api.peer.Peers() {
			address := n.Address()
			peers = append(peers, &open_api_models.PeerStatus{
				Name:    &n.Name,
				Address: &address,
			})
		}

		sort.Slice(peers, func(i, j int) bool {
			return *peers[i].Name < *peers[j].Name
		})

		resp.Cluster = &open_api_models.ClusterStatus{
			Name:   api.peer.Name(),
			Status: &status,
			Peers:  peers,
		}
	}

	return general_ops.NewGetStatusOK().WithPayload(&resp)
}

func (api *API) getReceiversHandler(params receiver_ops.GetReceiversParams) middleware.Responder {
	api.mtx.RLock()
	defer api.mtx.RUnlock()

	receivers := make([]*open_api_models.Receiver, 0, len(api.alertmanagerConfig.Receivers))
	for _, r := range api.alertmanagerConfig.Receivers {
		receivers = append(receivers, &open_api_models.Receiver{Name: &r.Name})
	}

	return receiver_ops.NewGetReceiversOK().WithPayload(receivers)
}

func (api *API) getAlertsHandler(params alert_ops.GetAlertsParams) middleware.Responder {
	var (
		receiverFilter *regexp.Regexp
		// Initialize result slice to prevent api returning `null` when there
		// are no alerts present
		res = open_api_models.GettableAlerts{}
		ctx = params.HTTPRequest.Context()
	)

	matchers, err := parseFilter(params.Filter)
	if err != nil {
		level.Error(api.logger).Log("msg", "failed to parse matchers", "err", err)
		return alertgroup_ops.NewGetAlertGroupsBadRequest().WithPayload(err.Error())
	}

	if params.Receiver != nil {
		receiverFilter, err = regexp.Compile("^(?:" + *params.Receiver + ")$")
		if err != nil {
			level.Error(api.logger).Log("msg", "failed to compile receiver regex", "err", err)
			return alert_ops.
				NewGetAlertsBadRequest().
				WithPayload(
					fmt.Sprintf("failed to parse receiver param: %v", err.Error()),
				)
		}
	}

	alerts := api.alerts.GetPending()
	defer alerts.Close()

	alertFilter := api.alertFilter(matchers, *params.Silenced, *params.Inhibited, *params.Active)
	now := time.Now()

	api.mtx.RLock()
	for a := range alerts.Next() {
		if err = alerts.Err(); err != nil {
			break
		}
		if err = ctx.Err(); err != nil {
			break
		}

		routes := api.route.Match(a.Labels)
		receivers := make([]string, 0, len(routes))
		for _, r := range routes {
			receivers = append(receivers, r.RouteOpts.Receiver)
		}

		if receiverFilter != nil && !receiversMatchFilter(receivers, receiverFilter) {
			continue
		}

		if !alertFilter(a, now) {
			continue
		}

		alert := alertToOpenAPIAlert(a, api.getAlertStatus(a.Fingerprint()), receivers)

		res = append(res, alert)
	}
	api.mtx.RUnlock()

	if err != nil {
		level.Error(api.logger).Log("msg", "failed to get alerts", "err", err)
		return alert_ops.NewGetAlertsInternalServerError().WithPayload(err.Error())
	}
	sort.Slice(res, func(i, j int) bool {
		return *res[i].Fingerprint < *res[j].Fingerprint
	})

	return alert_ops.NewGetAlertsOK().WithPayload(res)
}

func (api *API) postAlertsHandler(params alert_ops.PostAlertsParams) middleware.Responder {
	alerts := openAPIAlertsToAlerts(params.Alerts)
	now := time.Now()

	api.mtx.RLock()
	resolveTimeout := time.Duration(api.alertmanagerConfig.Global.ResolveTimeout)
	api.mtx.RUnlock()

	for _, alert := range alerts {
		alert.UpdatedAt = now

		// Ensure StartsAt is set.
		if alert.StartsAt.IsZero() {
			if alert.EndsAt.IsZero() {
				alert.StartsAt = now
			} else {
				alert.StartsAt = alert.EndsAt
			}
		}
		// If no end time is defined, set a timeout after which an alert
		// is marked resolved if it is not updated.
		if alert.EndsAt.IsZero() {
			alert.Timeout = true
			alert.EndsAt = now.Add(resolveTimeout)
		}
		if alert.EndsAt.After(time.Now()) {
			api.m.Firing().Inc()
		} else {
			api.m.Resolved().Inc()
		}
	}

	// Make a best effort to insert all alerts that are valid.
	var (
		validAlerts    = make([]*types.Alert, 0, len(alerts))
		validationErrs = &types.MultiError{}
	)
	for _, a := range alerts {
		removeEmptyLabels(a.Labels)

		if err := a.Validate(); err != nil {
			validationErrs.Add(err)
			api.m.Invalid().Inc()
			continue
		}
		validAlerts = append(validAlerts, a)
	}
	if err := api.alerts.Put(validAlerts...); err != nil {
		level.Error(api.logger).Log("msg", "failed to create alerts", "err", err)
		return alert_ops.NewPostAlertsInternalServerError().WithPayload(err.Error())
	}

	if validationErrs.Len() > 0 {
		level.Error(api.logger).Log("msg", "failed to validate alerts", "err", validationErrs.Error())
		return alert_ops.NewPostAlertsBadRequest().WithPayload(validationErrs.Error())
	}

	return alert_ops.NewPostAlertsOK()
}

func (api *API) getAlertGroupsHandler(params alertgroup_ops.GetAlertGroupsParams) middleware.Responder {
	var receiverFilter *regexp.Regexp

	matchers, err := parseFilter(params.Filter)
	if err != nil {
		level.Error(api.logger).Log("msg", "failed to parse matchers", "err", err)
		return alertgroup_ops.NewGetAlertGroupsBadRequest().WithPayload(err.Error())
	}

	if params.Receiver != nil {
		receiverFilter, err = regexp.Compile("^(?:" + *params.Receiver + ")$")
		if err != nil {
			level.Error(api.logger).Log("msg", "failed to compile receiver regex", "err", err)
			return alertgroup_ops.
				NewGetAlertGroupsBadRequest().
				WithPayload(
					fmt.Sprintf("failed to parse receiver param: %v", err.Error()),
				)
		}
	}

	rf := func(receiverFilter *regexp.Regexp) func(r *dispatch.Route) bool {
		return func(r *dispatch.Route) bool {
			receiver := r.RouteOpts.Receiver
			if receiverFilter != nil && !receiverFilter.MatchString(receiver) {
				return false
			}
			return true
		}
	}(receiverFilter)

	af := api.alertFilter(matchers, *params.Silenced, *params.Inhibited, *params.Active)
	alertGroups, allReceivers := api.alertGroups(rf, af)

	res := make(open_api_models.AlertGroups, 0, len(alertGroups))

	for _, alertGroup := range alertGroups {
		ag := &open_api_models.AlertGroup{
			Receiver: &open_api_models.Receiver{Name: &alertGroup.Receiver},
			Labels:   modelLabelSetToAPILabelSet(alertGroup.Labels),
			Alerts:   make([]*open_api_models.GettableAlert, 0, len(alertGroup.Alerts)),
		}

		for _, alert := range alertGroup.Alerts {
			fp := alert.Fingerprint()
			receivers := allReceivers[fp]
			status := api.getAlertStatus(fp)
			apiAlert := alertToOpenAPIAlert(alert, status, receivers)
			ag.Alerts = append(ag.Alerts, apiAlert)
		}
		res = append(res, ag)
	}

	return alertgroup_ops.NewGetAlertGroupsOK().WithPayload(res)
}

func (api *API) alertFilter(matchers []*labels.Matcher, silenced, inhibited, active bool) func(a *types.Alert, now time.Time) bool {
	return func(a *types.Alert, now time.Time) bool {
		if !a.EndsAt.IsZero() && a.EndsAt.Before(now) {
			return false
		}

		// Set alert's current status based on its label set.
		api.setAlertStatus(a.Labels)

		// Get alert's current status after seeing if it is suppressed.
		status := api.getAlertStatus(a.Fingerprint())

		if !active && status.State == types.AlertStateActive {
			return false
		}

		if !silenced && len(status.SilencedBy) != 0 {
			return false
		}

		if !inhibited && len(status.InhibitedBy) != 0 {
			return false
		}

		return alertMatchesFilterLabels(&a.Alert, matchers)
	}
}

func alertToOpenAPIAlert(alert *types.Alert, status types.AlertStatus, receivers []string) *open_api_models.GettableAlert {
	startsAt := strfmt.DateTime(alert.StartsAt)
	updatedAt := strfmt.DateTime(alert.UpdatedAt)
	endsAt := strfmt.DateTime(alert.EndsAt)

	apiReceivers := make([]*open_api_models.Receiver, 0, len(receivers))
	for i := range receivers {
		apiReceivers = append(apiReceivers, &open_api_models.Receiver{Name: &receivers[i]})
	}

	fp := alert.Fingerprint().String()
	state := string(status.State)
	aa := &open_api_models.GettableAlert{
		Alert: open_api_models.Alert{
			GeneratorURL: strfmt.URI(alert.GeneratorURL),
			Labels:       modelLabelSetToAPILabelSet(alert.Labels),
		},
		Annotations: modelLabelSetToAPILabelSet(alert.Annotations),
		StartsAt:    &startsAt,
		UpdatedAt:   &updatedAt,
		EndsAt:      &endsAt,
		Fingerprint: &fp,
		Receivers:   apiReceivers,
		Status: &open_api_models.AlertStatus{
			State:       &state,
			SilencedBy:  status.SilencedBy,
			InhibitedBy: status.InhibitedBy,
		},
	}

	if aa.Status.SilencedBy == nil {
		aa.Status.SilencedBy = []string{}
	}

	if aa.Status.InhibitedBy == nil {
		aa.Status.InhibitedBy = []string{}
	}

	return aa
}

func openAPIAlertsToAlerts(apiAlerts open_api_models.PostableAlerts) []*types.Alert {
	alerts := []*types.Alert{}
	for _, apiAlert := range apiAlerts {
		alert := types.Alert{
			Alert: prometheus_model.Alert{
				Labels:       apiLabelSetToModelLabelSet(apiAlert.Labels),
				Annotations:  apiLabelSetToModelLabelSet(apiAlert.Annotations),
				StartsAt:     time.Time(apiAlert.StartsAt),
				EndsAt:       time.Time(apiAlert.EndsAt),
				GeneratorURL: string(apiAlert.GeneratorURL),
			},
		}
		alerts = append(alerts, &alert)
	}

	return alerts
}

func removeEmptyLabels(ls prometheus_model.LabelSet) {
	for k, v := range ls {
		if string(v) == "" {
			delete(ls, k)
		}
	}
}

func modelLabelSetToAPILabelSet(modelLabelSet prometheus_model.LabelSet) open_api_models.LabelSet {
	apiLabelSet := open_api_models.LabelSet{}
	for key, value := range modelLabelSet {
		apiLabelSet[string(key)] = string(value)
	}

	return apiLabelSet
}

func apiLabelSetToModelLabelSet(apiLabelSet open_api_models.LabelSet) prometheus_model.LabelSet {
	modelLabelSet := prometheus_model.LabelSet{}
	for key, value := range apiLabelSet {
		modelLabelSet[prometheus_model.LabelName(key)] = prometheus_model.LabelValue(value)
	}

	return modelLabelSet
}

func receiversMatchFilter(receivers []string, filter *regexp.Regexp) bool {
	for _, r := range receivers {
		if filter.MatchString(r) {
			return true
		}
	}

	return false
}

func alertMatchesFilterLabels(a *prometheus_model.Alert, matchers []*labels.Matcher) bool {
	sms := make(map[string]string)
	for name, value := range a.Labels {
		sms[string(name)] = string(value)
	}
	return matchFilterLabels(matchers, sms)
}

func matchFilterLabels(matchers []*labels.Matcher, sms map[string]string) bool {
	for _, m := range matchers {
		v, prs := sms[m.Name]
		switch m.Type {
		case labels.MatchNotRegexp, labels.MatchNotEqual:
			if m.Value == "" && prs {
				continue
			}
			if !m.Matches(v) {
				return false
			}
		default:
			if m.Value == "" && !prs {
				continue
			}
			if !prs || !m.Matches(v) {
				return false
			}
		}
	}

	return true
}

func (api *API) getSilencesHandler(params silence_ops.GetSilencesParams) middleware.Responder {
	matchers := []*labels.Matcher{}
	if params.Filter != nil {
		for _, matcherString := range params.Filter {
			matcher, err := labels.ParseMatcher(matcherString)
			if err != nil {
				level.Error(api.logger).Log("msg", "failed to parse matchers", "err", err)
				return alert_ops.NewGetAlertsBadRequest().WithPayload(err.Error())
			}

			matchers = append(matchers, matcher)
		}
	}

	psils, _, err := api.silences.Query()
	if err != nil {
		level.Error(api.logger).Log("msg", "failed to get silences", "err", err)
		return silence_ops.NewGetSilencesInternalServerError().WithPayload(err.Error())
	}

	sils := open_api_models.GettableSilences{}
	for _, ps := range psils {
		silence, err := gettableSilenceFromProto(ps)
		if err != nil {
			level.Error(api.logger).Log("msg", "failed to unmarshal silence from proto", "err", err)
			return silence_ops.NewGetSilencesInternalServerError().WithPayload(err.Error())
		}
		if !gettableSilenceMatchesFilterLabels(silence, matchers) {
			continue
		}
		sils = append(sils, &silence)
	}

	sortSilences(sils)

	return silence_ops.NewGetSilencesOK().WithPayload(sils)
}

var (
	silenceStateOrder = map[types.SilenceState]int{
		types.SilenceStateActive:  1,
		types.SilenceStatePending: 2,
		types.SilenceStateExpired: 3,
	}
)

// sortSilences sorts first according to the state "active, pending, expired"
// then by end time or start time depending on the state.
// active silences should show the next to expire first
// pending silences are ordered based on which one starts next
// expired are ordered based on which one expired most recently
func sortSilences(sils open_api_models.GettableSilences) {
	sort.Slice(sils, func(i, j int) bool {
		state1 := types.SilenceState(*sils[i].Status.State)
		state2 := types.SilenceState(*sils[j].Status.State)
		if state1 != state2 {
			return silenceStateOrder[state1] < silenceStateOrder[state2]
		}
		switch state1 {
		case types.SilenceStateActive:
			endsAt1 := time.Time(*sils[i].Silence.EndsAt)
			endsAt2 := time.Time(*sils[j].Silence.EndsAt)
			return endsAt1.Before(endsAt2)
		case types.SilenceStatePending:
			startsAt1 := time.Time(*sils[i].Silence.StartsAt)
			startsAt2 := time.Time(*sils[j].Silence.StartsAt)
			return startsAt1.Before(startsAt2)
		case types.SilenceStateExpired:
			endsAt1 := time.Time(*sils[i].Silence.EndsAt)
			endsAt2 := time.Time(*sils[j].Silence.EndsAt)
			return endsAt1.After(endsAt2)
		}
		return false
	})
}

func gettableSilenceMatchesFilterLabels(s open_api_models.GettableSilence, matchers []*labels.Matcher) bool {
	sms := make(map[string]string)
	for _, m := range s.Matchers {
		sms[*m.Name] = *m.Value
	}

	return matchFilterLabels(matchers, sms)
}

func (api *API) getSilenceHandler(params silence_ops.GetSilenceParams) middleware.Responder {
	sils, _, err := api.silences.Query(silence.QIDs(params.SilenceID.String()))
	if err != nil {
		level.Error(api.logger).Log("msg", "failed to get silence by id", "err", err)
		return silence_ops.NewGetSilenceInternalServerError().WithPayload(err.Error())
	}

	if len(sils) == 0 {
		level.Error(api.logger).Log("msg", "failed to find silence", "err", err)
		return silence_ops.NewGetSilenceNotFound()
	}

	sil, err := gettableSilenceFromProto(sils[0])
	if err != nil {
		level.Error(api.logger).Log("msg", "failed to convert unmarshal from proto", "err", err)
		return silence_ops.NewGetSilenceInternalServerError().WithPayload(err.Error())
	}

	return silence_ops.NewGetSilenceOK().WithPayload(&sil)
}

func (api *API) deleteSilenceHandler(params silence_ops.DeleteSilenceParams) middleware.Responder {
	sid := params.SilenceID.String()

	if err := api.silences.Expire(sid); err != nil {
		level.Error(api.logger).Log("msg", "failed to expire silence", "err", err)
		return silence_ops.NewDeleteSilenceInternalServerError().WithPayload(err.Error())
	}
	return silence_ops.NewDeleteSilenceOK()
}

func gettableSilenceFromProto(s *silencepb.Silence) (open_api_models.GettableSilence, error) {
	start := strfmt.DateTime(s.StartsAt)
	end := strfmt.DateTime(s.EndsAt)
	updated := strfmt.DateTime(s.UpdatedAt)
	state := string(types.CalcSilenceState(s.StartsAt, s.EndsAt))
	sil := open_api_models.GettableSilence{
		Silence: open_api_models.Silence{
			StartsAt:  &start,
			EndsAt:    &end,
			Comment:   &s.Comment,
			CreatedBy: &s.CreatedBy,
		},
		ID:        &s.Id,
		UpdatedAt: &updated,
		Status: &open_api_models.SilenceStatus{
			State: &state,
		},
	}

	for _, m := range s.Matchers {
		matcher := &open_api_models.Matcher{
			Name:  &m.Name,
			Value: &m.Pattern,
		}
		switch m.Type {
		case silencepb.Matcher_EQUAL:
			f := false
			matcher.IsRegex = &f
		case silencepb.Matcher_REGEXP:
			t := true
			matcher.IsRegex = &t
		default:
			return sil, fmt.Errorf(
				"unknown matcher type for matcher '%v' in silence '%v'",
				m.Name,
				s.Id,
			)
		}
		sil.Matchers = append(sil.Matchers, matcher)
	}

	return sil, nil
}

func (api *API) postSilencesHandler(params silence_ops.PostSilencesParams) middleware.Responder {

	sil, err := postableSilenceToProto(params.Silence)
	if err != nil {
		level.Error(api.logger).Log("msg", "failed to marshal silence to proto", "err", err)
		return silence_ops.NewPostSilencesBadRequest().WithPayload(
			fmt.Sprintf("failed to convert API silence to internal silence: %v", err.Error()),
		)
	}

	if sil.StartsAt.After(sil.EndsAt) || sil.StartsAt.Equal(sil.EndsAt) {
		msg := "failed to create silence: start time must be equal or after end time"
		level.Error(api.logger).Log("msg", msg, "err", err)
		return silence_ops.NewPostSilencesBadRequest().WithPayload(msg)
	}

	if sil.EndsAt.Before(time.Now()) {
		msg := "failed to create silence: end time can't be in the past"
		level.Error(api.logger).Log("msg", msg, "err", err)
		return silence_ops.NewPostSilencesBadRequest().WithPayload(msg)
	}

	sid, err := api.silences.Set(sil)
	if err != nil {
		level.Error(api.logger).Log("msg", "failed to create silence", "err", err)
		if err == silence.ErrNotFound {
			return silence_ops.NewPostSilencesNotFound().WithPayload(err.Error())
		}
		return silence_ops.NewPostSilencesBadRequest().WithPayload(err.Error())
	}

	return silence_ops.NewPostSilencesOK().WithPayload(&silence_ops.PostSilencesOKBody{
		SilenceID: sid,
	})
}

func postableSilenceToProto(s *open_api_models.PostableSilence) (*silencepb.Silence, error) {
	sil := &silencepb.Silence{
		Id:        s.ID,
		StartsAt:  time.Time(*s.StartsAt),
		EndsAt:    time.Time(*s.EndsAt),
		Comment:   *s.Comment,
		CreatedBy: *s.CreatedBy,
	}
	for _, m := range s.Matchers {
		matcher := &silencepb.Matcher{
			Name:    *m.Name,
			Pattern: *m.Value,
			Type:    silencepb.Matcher_EQUAL,
		}
		if *m.IsRegex {
			matcher.Type = silencepb.Matcher_REGEXP
		}
		sil.Matchers = append(sil.Matchers, matcher)
	}
	return sil, nil
}

func parseFilter(filter []string) ([]*labels.Matcher, error) {
	matchers := make([]*labels.Matcher, 0, len(filter))
	for _, matcherString := range filter {
		matcher, err := labels.ParseMatcher(matcherString)
		if err != nil {
			return nil, err
		}

		matchers = append(matchers, matcher)
	}
	return matchers, nil
}
