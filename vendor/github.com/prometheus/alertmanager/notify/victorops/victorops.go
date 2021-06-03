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

package victorops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	commoncfg "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"

	"github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/notify"
	"github.com/prometheus/alertmanager/template"
	"github.com/prometheus/alertmanager/types"
)

// Notifier implements a Notifier for VictorOps notifications.
type Notifier struct {
	conf    *config.VictorOpsConfig
	tmpl    *template.Template
	logger  log.Logger
	client  *http.Client
	retrier *notify.Retrier
}

// New returns a new VictorOps notifier.
func New(c *config.VictorOpsConfig, t *template.Template, l log.Logger, httpOpts ...commoncfg.HTTPClientOption) (*Notifier, error) {
	client, err := commoncfg.NewClientFromConfig(*c.HTTPConfig, "victorops", append(httpOpts, commoncfg.WithHTTP2Disabled())...)
	if err != nil {
		return nil, err
	}
	return &Notifier{
		conf:   c,
		tmpl:   t,
		logger: l,
		client: client,
		// Missing documentation therefore assuming only 5xx response codes are
		// recoverable.
		retrier: &notify.Retrier{},
	}, nil
}

const (
	victorOpsEventTrigger = "CRITICAL"
	victorOpsEventResolve = "RECOVERY"
)

// Notify implements the Notifier interface.
func (n *Notifier) Notify(ctx context.Context, as ...*types.Alert) (bool, error) {

	var err error
	var (
		data   = notify.GetTemplateData(ctx, n.tmpl, as, n.logger)
		tmpl   = notify.TmplText(n.tmpl, data, &err)
		apiURL = n.conf.APIURL.Copy()
	)
	apiURL.Path += fmt.Sprintf("%s/%s", n.conf.APIKey, tmpl(n.conf.RoutingKey))
	if err != nil {
		return false, fmt.Errorf("templating error: %s", err)
	}

	buf, err := n.createVictorOpsPayload(ctx, as...)
	if err != nil {
		return true, err
	}

	resp, err := notify.PostJSON(ctx, n.client, apiURL.String(), buf)
	if err != nil {
		return true, notify.RedactURL(err)
	}
	defer notify.Drain(resp)

	return n.retrier.Check(resp.StatusCode, nil)
}

// Create the JSON payload to be sent to the VictorOps API.
func (n *Notifier) createVictorOpsPayload(ctx context.Context, as ...*types.Alert) (*bytes.Buffer, error) {
	victorOpsAllowedEvents := map[string]bool{
		"INFO":     true,
		"WARNING":  true,
		"CRITICAL": true,
	}

	key, err := notify.ExtractGroupKey(ctx)
	if err != nil {
		return nil, err
	}

	var (
		alerts = types.Alerts(as...)
		data   = notify.GetTemplateData(ctx, n.tmpl, as, n.logger)
		tmpl   = notify.TmplText(n.tmpl, data, &err)

		messageType  = tmpl(n.conf.MessageType)
		stateMessage = tmpl(n.conf.StateMessage)
	)

	if alerts.Status() == model.AlertFiring && !victorOpsAllowedEvents[messageType] {
		messageType = victorOpsEventTrigger
	}

	if alerts.Status() == model.AlertResolved {
		messageType = victorOpsEventResolve
	}

	stateMessage, truncated := notify.Truncate(stateMessage, 20480)
	if truncated {
		level.Debug(n.logger).Log("msg", "truncated stateMessage", "truncated_state_message", stateMessage, "incident", key)
	}

	msg := map[string]string{
		"message_type":        messageType,
		"entity_id":           key.Hash(),
		"entity_display_name": tmpl(n.conf.EntityDisplayName),
		"state_message":       stateMessage,
		"monitoring_tool":     tmpl(n.conf.MonitoringTool),
	}

	if err != nil {
		return nil, fmt.Errorf("templating error: %s", err)
	}

	// Add custom fields to the payload.
	for k, v := range n.conf.CustomFields {
		msg[k] = tmpl(v)
		if err != nil {
			return nil, fmt.Errorf("templating error: %s", err)
		}
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(msg); err != nil {
		return nil, err
	}
	return &buf, nil
}
