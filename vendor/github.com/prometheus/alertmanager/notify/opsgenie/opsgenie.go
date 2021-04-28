// Copyright 2019 Prometheus Team
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

package opsgenie

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	commoncfg "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"

	"github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/notify"
	"github.com/prometheus/alertmanager/template"
	"github.com/prometheus/alertmanager/types"
)

// Notifier implements a Notifier for OpsGenie notifications.
type Notifier struct {
	conf    *config.OpsGenieConfig
	tmpl    *template.Template
	logger  log.Logger
	client  *http.Client
	retrier *notify.Retrier
}

// New returns a new OpsGenie notifier.
func New(c *config.OpsGenieConfig, t *template.Template, l log.Logger, httpOpts ...commoncfg.HTTPClientOption) (*Notifier, error) {
	client, err := commoncfg.NewClientFromConfig(*c.HTTPConfig, "opsgenie", append(httpOpts, commoncfg.WithHTTP2Disabled())...)
	if err != nil {
		return nil, err
	}
	return &Notifier{
		conf:    c,
		tmpl:    t,
		logger:  l,
		client:  client,
		retrier: &notify.Retrier{RetryCodes: []int{http.StatusTooManyRequests}},
	}, nil
}

type opsGenieCreateMessage struct {
	Alias       string                           `json:"alias"`
	Message     string                           `json:"message"`
	Description string                           `json:"description,omitempty"`
	Details     map[string]string                `json:"details"`
	Source      string                           `json:"source"`
	Responders  []opsGenieCreateMessageResponder `json:"responders,omitempty"`
	Tags        []string                         `json:"tags,omitempty"`
	Note        string                           `json:"note,omitempty"`
	Priority    string                           `json:"priority,omitempty"`
}

type opsGenieCreateMessageResponder struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	Username string `json:"username,omitempty"`
	Type     string `json:"type"` // team, user, escalation, schedule etc.
}

type opsGenieCloseMessage struct {
	Source string `json:"source"`
}

// Notify implements the Notifier interface.
func (n *Notifier) Notify(ctx context.Context, as ...*types.Alert) (bool, error) {
	req, retry, err := n.createRequest(ctx, as...)
	if err != nil {
		return retry, err
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return true, err
	}
	defer notify.Drain(resp)

	return n.retrier.Check(resp.StatusCode, resp.Body)
}

// Like Split but filter out empty strings.
func safeSplit(s string, sep string) []string {
	a := strings.Split(strings.TrimSpace(s), sep)
	b := a[:0]
	for _, x := range a {
		if x != "" {
			b = append(b, x)
		}
	}
	return b
}

// Create requests for a list of alerts.
func (n *Notifier) createRequest(ctx context.Context, as ...*types.Alert) (*http.Request, bool, error) {
	key, err := notify.ExtractGroupKey(ctx)
	if err != nil {
		return nil, false, err
	}
	data := notify.GetTemplateData(ctx, n.tmpl, as, n.logger)

	level.Debug(n.logger).Log("incident", key)

	tmpl := notify.TmplText(n.tmpl, data, &err)

	details := make(map[string]string)

	for k, v := range data.CommonLabels {
		details[k] = v
	}

	for k, v := range n.conf.Details {
		details[k] = tmpl(v)
	}

	var (
		msg    interface{}
		apiURL = n.conf.APIURL.Copy()
		alias  = key.Hash()
		alerts = types.Alerts(as...)
	)
	switch alerts.Status() {
	case model.AlertResolved:
		apiURL.Path += fmt.Sprintf("v2/alerts/%s/close", alias)
		q := apiURL.Query()
		q.Set("identifierType", "alias")
		apiURL.RawQuery = q.Encode()
		msg = &opsGenieCloseMessage{Source: tmpl(n.conf.Source)}
	default:
		message, truncated := notify.Truncate(tmpl(n.conf.Message), 130)
		if truncated {
			level.Debug(n.logger).Log("msg", "truncated message", "truncated_message", message, "incident", key)
		}

		apiURL.Path += "v2/alerts"

		var responders []opsGenieCreateMessageResponder
		for _, r := range n.conf.Responders {
			responder := opsGenieCreateMessageResponder{
				ID:       tmpl(r.ID),
				Name:     tmpl(r.Name),
				Username: tmpl(r.Username),
				Type:     tmpl(r.Type),
			}

			if responder == (opsGenieCreateMessageResponder{}) {
				// Filter out empty responders. This is useful if you want to fill
				// responders dynamically from alert's common labels.
				continue
			}

			responders = append(responders, responder)
		}

		msg = &opsGenieCreateMessage{
			Alias:       alias,
			Message:     message,
			Description: tmpl(n.conf.Description),
			Details:     details,
			Source:      tmpl(n.conf.Source),
			Responders:  responders,
			Tags:        safeSplit(string(tmpl(n.conf.Tags)), ","),
			Note:        tmpl(n.conf.Note),
			Priority:    tmpl(n.conf.Priority),
		}
	}

	apiKey := tmpl(string(n.conf.APIKey))

	if err != nil {
		return nil, false, errors.Wrap(err, "templating error")
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(msg); err != nil {
		return nil, false, err
	}

	req, err := http.NewRequest("POST", apiURL.String(), &buf)
	if err != nil {
		return nil, true, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("GenieKey %s", apiKey))
	return req.WithContext(ctx), true, nil
}
